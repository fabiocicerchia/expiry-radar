package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DigitalOceanSource reports the certificates attached to load balancers and
// apps in a DigitalOcean account.
//
// Deliberately small. DigitalOcean hosts DNS but is not a registrar, so there
// are no domain registrations to report, and its personal access tokens have no
// list endpoint. One endpoint is the honest extent of what this API exposes
// that expires, and saying so is better than padding it out.
type DigitalOceanSource struct {
	// Token never comes from the config file; Load fills it from
	// $DIGITALOCEAN_TOKEN. A read-only token is enough.
	Token   string
	BaseURL string // test seam; empty means the real API
	Timeout time.Duration
}

const digitalOceanAPI = "https://api.digitalocean.com"

// Name identifies this source in an item's Source field.
func (s *DigitalOceanSource) Name() string { return "digitalocean" }

type doCertificate struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	NotAfter string   `json:"not_after"`
	DNSNames []string `json:"dns_names"`
	State    string   `json:"state"`
	Type     string   `json:"type"`
}

// Collect reads the account's certificates.
func (s *DigitalOceanSource) Collect(ctx context.Context) ([]Item, error) {
	if s.Token == "" {
		return nil, fmt.Errorf("digitalocean source is enabled but $DIGITALOCEAN_TOKEN is not set")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	base := s.BaseURL
	if base == "" {
		base = digitalOceanAPI
	}

	var body struct {
		Certificates []doCertificate `json:"certificates"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimSuffix(base, "/")+"/v2/certificates?per_page=200", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck // the body is read or abandoned either way.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("GET /v2/certificates: %s — the token needs read access", resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /v2/certificates: %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	var items []Item
	for _, c := range body.Certificates {
		expires, ok := cfTime(c.NotAfter)
		if !ok {
			continue
		}
		labels := map[string]string{LabelPublic: "true"}
		labels = label(labels, LabelHosts, strings.Join(c.DNSNames, ","))
		labels = label(labels, "state", c.State)
		labels = label(labels, "cert-type", c.Type)
		// DigitalOcean renews its own Let's Encrypt certificates; an uploaded
		// custom one is nobody's job but yours.
		if strings.EqualFold(c.Type, "lets_encrypt") && strings.EqualFold(c.State, "verified") {
			labels[LabelRenewal] = RenewalManaged
		}
		name := c.Name
		if name == "" {
			name = c.ID
		}
		items = append(items, Item{
			Kind:    KindTLSCert,
			Name:    name,
			Expires: expires,
			Source:  "digitalocean:certificate",
			Labels:  labels,
		})
	}
	return items, nil
}
