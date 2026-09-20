package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// RegistrarSource inventories domain registrations across registrars.
//
// One source with four adapters rather than four sources, and the earlier plan
// set the condition for that: build the shared shape once it has actually held.
// It has. Namecheap, Scaleway, Route 53 and Cloudflare Registrar all reduce to
// the same three facts — a name, an expiry, and whether auto-renew is on — and
// so do these four. There is nothing per-registrar left to model.
//
// The value is the same everywhere too, and it is not the date: the `domains`
// source already reports registry expiry over RDAP with no credentials at all.
// What a registrar credential buys is knowing whether the renewal is actually
// going to happen.
type RegistrarSource struct {
	Providers []RegistrarProvider
	Timeout   time.Duration
	// BaseURLs overrides an adapter's endpoint, keyed by provider name. A test
	// seam.
	BaseURLs map[string]string
}

// RegistrarProvider is one registrar account to read.
type RegistrarProvider struct {
	// Name selects the adapter: see RegistrarProviders.
	Name string `json:"name"`
	// Account is DNSimple's account id. Unused by the others.
	Account string `json:"account"`
	// Token and Secret never come from the config file; Load fills them from
	// the provider's environment variables.
	Token  string `json:"-"`
	Secret string `json:"-"`
	// Labels are operator hints merged into every domain from this registrar.
	Labels map[string]string `json:"labels"`
}

// RegistrarProviders lists the adapters and the environment variables each
// reads. A second variable means the registrar needs a key *and* a secret.
var RegistrarProviders = map[string][2]string{
	"dnsimple": {"DNSIMPLE_TOKEN", ""},
	"gandi":    {"GANDI_API_KEY", ""},
	"porkbun":  {"PORKBUN_API_KEY", "PORKBUN_SECRET_KEY"},
	"godaddy":  {"GODADDY_API_KEY", "GODADDY_API_SECRET"},
}

// Name identifies this source in an item's Source field.
func (s *RegistrarSource) Name() string { return "registrar" }

// ValidateRegistrars rejects a provider that could never be read.
func ValidateRegistrars(providers []RegistrarProvider) error {
	for i, p := range providers {
		where := fmt.Sprintf("registrars[%d]", i)
		if p.Name == "" {
			return fmt.Errorf("%s: name is required (one of %s)", where, knownRegistrarNames())
		}
		if _, ok := RegistrarProviders[p.Name]; !ok {
			return fmt.Errorf("%s: unknown registrar %q, want one of %s", where, p.Name, knownRegistrarNames())
		}
		if p.Name == "dnsimple" && p.Account == "" {
			return fmt.Errorf("%s: dnsimple needs an account id — its domain list is account-scoped", where)
		}
	}
	return nil
}

func knownRegistrarNames() string {
	names := make([]string, 0, len(RegistrarProviders))
	for n := range RegistrarProviders {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// Collect reads every configured registrar.
func (s *RegistrarSource) Collect(ctx context.Context) ([]Item, error) {
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	units := make([]collectUnit, 0, len(s.Providers))
	for _, p := range s.Providers {
		units = append(units, collectUnit{
			Name:    p.Name,
			Collect: func() ([]Item, error) { return s.provider(ctx, client, p) },
		})
	}
	items, _, err := collectUnits(units)
	return items, err
}

// registrarDomain is the three facts every registrar agrees on.
type registrarDomain struct {
	Name      string
	Expires   time.Time
	AutoRenew bool
	Extra     map[string]string
}

func (s *RegistrarSource) provider(ctx context.Context, client *http.Client,
	p RegistrarProvider) ([]Item, error) {
	envs := RegistrarProviders[p.Name]
	if p.Token == "" {
		return nil, fmt.Errorf("no credential: set $%s", envs[0])
	}
	if envs[1] != "" && p.Secret == "" {
		return nil, fmt.Errorf("no secret: set $%s", envs[1])
	}

	// An adapter may return domains *and* an error — a truncated read is the
	// usual case — so both are kept. Taking only the error here would lose
	// everything that was read, which is the rule this whole tool is built on.
	var (
		domains []registrarDomain
		err     error
	)
	switch p.Name {
	case "dnsimple":
		domains, err = s.dnsimple(ctx, client, p)
	case "gandi":
		domains, err = s.gandi(ctx, client, p)
	case "porkbun":
		domains, err = s.porkbun(ctx, client, p)
	case "godaddy":
		domains, err = s.godaddy(ctx, client, p)
	default:
		return nil, fmt.Errorf("unknown registrar %q", p.Name)
	}

	items := make([]Item, 0, len(domains))
	for _, d := range domains {
		if d.Name == "" || d.Expires.IsZero() {
			continue
		}
		labels := map[string]string{LabelPublic: "true"}
		for k, v := range p.Labels {
			labels = label(labels, k, v)
		}
		for k, v := range d.Extra {
			labels = label(labels, k, v)
		}
		// The one thing RDAP cannot tell you, and the reason to hold the
		// credential at all.
		if d.AutoRenew {
			labels[LabelRenewal] = RenewalManaged
		}
		items = append(items, Item{
			Kind:      KindDomain,
			Name:      d.Name,
			Expires:   d.Expires,
			Source:    "registrar:" + p.Name,
			Namespace: d.Name,
			Labels:    labels,
		})
	}
	return items, err
}

func (s *RegistrarSource) base(provider, fallback string) string {
	if u, ok := s.BaseURLs[provider]; ok && u != "" {
		return strings.TrimSuffix(u, "/")
	}
	return fallback
}

// flexBool reads the several ways a registrar spells a boolean: a real bool,
// "1"/"0", "true"/"false", a number, or an object with an `enabled` field —
// all of which appear across these four APIs for the same auto-renew flag.
type flexBool bool

func (b *flexBool) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		*b = false
		return nil
	}
	switch raw[0] {
	case 't', 'f':
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		*b = flexBool(v)
	case '"':
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		v = strings.ToLower(strings.TrimSpace(v))
		*b = flexBool(v == "1" || v == "true" || v == "yes" || v == "enabled")
	case '{':
		var v struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		*b = flexBool(v.Enabled)
	default:
		var n float64
		if err := json.Unmarshal(raw, &n); err != nil {
			return err
		}
		*b = n != 0
	}
	return nil
}

func (s *RegistrarSource) dnsimple(ctx context.Context, client *http.Client,
	p RegistrarProvider) ([]registrarDomain, error) {
	var body struct {
		Data []struct {
			Name      string   `json:"name"`
			ExpiresAt string   `json:"expires_at"`
			AutoRenew flexBool `json:"auto_renew"`
			State     string   `json:"state"`
		} `json:"data"`
		Pagination struct {
			TotalEntries int `json:"total_entries"`
		} `json:"pagination"`
	}
	u := fmt.Sprintf("%s/v2/%s/domains?per_page=100",
		s.base("dnsimple", "https://api.dnsimple.com"), url.PathEscape(p.Account))
	if err := getJSON(ctx, client, u, map[string]string{"Authorization": "Bearer " + p.Token},
		"the token needs domain read access to this account", &body); err != nil {
		return nil, err
	}

	out := make([]registrarDomain, 0, len(body.Data))
	for _, d := range body.Data {
		expires, _ := cfTime(d.ExpiresAt)
		out = append(out, registrarDomain{
			Name: d.Name, Expires: expires, AutoRenew: bool(d.AutoRenew),
			Extra: map[string]string{"state": d.State},
		})
	}
	if t := truncationWarning("domains", len(body.Data), body.Pagination.TotalEntries); t != nil {
		return out, t
	}
	return out, nil
}

func (s *RegistrarSource) gandi(ctx context.Context, client *http.Client,
	p RegistrarProvider) ([]registrarDomain, error) {
	var domains []struct {
		FQDN  string `json:"fqdn"`
		Dates struct {
			RegistryEndsAt string `json:"registry_ends_at"`
		} `json:"dates"`
		AutoRenew flexBool `json:"autorenew"`
		Status    []string `json:"status"`
	}
	const perPage = 100
	u := fmt.Sprintf("%s/v5/domain/domains?per_page=%d",
		s.base("gandi", "https://api.gandi.net"), perPage)
	// Gandi's scheme is "Apikey", not Bearer.
	if err := getJSON(ctx, client, u, map[string]string{"Authorization": "Apikey " + p.Token},
		"the API key needs domain read access", &domains); err != nil {
		return nil, err
	}
	// Gandi returns no total, so a full page is the only signal that there may
	// be more — and saying nothing would match the DNSimple adapter's silence
	// rather than its check.
	var truncated error
	if len(domains) == perPage {
		truncated = fmt.Errorf("read a full page of %d domains; there may be more", perPage)
	}

	out := make([]registrarDomain, 0, len(domains))
	for _, d := range domains {
		expires, _ := cfTime(d.Dates.RegistryEndsAt)
		out = append(out, registrarDomain{
			Name: d.FQDN, Expires: expires, AutoRenew: bool(d.AutoRenew),
			Extra: map[string]string{"status": strings.Join(d.Status, ",")},
		})
	}
	return out, truncated
}

// porkbun is the odd one: credentials go in a JSON POST body rather than a
// header, which is its API's design and not a choice available here.
func (s *RegistrarSource) porkbun(ctx context.Context, client *http.Client,
	p RegistrarProvider) ([]registrarDomain, error) {
	payload, err := json.Marshal(map[string]string{
		"apikey": p.Token, "secretapikey": p.Secret,
	})
	if err != nil {
		return nil, err
	}
	u := s.base("porkbun", "https://api.porkbun.com") + "/api/json/v3/domain/listAll"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("porkbun listAll: %w", err)
	}
	//nolint:errcheck // the body is read or abandoned either way.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("porkbun listAll: %s", resp.Status)
	}

	var body struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Domains []struct {
			Domain     string   `json:"domain"`
			ExpireDate string   `json:"expireDate"`
			AutoRenew  flexBool `json:"autoRenew"`
			Status     string   `json:"status"`
		} `json:"domains"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("porkbun listAll: %w", err)
	}
	// Porkbun reports failure in the body with a 200, the same trap Cloudflare
	// and Namecheap set.
	if !strings.EqualFold(body.Status, "SUCCESS") {
		msg := body.Message
		if msg == "" {
			msg = "the API reported failure with no detail"
		}
		return nil, fmt.Errorf("porkbun listAll: %s", msg)
	}

	out := make([]registrarDomain, 0, len(body.Domains))
	for _, d := range body.Domains {
		out = append(out, registrarDomain{
			Name: d.Domain, Expires: porkbunTime(d.ExpireDate), AutoRenew: bool(d.AutoRenew),
			Extra: map[string]string{"status": d.Status},
		})
	}
	return out, nil
}

// porkbunTime reads "2027-03-01 12:00:00", which is neither RFC 3339 nor a
// bare date.
func porkbunTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, dateOnly} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (s *RegistrarSource) godaddy(ctx context.Context, client *http.Client,
	p RegistrarProvider) ([]registrarDomain, error) {
	var domains []struct {
		Domain    string   `json:"domain"`
		Expires   string   `json:"expires"`
		RenewAuto flexBool `json:"renewAuto"`
		Status    string   `json:"status"`
	}
	u := s.base("godaddy", "https://api.godaddy.com") + "/v1/domains?limit=1000"
	// GoDaddy's scheme puts the key and secret in one header value.
	err := getJSON(ctx, client, u,
		map[string]string{"Authorization": "sso-key " + p.Token + ":" + p.Secret},
		"the key needs domain read access — note GoDaddy gates its API on holding enough domains",
		&domains)
	if err != nil {
		return nil, err
	}

	out := make([]registrarDomain, 0, len(domains))
	for _, d := range domains {
		expires, _ := cfTime(d.Expires)
		out = append(out, registrarDomain{
			Name: d.Domain, Expires: expires, AutoRenew: bool(d.RenewAuto),
			Extra: map[string]string{"status": d.Status},
		})
	}
	return out, nil
}
