package source

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HetznerSource reports the certificates attached to Hetzner Cloud load
// balancers.
//
// Small by nature, like DigitalOcean: Hetzner is not a registrar and its API
// tokens have no list endpoint, so certificates are the whole of what this API
// exposes with a date on it.
type HetznerSource struct {
	// Token never comes from the config file; Load fills it from
	// $HCLOUD_TOKEN.
	Token   string
	BaseURL string // test seam
	Timeout time.Duration
}

const hetznerAPI = "https://api.hetzner.cloud"

// Name identifies this source in an item's Source field.
func (s *HetznerSource) Name() string { return "hetzner" }

// Collect reads the project's certificates.
func (s *HetznerSource) Collect(ctx context.Context) ([]Item, error) {
	if s.Token == "" {
		return nil, fmt.Errorf("hetzner source is enabled but $HCLOUD_TOKEN is not set")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	base := s.BaseURL
	if base == "" {
		base = hetznerAPI
	}

	var body struct {
		Certificates []struct {
			ID            int      `json:"id"`
			Name          string   `json:"name"`
			Type          string   `json:"type"`
			DomainNames   []string `json:"domain_names"`
			NotValidAfter string   `json:"not_valid_after"`
			Status        *struct {
				Issuance string `json:"issuance"`
				Renewal  string `json:"renewal"`
			} `json:"status"`
		} `json:"certificates"`
		Meta struct {
			Pagination struct {
				TotalEntries int `json:"total_entries"`
			} `json:"pagination"`
		} `json:"meta"`
	}
	u := strings.TrimSuffix(base, "/") + "/v1/certificates?per_page=50"
	err := getJSON(ctx, &http.Client{Timeout: timeout}, u,
		map[string]string{"Authorization": "Bearer " + s.Token},
		"the API token needs read access to this project", &body)
	if err != nil {
		return nil, err
	}

	var items []Item
	for _, c := range body.Certificates {
		expires, ok := cfTime(c.NotValidAfter)
		if !ok {
			continue
		}
		name := c.Name
		if name == "" {
			name = fmt.Sprintf("certificate-%d", c.ID)
		}
		labels := map[string]string{LabelPublic: "true"}
		labels = label(labels, LabelHosts, strings.Join(c.DomainNames, ","))
		labels = label(labels, "cert-type", c.Type)
		// Hetzner renews its own managed certificates; an uploaded one is
		// nobody's job but yours.
		if strings.EqualFold(c.Type, "managed") {
			labels[LabelRenewal] = RenewalManaged
			if c.Status != nil && c.Status.Renewal != "" &&
				!strings.EqualFold(c.Status.Renewal, "scheduled") &&
				!strings.EqualFold(c.Status.Renewal, "completed") {
				// Managed, but the renewal is not healthy — so the de-rank is
				// not earned.
				delete(labels, LabelRenewal)
				labels = label(labels, LabelRenewal, RenewalStuck)
			}
		}
		items = append(items, Item{
			Kind:    KindTLSCert,
			Name:    name,
			Expires: expires,
			Source:  "hetzner:certificate",
			Labels:  labels,
		})
	}
	if t := truncationWarning("certificates", len(body.Certificates),
		body.Meta.Pagination.TotalEntries); t != nil {
		return items, t
	}
	return items, nil
}
