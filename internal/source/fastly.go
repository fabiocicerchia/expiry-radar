package source

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// FastlySource reports TLS certificates and API tokens.
//
// Fastly is Cloudflare's closest competitor here and exposes the same two
// classes of thing, with one difference worth knowing: its API tokens can be
// scoped to individual services, so an expiry is a blast radius question and
// the scope says how wide.
type FastlySource struct {
	// Token never comes from the config file; Load fills it from
	// $FASTLY_API_TOKEN.
	Token   string
	BaseURL string // test seam
	Timeout time.Duration

	SkipCertificates bool
	SkipTokens       bool
}

const fastlyAPI = "https://api.fastly.com"

// Name identifies this source in an item's Source field.
func (s *FastlySource) Name() string { return "fastly" }

// Collect reads certificates and tokens.
func (s *FastlySource) Collect(ctx context.Context) ([]Item, error) {
	if s.Token == "" {
		return nil, fmt.Errorf("fastly source is enabled but $FASTLY_API_TOKEN is not set")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	items, _, err := collectUnits([]collectUnit{
		{"certificates", s.SkipCertificates, func() ([]Item, error) { return s.certificates(ctx, client) }},
		{"tokens", s.SkipTokens, func() ([]Item, error) { return s.tokens(ctx, client) }},
	})
	return items, err
}

func (s *FastlySource) base() string {
	if s.BaseURL != "" {
		return strings.TrimSuffix(s.BaseURL, "/")
	}
	return fastlyAPI
}

func (s *FastlySource) headers() map[string]string {
	// Fastly's own header, not Authorization.
	return map[string]string{"Fastly-Key": s.Token}
}

// certificates reads the TLS platform. The response is JSON:API, so the useful
// fields sit under `attributes` rather than at the top level.
func (s *FastlySource) certificates(ctx context.Context, client *http.Client) ([]Item, error) {
	var body struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Name     string `json:"name"`
				NotAfter string `json:"not_after"`
				IssuedTo string `json:"issued_to"`
				Issuer   string `json:"issuer"`
			} `json:"attributes"`
		} `json:"data"`
		Meta struct {
			TotalPages int `json:"total_pages"`
		} `json:"meta"`
	}
	u := s.base() + "/tls/certificates?page[size]=200"
	if err := getJSON(ctx, client, u, s.headers(),
		"the token needs the global:read scope", &body); err != nil {
		return nil, err
	}

	var items []Item
	for _, c := range body.Data {
		expires, ok := cfTime(c.Attributes.NotAfter)
		if !ok {
			continue
		}
		name := c.Attributes.Name
		if name == "" {
			name = c.Attributes.IssuedTo
		}
		if name == "" {
			name = c.ID
		}
		labels := map[string]string{LabelPublic: "true"}
		labels = label(labels, LabelHosts, c.Attributes.IssuedTo)
		labels = label(labels, LabelIssuer, c.Attributes.Issuer)
		items = append(items, Item{
			Kind:    KindTLSCert,
			Name:    name,
			Expires: expires,
			Source:  "fastly:certificate",
			Labels:  labels,
		})
	}
	if body.Meta.TotalPages > 1 {
		return items, fmt.Errorf("read one page of %d; the rest were not fetched", body.Meta.TotalPages)
	}
	return items, nil
}

// tokens reports API tokens. Unlike most providers Fastly scopes these, so the
// scope is the blast radius: a global token is not a read-only one.
func (s *FastlySource) tokens(ctx context.Context, client *http.Client) ([]Item, error) {
	var tokens []struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		ExpiresAt string   `json:"expires_at"`
		Scope     string   `json:"scope"`
		Services  []string `json:"services"`
	}
	u := s.base() + "/tokens"
	if err := getJSON(ctx, client, u, s.headers(),
		"listing tokens needs an account-level credential", &tokens); err != nil {
		return nil, err
	}

	var items []Item
	for _, t := range tokens {
		expires, ok := cfTime(t.ExpiresAt)
		if !ok {
			continue // a Fastly token need not expire
		}
		name := t.Name
		if name == "" {
			name = t.ID
		}
		labels := map[string]string{}
		labels = label(labels, "scope", t.Scope)
		// A token pinned to specific services cannot touch the rest.
		if len(t.Services) > 0 {
			labels = label(labels, "services", strings.Join(t.Services, ","))
		} else if strings.Contains(strings.ToLower(t.Scope), "global") {
			labels = label(labels, LabelBlastRadius, "0.85")
		}
		items = append(items, Item{
			Kind:    KindIAMKey,
			Name:    "token/" + name,
			Expires: expires,
			Source:  "fastly:token",
			Labels:  labels,
		})
	}
	return items, nil
}
