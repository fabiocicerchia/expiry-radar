package source

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// AppleSource reports signing certificates and provisioning profiles from App
// Store Connect.
//
// The highest nobody-watches-it ratio in the tool. Both expire annually, both
// are created once by whoever set up the pipeline, and when one lapses the
// failure is a build error in CI rather than anything that looks like a
// certificate — so it is usually diagnosed by the person least likely to know
// what a provisioning profile is.
type AppleSource struct {
	// IssuerID and KeyID come from App Store Connect; PrivateKeyFile is the
	// .p8 that was downloadable exactly once.
	IssuerID       string
	KeyID          string
	PrivateKeyFile string
	BaseURL        string // test seam
	Timeout        time.Duration

	SkipCertificates bool
	SkipProfiles     bool
}

const (
	appleAPI      = "https://api.appstoreconnect.apple.com"
	appleAudience = "appstoreconnect-v1"
	// Apple rejects an assertion valid for longer than twenty minutes.
	appleTokenLife = 15 * time.Minute
)

// Name identifies this source in an item's Source field.
func (s *AppleSource) Name() string { return "apple" }

// Collect reads certificates and provisioning profiles.
func (s *AppleSource) Collect(ctx context.Context) ([]Item, error) {
	if s.IssuerID == "" || s.KeyID == "" || s.PrivateKeyFile == "" {
		return nil, fmt.Errorf(
			"apple source needs issuerId, keyId and privateKeyFile (the .p8 from App Store Connect)")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	token, err := s.assertion()
	if err != nil {
		// No token means nothing is readable, so there are no partial
		// findings to keep.
		return nil, err
	}
	headers := map[string]string{"Authorization": "Bearer " + token}

	items, _, collectErr := collectUnits([]collectUnit{
		{"certificates", s.SkipCertificates,
			func() ([]Item, error) { return s.certificates(ctx, client, headers) }},
		{"profiles", s.SkipProfiles,
			func() ([]Item, error) { return s.profiles(ctx, client, headers) }},
	})
	return items, collectErr
}

func (s *AppleSource) base() string {
	if s.BaseURL != "" {
		return strings.TrimSuffix(s.BaseURL, "/")
	}
	return appleAPI
}

// assertion builds and signs the ES256 JWT App Store Connect requires.
//
// The signature encoding is the part worth being careful about: ecdsa.Sign
// returns two big integers, and JWS wants them as a fixed-width big-endian
// pair padded to the curve size — not the ASN.1 sequence that
// ecdsa.SignASN1 and most Go examples produce. Getting that wrong yields a
// token Apple rejects with a 401 that says nothing useful.
func (s *AppleSource) assertion() (string, error) {
	raw, err := os.ReadFile(s.PrivateKeyFile)
	if err != nil {
		return "", fmt.Errorf("reading the App Store Connect key: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return "", fmt.Errorf("%s is not a PEM .p8 key", s.PrivateKeyFile)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parsing the App Store Connect key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("%s is not an ECDSA key; App Store Connect keys are P-256",
			s.PrivateKeyFile)
	}

	header, err := json.Marshal(map[string]string{
		"alg": "ES256", "kid": s.KeyID, "typ": "JWT",
	})
	if err != nil {
		return "", err
	}
	now := time.Now()
	claims, err := json.Marshal(map[string]any{
		"iss": s.IssuerID,
		"iat": now.Unix(),
		"exp": now.Add(appleTokenLife).Unix(),
		"aud": appleAudience,
	})
	if err != nil {
		return "", err
	}

	signing := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	r, sig, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", fmt.Errorf("signing the App Store Connect assertion: %w", err)
	}

	// JWS ES256: r and s, each left-padded to the curve's byte size.
	size := (key.Curve.Params().BitSize + 7) / 8
	out := make([]byte, 2*size)
	r.FillBytes(out[:size])
	sig.FillBytes(out[size:])

	return signing + "." + base64.RawURLEncoding.EncodeToString(out), nil
}

// appleList is the JSON:API envelope App Store Connect returns.
type appleList[T any] struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes T      `json:"attributes"`
	} `json:"data"`
}

type appleCertificate struct {
	Name            string `json:"name"`
	DisplayName     string `json:"displayName"`
	CertificateType string `json:"certificateType"`
	SerialNumber    string `json:"serialNumber"`
	ExpirationDate  string `json:"expirationDate"`
}

func (s *AppleSource) certificates(ctx context.Context, client *http.Client,
	headers map[string]string) ([]Item, error) {
	var body appleList[appleCertificate]
	u := s.base() + "/v1/certificates?limit=200"
	if err := getJSON(ctx, client, u, headers,
		"the App Store Connect key needs at least Developer access", &body); err != nil {
		return nil, err
	}

	var items []Item
	for _, c := range body.Data {
		expires, ok := cfTime(c.Attributes.ExpirationDate)
		if !ok {
			continue
		}
		name := c.Attributes.DisplayName
		if name == "" {
			name = c.Attributes.Name
		}
		if name == "" {
			name = c.ID
		}
		labels := map[string]string{}
		labels = label(labels, "certificate-type", c.Attributes.CertificateType)
		labels = label(labels, LabelSerial, c.Attributes.SerialNumber)
		// A distribution certificate lapsing blocks every release; a
		// development one only inconveniences one machine.
		if strings.Contains(strings.ToUpper(c.Attributes.CertificateType), "DISTRIBUTION") {
			labels = label(labels, LabelBlastRadius, "0.75")
		}
		items = append(items, Item{
			Kind:    KindTLSCert,
			Name:    "certificate/" + name,
			Expires: expires,
			Source:  "apple:certificate",
			Labels:  labels,
		})
	}
	return items, nil
}

type appleProfile struct {
	Name           string `json:"name"`
	ProfileType    string `json:"profileType"`
	ProfileState   string `json:"profileState"`
	ExpirationDate string `json:"expirationDate"`
}

func (s *AppleSource) profiles(ctx context.Context, client *http.Client,
	headers map[string]string) ([]Item, error) {
	var body appleList[appleProfile]
	u := s.base() + "/v1/profiles?limit=200"
	if err := getJSON(ctx, client, u, headers,
		"the App Store Connect key needs at least Developer access", &body); err != nil {
		return nil, err
	}

	var items []Item
	for _, p := range body.Data {
		expires, ok := cfTime(p.Attributes.ExpirationDate)
		if !ok {
			continue
		}
		name := p.Attributes.Name
		if name == "" {
			name = p.ID
		}
		labels := map[string]string{}
		labels = label(labels, "profile-type", p.Attributes.ProfileType)
		labels = label(labels, "state", p.Attributes.ProfileState)
		// Apple marks a profile INVALID once it has lapsed or its certificate
		// has; it is already not signing anything.
		if !strings.EqualFold(p.Attributes.ProfileState, "ACTIVE") {
			labels[LabelInUse] = "false"
		}
		items = append(items, Item{
			Kind:    KindSecret,
			Name:    "profile/" + name,
			Expires: expires,
			Source:  "apple:profile",
			Labels:  labels,
		})
	}
	return items, nil
}
