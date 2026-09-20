package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ScalewaySource reports registered domains, load-balancer certificates and
// IAM API keys.
//
// The simplest credential story of any cloud here: one secret key in an
// X-Auth-Token header, plain REST, no signing. Domains are the valuable half —
// Scaleway is a registrar, so unlike the RDAP source it can say whether the
// renewal was actually paid for, which is the difference between a date and a
// deadline.
type ScalewaySource struct {
	// SecretKey never comes from the config file; Load fills it from
	// $SCW_SECRET_KEY.
	SecretKey string
	// OrganizationID scopes the IAM key listing. Empty skips it rather than
	// guessing.
	OrganizationID string
	// Zones are load-balancer zones ("fr-par-1"). Empty skips LB certificates:
	// they are zonal and there is no cross-zone listing.
	Zones   []string
	BaseURL string // test seam
	Timeout time.Duration

	SkipDomains bool
	SkipLB      bool
	SkipKeys    bool
}

const scalewayAPI = "https://api.scaleway.com"

// Name identifies this source in an item's Source field.
func (s *ScalewaySource) Name() string { return "scaleway" }

// Collect reads domains, load-balancer certificates and IAM keys.
func (s *ScalewaySource) Collect(ctx context.Context) ([]Item, error) {
	if s.SecretKey == "" {
		return nil, fmt.Errorf("scaleway source is enabled but $SCW_SECRET_KEY is not set")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	items, _, err := collectUnits([]collectUnit{
		{"domains", s.SkipDomains, func() ([]Item, error) { return s.domains(ctx, client) }},
		{"loadbalancers", s.SkipLB || len(s.Zones) == 0,
			func() ([]Item, error) { return s.lbCertificates(ctx, client) }},
		{"iam", s.SkipKeys || s.OrganizationID == "",
			func() ([]Item, error) { return s.apiKeys(ctx, client) }},
	})
	return items, err
}

func scwGet(ctx context.Context, s *ScalewaySource, client *http.Client, path string, out any) error {
	base := s.BaseURL
	if base == "" {
		base = scalewayAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Auth-Token", s.SecretKey)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	//nolint:errcheck // the body is read or abandoned either way.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("GET %s: %s — the secret key is missing a read permission", path, resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type scwDomain struct {
	Domain          string `json:"domain"`
	ExpiredAt       string `json:"expired_at"`
	AutoRenewStatus string `json:"auto_renew_status"`
	Status          string `json:"status"`
}

// domains reports registrations. Unlike the RDAP source, which can only read
// the registry date, this knows whether auto-renew is switched on — the
// difference between "this date exists" and "somebody has to act on it".
func (s *ScalewaySource) domains(ctx context.Context, client *http.Client) ([]Item, error) {
	var body struct {
		Domains    []scwDomain `json:"domains"`
		TotalCount int         `json:"total_count"`
	}
	if err := scwGet(ctx, s, client, "/domain/v2beta1/domains?page_size=100", &body); err != nil {
		return nil, err
	}
	truncated := truncationWarning("domains", len(body.Domains), body.TotalCount)
	var items []Item
	for _, d := range body.Domains {
		expires, ok := cfTime(d.ExpiredAt)
		if !ok {
			continue
		}
		labels := map[string]string{LabelPublic: "true"}
		labels = label(labels, "status", d.Status)
		if strings.EqualFold(d.AutoRenewStatus, "enabled") {
			labels[LabelRenewal] = RenewalManaged
		}
		items = append(items, Item{
			Kind:      KindDomain,
			Name:      d.Domain,
			Expires:   expires,
			Source:    "scaleway:domain",
			Namespace: d.Domain,
			Labels:    labels,
		})
	}
	return items, truncated
}

type scwLBCert struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	NotValidAfter string `json:"not_valid_after"`
	Status        string `json:"status"`
}

func (s *ScalewaySource) lbCertificates(ctx context.Context, client *http.Client) ([]Item, error) {
	var items []Item
	var warnings []string
	for _, zone := range s.Zones {
		var body struct {
			Certificates []scwLBCert `json:"certificates"`
			TotalCount   int         `json:"total_count"`
		}
		path := "/lb/v1/zones/" + url.PathEscape(zone) + "/certificates?page_size=100"
		if err := scwGet(ctx, s, client, path, &body); err != nil {
			warnings = append(warnings, zone+": "+err.Error())
			continue
		}
		if t := truncationWarning(zone, len(body.Certificates), body.TotalCount); t != nil {
			warnings = append(warnings, t.Error())
		}
		for _, c := range body.Certificates {
			expires, ok := cfTime(c.NotValidAfter)
			if !ok {
				continue
			}
			name := c.Name
			if name == "" {
				name = c.ID
			}
			items = append(items, Item{
				Kind:      KindTLSCert,
				Name:      zone + "/" + name,
				Expires:   expires,
				Source:    "scaleway:lb",
				Namespace: zone,
				Labels: label(map[string]string{LabelPublic: "true"},
					"status", c.Status),
			})
		}
	}
	return items, joinErrs(warnings)
}

type scwAPIKey struct {
	AccessKey   string `json:"access_key"`
	Description string `json:"description"`
	ExpiresAt   string `json:"expires_at"`
	CreatedAt   string `json:"created_at"`
}

// apiKeys reports only the keys that carry an expiry. Scaleway allows keys
// without one, and those are a rotation-policy question rather than a deadline
// this source can read — the same line the AWS IAM adapter draws.
func (s *ScalewaySource) apiKeys(ctx context.Context, client *http.Client) ([]Item, error) {
	var body struct {
		APIKeys    []scwAPIKey `json:"api_keys"`
		TotalCount int         `json:"total_count"`
	}
	path := "/iam/v1alpha1/api-keys?page_size=100&organization_id=" + url.QueryEscape(s.OrganizationID)
	if err := scwGet(ctx, s, client, path, &body); err != nil {
		return nil, err
	}
	truncated := truncationWarning("api keys", len(body.APIKeys), body.TotalCount)
	var items []Item
	for _, k := range body.APIKeys {
		expires, ok := cfTime(k.ExpiresAt)
		if !ok {
			continue
		}
		name := k.Description
		if name == "" {
			name = k.AccessKey
		}
		items = append(items, Item{
			Kind:    KindIAMKey,
			Name:    name,
			Expires: expires,
			Source:  "scaleway:iam",
			Labels:  label(map[string]string{}, "created", k.CreatedAt),
		})
	}
	return items, truncated
}

// truncationWarning turns a page cap into a warning. Reading 100 of 240 and
// saying nothing looks exactly like an account that has 100.
func truncationWarning(what string, read, total int) error {
	if total > read {
		return fmt.Errorf("%s: read %d of %d; the rest were not fetched", what, read, total)
	}
	return nil
}
