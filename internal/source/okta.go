package source

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OktaSource reports API tokens and SAML application signing certificates.
//
// Okta is unusual in stating an expiry for both. API tokens carry `expiresAt`
// outright, and an application's signing key is a real certificate whose
// NotAfter can be read from the JWK the API returns — so nothing here has to be
// inferred or synthesised from a policy.
//
// The signing certificates are the reason to bother. When one lapses, every
// sign-in through that application stops at once, and it does not present as a
// certificate problem to anybody looking at it from the application's side.
type OktaSource struct {
	// OrgURL is the tenant, e.g. https://acme.okta.com.
	OrgURL string
	// Token never comes from the config file; Load fills it from
	// $OKTA_API_TOKEN. A read-only admin token is enough.
	Token   string
	Timeout time.Duration

	SkipTokens bool
	SkipApps   bool
}

// Name identifies this source in an item's Source field.
func (s *OktaSource) Name() string { return "okta" }

// Collect reads API tokens and application signing certificates.
func (s *OktaSource) Collect(ctx context.Context) ([]Item, error) {
	if s.OrgURL == "" {
		return nil, fmt.Errorf("okta source needs orgUrl, e.g. https://acme.okta.com")
	}
	if s.Token == "" {
		return nil, fmt.Errorf("okta source is enabled but $OKTA_API_TOKEN is not set")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	items, _, err := collectUnits([]collectUnit{
		{"api-tokens", s.SkipTokens, func() ([]Item, error) { return s.apiTokens(ctx, client) }},
		{"apps", s.SkipApps, func() ([]Item, error) { return s.appCertificates(ctx, client) }},
	})
	return items, err
}

func oktaGet[T any](ctx context.Context, s *OktaSource, client *http.Client, path string) ([]T, error) {
	u := strings.TrimSuffix(s.OrgURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	// SSWS is Okta's scheme, not Bearer.
	req.Header.Set("Authorization", "SSWS "+s.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck // the body is read or abandoned either way.
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("GET %s: %s — the API token needs a read-only admin role", path, resp.Status)
	default:
		return nil, fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	var out []T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	return out, nil
}

type oktaAPIToken struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ExpiresAt string `json:"expiresAt"`
}

// apiTokens reports the org's API tokens, including the one doing the reading.
// When that lapses every other Okta finding stops arriving.
func (s *OktaSource) apiTokens(ctx context.Context, client *http.Client) ([]Item, error) {
	tokens, err := oktaGet[oktaAPIToken](ctx, s, client, "/api/v1/api-tokens")
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, t := range tokens {
		expires, ok := cfTime(t.ExpiresAt)
		if !ok {
			continue
		}
		name := t.Name
		if name == "" {
			name = t.ID
		}
		items = append(items, Item{
			Kind:    KindIAMKey,
			Name:    "token/" + name,
			Expires: expires,
			Source:  "okta:api-token",
			// An Okta admin token can read and change the directory every
			// other system federates against.
			Labels: map[string]string{LabelBlastRadius: "0.85"},
		})
	}
	return items, nil
}

type oktaApp struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	SignOnMode string `json:"signOnMode"`
}

type oktaKey struct {
	Kid string   `json:"kid"`
	X5c []string `json:"x5c"`
}

// appCertificates reads the signing key of every SAML application.
//
// Only SAML applications are visited: they are the ones with a signing
// certificate, and restricting the fan-out to them keeps this from becoming one
// request per application in the org.
func (s *OktaSource) appCertificates(ctx context.Context, client *http.Client) ([]Item, error) {
	apps, err := oktaGet[oktaApp](ctx, s, client, "/api/v1/apps?limit=200")
	if err != nil {
		return nil, err
	}

	var items []Item
	var warnings []string
	for _, app := range apps {
		if !strings.Contains(strings.ToUpper(app.SignOnMode), "SAML") {
			continue
		}
		keys, err := oktaGet[oktaKey](ctx, s, client,
			"/api/v1/apps/"+url.PathEscape(app.ID)+"/credentials/keys")
		if err != nil {
			warnings = append(warnings, app.Label+": "+err.Error())
			continue
		}
		for _, k := range keys {
			cert, ok := firstX5C(k.X5c)
			if !ok {
				continue
			}
			appName := app.Label
			if appName == "" {
				appName = app.ID
			}
			labels := map[string]string{"kid": k.Kid}
			labels = label(labels, LabelIssuer, cert.Issuer.CommonName)
			labels = label(labels, "sign-on-mode", app.SignOnMode)
			// A deactivated app is not signing anybody in.
			if !strings.EqualFold(app.Status, "ACTIVE") {
				labels[LabelInUse] = "false"
			}
			items = append(items, Item{
				// Every sign-in through the app validates against this, and
				// nothing behind it fails gracefully.
				Kind:      KindTrustAnchor,
				Name:      "saml/" + appName,
				Expires:   cert.NotAfter,
				Source:    "okta:app-key",
				Namespace: appName,
				Labels:    labels,
			})
		}
	}
	return items, joinErrs(warnings)
}

// firstX5C parses the leading certificate of a JWK x5c chain, which is
// base64-encoded DER rather than PEM.
func firstX5C(x5c []string) (*x509.Certificate, bool) {
	for _, entry := range x5c {
		der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(entry))
		if err != nil {
			continue
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			continue
		}
		return cert, true
	}
	return nil, false
}
