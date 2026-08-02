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

// DomainSource reads registrar expiry over RDAP — the protocol that replaced
// WHOIS. It is JSON, unauthenticated, and rdap.org redirects to the right
// registry for the TLD, so this needs no registrar API keys and no scraping.
//
// A lapsed domain is the one expiry that takes everything down at once, which
// is why it ranks high by default.
type DomainSource struct {
	Domains   []string
	Bootstrap string // override the RDAP bootstrap service
	Timeout   time.Duration
}

const defaultRDAPBootstrap = "https://rdap.org"

func (s *DomainSource) Name() string { return "domain:rdap" }

func (s *DomainSource) Collect(ctx context.Context) ([]Item, error) {
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	base := s.Bootstrap
	if base == "" {
		base = defaultRDAPBootstrap
	}
	client := &http.Client{Timeout: timeout}

	var items []Item
	var errs []string
	for _, domain := range s.Domains {
		expires, err := s.lookup(ctx, client, base, domain)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", domain, err))
			continue
		}
		items = append(items, Item{
			Kind:    KindDomain,
			Name:    domain,
			Expires: expires,
			Source:  "domain:rdap",
			Labels:  map[string]string{LabelPublic: "true"},
		})
	}
	if len(errs) > 0 {
		return items, fmt.Errorf("%d domain(s) failed: %s", len(errs), strings.Join(errs, "; "))
	}
	return items, nil
}

func (s *DomainSource) lookup(ctx context.Context, client *http.Client, base, domain string) (time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+"/domain/"+url.PathEscape(domain), nil)
	if err != nil {
		return time.Time{}, err
	}
	req.Header.Set("Accept", "application/rdap+json")
	resp, err := client.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("RDAP lookup: %s", resp.Status)
	}
	var body rdapDomain
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return time.Time{}, err
	}
	return body.expiration()
}

type rdapDomain struct {
	Events []struct {
		Action string `json:"eventAction"`
		Date   string `json:"eventDate"`
	} `json:"events"`
}

func (d rdapDomain) expiration() (time.Time, error) {
	for _, e := range d.Events {
		if e.Action != "expiration" {
			continue
		}
		// Registries are inconsistent about the timezone suffix, so try the
		// strict form first and fall back to the common sloppy one.
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05"} {
			if t, err := time.Parse(layout, e.Date); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("unparseable expiration date %q", e.Date)
	}
	return time.Time{}, fmt.Errorf("no expiration event (some registries hide it from anonymous queries)")
}
