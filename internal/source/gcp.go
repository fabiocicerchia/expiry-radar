package source

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// GCPSource reports what expires across a Google Cloud project: managed and
// classic certificates, Secret Manager deadlines, and user-managed service
// account keys.
//
// The data is ordinary REST; the cost of this source is authentication, and it
// is paid here in about eighty lines rather than by taking a dependency. Two
// paths are supported, which is all Google needs: a service account key file
// signs its own JWT assertion and exchanges it for an access token, and a
// workload running on GCP asks the metadata server instead and holds no key at
// all. The second is strictly better where it is available.
type GCPSource struct {
	// Projects to scan. There is no cheap enumeration that does not need
	// resourcemanager permissions most read-only roles lack, so these are
	// named rather than discovered.
	Projects []string
	// Locations for Certificate Manager, which is regional. "global" is the
	// usual answer and the default.
	Locations []string
	// CredentialsFile is a service account key JSON. Empty means ask the
	// metadata server, which is how this should run on GCP.
	CredentialsFile string
	// MaxKeyAgeDays turns service account key age into a rotation deadline.
	// User-managed keys default to a ten-year validity, which is not a
	// deadline anybody means; 0 disables the synthesised date entirely.
	MaxKeyAgeDays int
	Timeout       time.Duration

	// Endpoints override the API hosts. A test seam.
	Endpoints map[string]string

	SkipCertManager bool
	SkipCompute     bool
	SkipSecrets     bool
	SkipKeys        bool

	token     string
	tokenTill time.Time
}

const (
	gcpScope       = "https://www.googleapis.com/auth/cloud-platform.read-only"
	gcpMetadataURL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token"
)

// Name identifies this source in an item's Source field.
func (s *GCPSource) Name() string { return "gcp" }

// Collect reads every configured project.
func (s *GCPSource) Collect(ctx context.Context) ([]Item, error) {
	if len(s.Projects) == 0 {
		return nil, fmt.Errorf("gcp source is enabled but no projects are configured")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	if _, err := s.accessToken(ctx, client); err != nil {
		// No token means nothing is readable at all, which is the one case
		// with no partial findings to keep.
		return nil, err
	}

	items, _, err := collectUnits([]collectUnit{
		{"certificatemanager", s.SkipCertManager, func() ([]Item, error) { return s.certManager(ctx, client) }},
		{"compute", s.SkipCompute, func() ([]Item, error) { return s.computeCerts(ctx, client) }},
		{"secretmanager", s.SkipSecrets, func() ([]Item, error) { return s.secrets(ctx, client) }},
		{"iam", s.SkipKeys, func() ([]Item, error) { return s.serviceAccountKeys(ctx, client) }},
	})
	return items, err
}

// accessToken fetches and caches a bearer token, by whichever route is
// available. Cached because four APIs across several projects is a lot of
// requests to re-authenticate for.
func (s *GCPSource) accessToken(ctx context.Context, client *http.Client) (string, error) {
	if s.token != "" && time.Now().Before(s.tokenTill) {
		return s.token, nil
	}
	var (
		tok string
		ttl time.Duration
		err error
	)
	if s.CredentialsFile != "" {
		tok, ttl, err = s.tokenFromKeyFile(ctx, client)
	} else {
		tok, ttl, err = s.tokenFromMetadata(ctx, client)
	}
	if err != nil {
		return "", err
	}
	s.token = tok
	// Renew early: a token that expires mid-scan would fail half the reads.
	s.tokenTill = time.Now().Add(ttl - time.Minute)
	return tok, nil
}

type gcpKeyFile struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
	Type        string `json:"type"`
}

// tokenFromKeyFile performs the JWT-bearer flow: build an assertion, sign it
// with the account's own key, and exchange it. Google documents this as a plain
// HTTP exchange, so it needs RSA and base64 and nothing else.
func (s *GCPSource) tokenFromKeyFile(ctx context.Context, client *http.Client) (string, time.Duration, error) {
	raw, err := os.ReadFile(s.CredentialsFile)
	if err != nil {
		return "", 0, fmt.Errorf("reading gcp credentials: %w", err)
	}
	var kf gcpKeyFile
	if err := json.Unmarshal(raw, &kf); err != nil {
		return "", 0, fmt.Errorf("parsing gcp credentials: %w", err)
	}
	if kf.ClientEmail == "" || kf.PrivateKey == "" {
		return "", 0, fmt.Errorf("%s is not a service account key file", s.CredentialsFile)
	}
	tokenURI := kf.TokenURI
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}

	key, err := parseRSAPrivateKey(kf.PrivateKey)
	if err != nil {
		return "", 0, err
	}

	now := time.Now()
	header := base64URL(`{"alg":"RS256","typ":"JWT"}`)
	claims, err := json.Marshal(map[string]any{
		"iss":   kf.ClientEmail,
		"scope": gcpScope,
		"aud":   tokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", 0, err
	}
	signing := header + "." + base64URL(string(claims))
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", 0, fmt.Errorf("signing the gcp assertion: %w", err)
	}
	assertion := signing + "." + base64.RawURLEncoding.EncodeToString(sig)

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURI,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	//nolint:errcheck // the body is read or abandoned either way.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("gcp token exchange: %s", resp.Status)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", 0, err
	}
	if out.AccessToken == "" {
		return "", 0, fmt.Errorf("gcp token exchange returned no access token")
	}
	return out.AccessToken, time.Duration(out.ExpiresIn) * time.Second, nil
}

// tokenFromMetadata asks the instance metadata server, which is how this runs
// on GCP: no key file exists, so none can leak.
func (s *GCPSource) tokenFromMetadata(ctx context.Context, client *http.Client) (string, time.Duration, error) {
	endpoint := gcpMetadataURL
	if u, ok := s.Endpoints["metadata"]; ok && u != "" {
		endpoint = u
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf(
			"no gcp credentials: set credentialsFile, or run where the metadata server is reachable: %w", err)
	}
	//nolint:errcheck // the body is read or abandoned either way.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("gcp metadata token: %s", resp.Status)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", 0, err
	}
	return out.AccessToken, time.Duration(out.ExpiresIn) * time.Second, nil
}

func parseRSAPrivateKey(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, fmt.Errorf("gcp private key is not PEM")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("gcp private key is not RSA")
		}
		return rsaKey, nil
	}
	// Older key files are PKCS#1.
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func base64URL(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func (s *GCPSource) api(name, fallback string) string {
	if u, ok := s.Endpoints[name]; ok && u != "" {
		return strings.TrimSuffix(u, "/")
	}
	return fallback
}

// gcpPages walks a list endpoint, handing each page to decode. Google caps
// pageSize well below what a real project holds, so stopping at the first page
// would silently drop service accounts and certificates — the quietest way for
// this tool to be wrong.
func gcpPages(ctx context.Context, s *GCPSource, client *http.Client, base string,
	decode func([]byte) (string, error)) error {
	token := ""
	for page := 0; page < 50; page++ {
		u := base
		if token != "" {
			sep := "?"
			if strings.Contains(u, "?") {
				sep = "&"
			}
			u += sep + "pageToken=" + url.QueryEscape(token)
		}
		raw, err := gcpRaw(ctx, s, client, u)
		if err != nil {
			return err
		}
		next, err := decode(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", base, err)
		}
		if next == "" {
			return nil
		}
		token = next
	}
	return fmt.Errorf("%s: stopped after 50 pages", base)
}

// gcpRaw returns the body so the caller can decode it into its own shape and
// still read nextPageToken out of the same document.
func gcpRaw(ctx context.Context, s *GCPSource, client *http.Client, url string) ([]byte, error) {
	tok, err := s.accessToken(ctx, client)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck // the body is read or abandoned either way.
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden, http.StatusUnauthorized:
		return nil, fmt.Errorf("%s: %s — the service account is missing a viewer role for this API",
			url, resp.Status)
	case http.StatusNotFound:
		return nil, fmt.Errorf("%s: not found — check the project name, or enable the API on it", url)
	default:
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func (s *GCPSource) locations() []string {
	if len(s.Locations) == 0 {
		return []string{"global"}
	}
	return s.Locations
}

// gcpList reads every page of a list endpoint and returns the named array.
// Google spells the array differently per API ("certificates", "items",
// "secrets", "accounts", "keys") but always spells the cursor nextPageToken,
// so the envelope is decoded loosely and the payload strictly.
func gcpList[T any](ctx context.Context, s *GCPSource, client *http.Client,
	url, field string) ([]T, error) {
	var out []T
	err := gcpPages(ctx, s, client, url, func(raw []byte) (string, error) {
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return "", err
		}
		if arr, ok := envelope[field]; ok {
			var batch []T
			if err := json.Unmarshal(arr, &batch); err != nil {
				return "", err
			}
			out = append(out, batch...)
		}
		var next string
		if tok, ok := envelope["nextPageToken"]; ok {
			_ = json.Unmarshal(tok, &next)
		}
		return next, nil
	})
	return out, err
}

type gcpCertificate struct {
	Name        string   `json:"name"`
	ExpireTime  string   `json:"expireTime"`
	SanDnsnames []string `json:"sanDnsnames"`
	Managed     *struct {
		State string `json:"state"`
	} `json:"managed"`
}

// certManager reads Certificate Manager, which is regional.
func (s *GCPSource) certManager(ctx context.Context, client *http.Client) ([]Item, error) {
	base := s.api("certificatemanager", "https://certificatemanager.googleapis.com")
	var items []Item
	var warnings []string

	for _, p := range s.Projects {
		for _, loc := range s.locations() {
			u := fmt.Sprintf("%s/v1/projects/%s/locations/%s/certificates?pageSize=500",
				base, url.PathEscape(p), url.PathEscape(loc))
			certs, err := gcpList[gcpCertificate](ctx, s, client, u, "certificates")
			if err != nil {
				warnings = append(warnings, p+"/"+loc+": "+err.Error())
			}
			for _, c := range certs {
				expires, ok := cfTime(c.ExpireTime)
				if !ok {
					continue
				}
				labels := map[string]string{LabelPublic: "true"}
				labels = label(labels, LabelHosts, strings.Join(c.SanDnsnames, ","))
				// A Google-managed certificate renews itself; a self-managed
				// one is uploaded and nobody renews it for you.
				if c.Managed != nil {
					labels = label(labels, "managed-state", c.Managed.State)
					if strings.EqualFold(c.Managed.State, "ACTIVE") {
						labels[LabelRenewal] = RenewalManaged
					}
				}
				items = append(items, Item{
					Kind:      KindTLSCert,
					Name:      shortGCPName(c.Name),
					Expires:   expires,
					Source:    "gcp:certificatemanager",
					Namespace: p,
					Labels:    labels,
				})
			}
		}
	}
	return items, joinErrs(warnings)
}

type gcpComputeCert struct {
	Name            string   `json:"name"`
	ExpireTime      string   `json:"expireTime"`
	SubjectAltNames []string `json:"subjectAltNames"`
	Type            string   `json:"type"`
}

// computeCerts reads the classic global SSL certificates, which predate
// Certificate Manager and are still what most load balancers use.
func (s *GCPSource) computeCerts(ctx context.Context, client *http.Client) ([]Item, error) {
	base := s.api("compute", "https://compute.googleapis.com")
	var items []Item
	var warnings []string

	for _, p := range s.Projects {
		u := fmt.Sprintf("%s/compute/v1/projects/%s/global/sslCertificates?maxResults=500",
			base, url.PathEscape(p))
		certs, err := gcpList[gcpComputeCert](ctx, s, client, u, "items")
		if err != nil {
			warnings = append(warnings, p+": "+err.Error())
		}
		for _, c := range certs {
			expires, ok := cfTime(c.ExpireTime)
			if !ok {
				continue
			}
			labels := map[string]string{LabelPublic: "true"}
			labels = label(labels, LabelHosts, strings.Join(c.SubjectAltNames, ","))
			labels = label(labels, "cert-type", c.Type)
			if strings.EqualFold(c.Type, "MANAGED") {
				labels[LabelRenewal] = RenewalManaged
			}
			items = append(items, Item{
				Kind:      KindTLSCert,
				Name:      c.Name,
				Expires:   expires,
				Source:    "gcp:compute",
				Namespace: p,
				Labels:    labels,
			})
		}
	}
	return items, joinErrs(warnings)
}

type gcpSecret struct {
	Name       string `json:"name"`
	ExpireTime string `json:"expireTime"`
	Rotation   *struct {
		NextRotationTime string `json:"nextRotationTime"`
	} `json:"rotation"`
}

// secrets reports the two dates a secret can carry: an outright expiry, and a
// next rotation. Both are real deadlines and neither is the other.
func (s *GCPSource) secrets(ctx context.Context, client *http.Client) ([]Item, error) {
	base := s.api("secretmanager", "https://secretmanager.googleapis.com")
	var items []Item
	var warnings []string

	for _, p := range s.Projects {
		u := fmt.Sprintf("%s/v1/projects/%s/secrets?pageSize=500", base, url.PathEscape(p))
		secrets, err := gcpList[gcpSecret](ctx, s, client, u, "secrets")
		if err != nil {
			warnings = append(warnings, p+": "+err.Error())
		}
		for _, sec := range secrets {
			name := shortGCPName(sec.Name)
			if expires, ok := cfTime(sec.ExpireTime); ok {
				items = append(items, Item{
					Kind: KindSecret, Name: name, Expires: expires,
					Source: "gcp:secretmanager", Namespace: p,
					Labels: map[string]string{"deadline": "issuer"},
				})
			}
			if sec.Rotation != nil {
				if next, ok := cfTime(sec.Rotation.NextRotationTime); ok {
					items = append(items, Item{
						Kind: KindSecret, Name: name + " (rotation)", Expires: next,
						Source: "gcp:secretmanager", Namespace: p,
						Labels: map[string]string{"deadline": "rotation schedule"},
					})
				}
			}
		}
	}
	return items, joinErrs(warnings)
}

type gcpServiceAccount struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type gcpSAKey struct {
	Name            string `json:"name"`
	ValidAfterTime  string `json:"validAfterTime"`
	ValidBeforeTime string `json:"validBeforeTime"`
	KeyType         string `json:"keyType"`
}

// serviceAccountKeys reports user-managed keys. Google-managed ones are
// rotated for you and are not anybody's deadline.
//
// A user-managed key's validBeforeTime is about ten years out by default, which
// is not a deadline anybody means, so maxKeyAgeDays synthesises the rotation
// deadline instead — the same treatment AWS access keys get, and labelled the
// same way so the report never implies Google set the date.
func (s *GCPSource) serviceAccountKeys(ctx context.Context, client *http.Client) ([]Item, error) {
	base := s.api("iam", "https://iam.googleapis.com")
	var items []Item
	var warnings []string

	for _, p := range s.Projects {
		u := fmt.Sprintf("%s/v1/projects/%s/serviceAccounts?pageSize=100", base, url.PathEscape(p))
		accounts, err := gcpList[gcpServiceAccount](ctx, s, client, u, "accounts")
		if err != nil {
			warnings = append(warnings, p+": "+err.Error())
		}

		for _, a := range accounts {
			// The key listing is not paginated by Google, but it is read
			// through the same helper so a future cursor is not missed.
			ku := fmt.Sprintf("%s/v1/%s/keys?keyTypes=USER_MANAGED", base, a.Name)
			keys, err := gcpList[gcpSAKey](ctx, s, client, ku, "keys")
			if err != nil {
				warnings = append(warnings, a.Email+": "+err.Error())
				continue
			}
			for _, k := range keys {
				if k.KeyType != "" && k.KeyType != "USER_MANAGED" {
					continue // Google rotates its own; not a deadline
				}
				created, hasCreated := cfTime(k.ValidAfterTime)
				validBefore, hasBefore := cfTime(k.ValidBeforeTime)

				expires := validBefore
				labels := map[string]string{"deadline": "issuer"}
				if s.MaxKeyAgeDays > 0 && hasCreated {
					policy := created.Add(time.Duration(s.MaxKeyAgeDays) * 24 * time.Hour)
					// The policy date is the one that means anything here.
					if !hasBefore || policy.Before(validBefore) {
						expires = policy
						labels = map[string]string{
							"deadline":    "rotation policy",
							"created":     created.Format(time.RFC3339),
							"policy.days": fmt.Sprintf("%d", s.MaxKeyAgeDays),
						}
					}
				}
				if expires.IsZero() {
					continue
				}
				items = append(items, Item{
					Kind:      KindIAMKey,
					Name:      a.Email + "/" + shortGCPName(k.Name),
					Expires:   expires,
					Source:    "gcp:iam",
					Namespace: p,
					Labels:    labels,
				})
			}
		}
	}
	return items, joinErrs(warnings)
}

// shortGCPName trims the projects/.../locations/.../thing/NAME prefix Google
// returns, because the full resource path is not what an operator recognises.
func shortGCPName(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return name
}
