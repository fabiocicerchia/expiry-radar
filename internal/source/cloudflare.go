package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// CloudflareSource reports everything a Cloudflare account knows expires.
//
// It is the richest single token in the tool: one read-scoped API token reaches
// edge certificates, uploaded custom certificates, mTLS client certificates,
// Zero Trust service tokens, registrar domains and the API tokens themselves.
// It is also the best-behaved source for ranking, because Cloudflare knows
// things the others do not — a zone name is the hostname, and a proxied zone is
// internet-facing by definition, so `public` and `hosts` come for free rather
// than being inferred from a namespace.
//
// Read-only, like every source. Every call is a GET, and a token scoped to the
// *:read permissions is enough — see docs/cloudflare-readonly.md.
type CloudflareSource struct {
	// Token never comes from the config file; Load fills it from
	// $CLOUDFLARE_API_TOKEN. A token in a config file is a token in a git repo.
	Token string
	// AccountID scopes the registrar and Zero Trust reads. Empty skips them:
	// they are account-level and there is nothing to guess.
	AccountID string
	// Zones limits the scan to these zone IDs. Empty means every zone the token
	// can see.
	Zones []string
	// BaseURL is a test seam; empty means the real API.
	BaseURL string
	Timeout time.Duration

	// MaxKeyAgeDays is the rotation policy for API tokens created without an
	// expiry. There is no default: these have no deadline of their own, so the
	// policy is the only one there is, and a deadline nobody chose is not a
	// policy. Unset, such tokens stay out of the report.
	MaxKeyAgeDays int

	SkipZones   bool
	SkipAccount bool
	SkipUser    bool
	// SkipAccountTokens turns off the account-owned token read on its own.
	// Separate from SkipAccount because the rest of the account scope needs no
	// extra permission, and this read needs "Account API Tokens Read".
	SkipAccountTokens bool
}

const (
	cloudflareAPI      = "https://api.cloudflare.com/client/v4"
	cloudflarePageSize = 50
	// A page cap, not a result cap: pagination that never terminates is a bug
	// somewhere, and looping forever is a worse failure than reporting less.
	cloudflareMaxPages = 40
)

// Name identifies this source in an item's Source field.
func (s *CloudflareSource) Name() string { return "cloudflare" }

// Collect reads every expiring thing this token can see.
func (s *CloudflareSource) Collect(ctx context.Context) ([]Item, error) {
	if s.Token == "" {
		return nil, fmt.Errorf("cloudflare source is enabled but $CLOUDFLARE_API_TOKEN is not set")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	// Same seam as the AWS and Kubernetes sources: one denied permission must
	// not lose the others' findings, and a token scoped to certificates only is
	// the normal case rather than an error.
	items, _, err := collectUnits([]collectUnit{
		{"zones", s.SkipZones, func() ([]Item, error) { return s.zoneItems(ctx, client) }},
		{"account", s.SkipAccount || s.AccountID == "",
			func() ([]Item, error) { return s.accountItems(ctx, client) }},
		{"user", s.SkipUser, func() ([]Item, error) { return s.userTokens(ctx, client) }},
	})
	return items, err
}

// cfResult is the envelope every Cloudflare endpoint returns. `success` is
// checked rather than the status code alone: the API answers 200 with
// success=false often enough that trusting the code would swallow errors.
type cfResult[T any] struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result     []T `json:"result"`
	ResultInfo struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

// cfGet reads every page of a list endpoint. A free function rather than a
// method because Go does not allow type parameters on methods.
func cfGet[T any](ctx context.Context, s *CloudflareSource, client *http.Client, path string) ([]T, error) {
	base := s.BaseURL
	if base == "" {
		base = cloudflareAPI
	}
	base = strings.TrimSuffix(base, "/")

	var out []T
	for page := 1; page <= cloudflareMaxPages; page++ {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		u := base + path + sep +
			"page=" + strconv.Itoa(page) + "&per_page=" + strconv.Itoa(cloudflarePageSize)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return out, err
		}
		req.Header.Set("Authorization", "Bearer "+s.Token)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return out, err
		}
		var body cfResult[T]
		decErr := json.NewDecoder(resp.Body).Decode(&body)
		//nolint:errcheck // the body is read or abandoned either way.
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			return out, fmt.Errorf("GET %s: %s — the API token is missing a read permission for this resource",
				path, resp.Status)
		}
		if resp.StatusCode != http.StatusOK {
			return out, fmt.Errorf("GET %s: %s", path, resp.Status)
		}
		if decErr != nil {
			return out, fmt.Errorf("GET %s: %w", path, decErr)
		}
		if !body.Success {
			return out, fmt.Errorf("GET %s: %s", path, cfErrors(body.Errors))
		}

		out = append(out, body.Result...)
		if body.ResultInfo.TotalPages <= page || len(body.Result) == 0 {
			return out, nil
		}
	}
	return out, fmt.Errorf("GET %s: stopped after %d pages", path, cloudflareMaxPages)
}

func cfErrors(errs []struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}) string {
	if len(errs) == 0 {
		return "the API reported failure with no error detail"
	}
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, fmt.Sprintf("%d %s", e.Code, e.Message))
	}
	return strings.Join(msgs, "; ")
}

type cfZone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Paused bool   `json:"paused"`
}

// zoneItems walks the zones and reads the certificates in each.
//
// One zone failing must not lose the others, so the per-zone errors accumulate
// the way the AWS paginators and the Kubernetes namespace fan-out already do.
func (s *CloudflareSource) zoneItems(ctx context.Context, client *http.Client) ([]Item, error) {
	// zoneList returns what it could read alongside its error, so taking only
	// the error here would let one unreadable zone id lose every other zone's
	// certificates — the rule this source is supposed to keep.
	zones, zoneErr := s.zoneList(ctx, client)
	if len(zones) == 0 && zoneErr != nil {
		return nil, zoneErr
	}

	var items []Item
	var warnings []string
	if zoneErr != nil {
		warnings = append(warnings, zoneErr.Error())
	}
	for _, z := range zones {
		got, zErr := s.certsForZone(ctx, client, z)
		items = append(items, got...)
		if zErr != nil {
			warnings = append(warnings, z.Name+": "+zErr.Error())
		}
	}
	return items, joinErrs(warnings)
}

func (s *CloudflareSource) zoneList(ctx context.Context, client *http.Client) ([]cfZone, error) {
	if len(s.Zones) == 0 {
		return cfGet[cfZone](ctx, s, client, "/zones")
	}
	// Configured zone IDs still get looked up, because the zone name is the
	// hostname every certificate in it is ranked by.
	var out []cfZone
	var warnings []string
	for _, id := range s.Zones {
		got, err := cfGet[cfZone](ctx, s, client, "/zones?id="+url.QueryEscape(id))
		if err != nil {
			warnings = append(warnings, id+": "+err.Error())
			continue
		}
		if len(got) == 0 {
			warnings = append(warnings, id+": no such zone, or the token cannot see it")
			continue
		}
		out = append(out, got...)
	}
	if len(warnings) > 0 {
		return out, joinErrs(warnings)
	}
	return out, nil
}

type cfCertificatePack struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Hosts        []string `json:"hosts"`
	Status       string   `json:"status"`
	Certificates []struct {
		ID        string   `json:"id"`
		Hosts     []string `json:"hosts"`
		Issuer    string   `json:"issuer"`
		Status    string   `json:"status"`
		ExpiresOn string   `json:"expires_on"`
	} `json:"certificates"`
}

type cfCustomCertificate struct {
	ID        string   `json:"id"`
	Hosts     []string `json:"hosts"`
	Issuer    string   `json:"issuer"`
	Status    string   `json:"status"`
	ExpiresOn string   `json:"expires_on"`
}

type cfClientCertificate struct {
	ID         string `json:"id"`
	CommonName string `json:"common_name"`
	Status     string `json:"status"`
	ExpiresOn  string `json:"expires_on"`
}

func (s *CloudflareSource) certsForZone(ctx context.Context, client *http.Client, z cfZone) ([]Item, error) {
	var items []Item
	var warnings []string
	zid := url.PathEscape(z.ID)

	// Edge certificates. A pack holds one certificate per signature algorithm,
	// and each carries its own date, so each is its own row.
	packs, err := cfGet[cfCertificatePack](ctx, s, client, "/zones/"+zid+"/ssl/certificate_packs?status=all")
	if err != nil {
		warnings = append(warnings, "certificate packs: "+err.Error())
	}
	for _, p := range packs {
		for _, c := range p.Certificates {
			expires, ok := cfTime(c.ExpiresOn)
			if !ok {
				continue
			}
			hosts := c.Hosts
			if len(hosts) == 0 {
				hosts = p.Hosts
			}
			it := s.certItem(z, "cloudflare:edge", hosts, c.Issuer, c.Status, expires)
			it.Labels = label(it.Labels, "pack-type", p.Type)
			// Cloudflare renews a universal or advanced pack itself. That is
			// the same evidence cert-manager gives: a deadline something else
			// is meeting is not one you have to act on.
			if p.Type != "custom" && strings.EqualFold(c.Status, "active") {
				it.Labels = label(it.Labels, LabelRenewal, RenewalManaged)
			}
			items = append(items, it)
		}
	}

	// Uploaded custom certificates. Nobody renews these for you, which is
	// exactly why they are the ones that lapse.
	customs, err := cfGet[cfCustomCertificate](ctx, s, client, "/zones/"+zid+"/custom_certificates")
	if err != nil {
		warnings = append(warnings, "custom certificates: "+err.Error())
	}
	for _, c := range customs {
		expires, ok := cfTime(c.ExpiresOn)
		if !ok {
			continue
		}
		items = append(items, s.certItem(z, "cloudflare:custom", c.Hosts, c.Issuer, c.Status, expires))
	}

	// mTLS client certificates: these authenticate callers, so an expiry is a
	// caller locked out rather than a site down.
	clients, err := cfGet[cfClientCertificate](ctx, s, client, "/zones/"+zid+"/client_certificates")
	if err != nil {
		warnings = append(warnings, "client certificates: "+err.Error())
	}
	for _, c := range clients {
		expires, ok := cfTime(c.ExpiresOn)
		if !ok {
			continue
		}
		name := c.CommonName
		if name == "" {
			name = c.ID
		}
		items = append(items, Item{
			Kind:      KindTLSCert,
			Name:      z.Name + "/mtls/" + name,
			Expires:   expires,
			Source:    "cloudflare:mtls",
			Namespace: z.Name,
			Labels:    label(map[string]string{"status": c.Status}, LabelHosts, name),
		})
	}

	return items, joinErrs(warnings)
}

// certItem builds the shape the three certificate endpoints share, including
// the ranking evidence Cloudflare can supply that no other source can.
func (s *CloudflareSource) certItem(z cfZone, src string, hosts []string,
	issuer, status string, expires time.Time) Item {
	labels := map[string]string{}
	labels = label(labels, LabelHosts, strings.Join(hosts, ","))
	labels = label(labels, LabelIssuer, issuer)
	labels = label(labels, "status", status)
	labels = label(labels, "zone-status", z.Status)
	// A zone served by Cloudflare is reachable from the internet by
	// definition — this is the one source that does not have to infer it.
	if !z.Paused && strings.EqualFold(z.Status, "active") {
		labels[LabelPublic] = "true"
	}
	// A paused zone is not serving, so its certificates are not load-bearing.
	if z.Paused {
		labels[LabelInUse] = "false"
	}

	name := z.Name
	if len(hosts) == 1 && hosts[0] != "" {
		name = hosts[0]
	} else if len(hosts) > 1 {
		name = z.Name + " (" + strconv.Itoa(len(hosts)) + " hosts)"
	}
	return Item{
		Kind:      KindTLSCert,
		Name:      name,
		Expires:   expires,
		Source:    src,
		Namespace: z.Name,
		Labels:    labels,
	}
}

type cfRegistrarDomain struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ExpiresAt string `json:"expires_at"`
	AutoRenew bool   `json:"auto_renew"`
	Locked    bool   `json:"locked"`
}

type cfServiceToken struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ClientID  string `json:"client_id"`
	ExpiresAt string `json:"expires_at"`
}

func (s *CloudflareSource) accountItems(ctx context.Context, client *http.Client) ([]Item, error) {
	var items []Item
	var warnings []string
	acct := url.PathEscape(s.AccountID)

	domains, err := cfGet[cfRegistrarDomain](ctx, s, client, "/accounts/"+acct+"/registrar/domains")
	if err != nil {
		warnings = append(warnings, "registrar: "+err.Error())
	}
	for _, d := range domains {
		expires, ok := cfTime(d.ExpiresAt)
		if !ok {
			continue
		}
		name := d.Name
		if name == "" {
			name = d.ID
		}
		labels := map[string]string{LabelPublic: "true"}
		// Auto-renew is the registrar equivalent of a healthy cert-manager
		// renewal: the date is still real, but somebody else is meeting it.
		if d.AutoRenew {
			labels[LabelRenewal] = RenewalManaged
		}
		items = append(items, Item{
			Kind:      KindDomain,
			Name:      name,
			Expires:   expires,
			Source:    "cloudflare:registrar",
			Namespace: name,
			Labels:    labels,
		})
	}

	// Zero Trust service tokens default to a one-year life, are used by
	// machines that nobody watches, and take out whatever they authenticate.
	tokens, err := cfGet[cfServiceToken](ctx, s, client, "/accounts/"+acct+"/access/service_tokens")
	if err != nil {
		warnings = append(warnings, "access service tokens: "+err.Error())
	}
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
			Kind:    KindSecret,
			Name:    "access/" + name,
			Expires: expires,
			Source:  "cloudflare:access",
			Labels:  map[string]string{"client-id": t.ClientID},
		})
	}

	// The account's own API tokens. Last, because it is the one read here that
	// needs a permission the others do not — a token without "Account API
	// Tokens Read" warns and the registrar and Zero Trust findings still
	// arrive, which is the per-scope rule this source is built on.
	if !s.SkipAccountTokens {
		owned, err := s.accountTokens(ctx, client, acct)
		if err != nil {
			warnings = append(warnings, "account tokens: "+err.Error())
		}
		items = append(items, owned...)
	}

	return items, joinErrs(warnings)
}

type cfAPIToken struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	IssuedOn  string `json:"issued_on"`
	ExpiresOn string `json:"expires_on"`
}

// userTokens reports the API tokens themselves, including the one doing the
// reading: when it lapses, every other Cloudflare finding in this report stops
// arriving, and the report goes quiet rather than wrong.
//
// User-owned tokens are only half of them, and on many accounts the empty
// half. See accountTokens.
func (s *CloudflareSource) userTokens(ctx context.Context, client *http.Client) ([]Item, error) {
	tokens, err := cfGet[cfAPIToken](ctx, s, client, "/user/tokens")
	if err != nil {
		return nil, err
	}
	return s.tokenItems(tokens, "cloudflare:token"), nil
}

// accountTokens reports the account-owned API tokens, which /user/tokens never
// returns.
//
// Cloudflare has two token stores and one of them was unreadable here. An
// account whose credentials were all created under Manage Account > API Tokens
// reported nothing at all, and the source looked like it had simply found
// nothing to say — on one real estate, six live tokens including the one its
// own Terraform authenticates with.
//
// It is also the half an API token can actually read. /user/tokens requires
// user-level auth (the Global API Key); this endpoint does not, so a scoped
// read-only token can inventory it.
func (s *CloudflareSource) accountTokens(ctx context.Context, client *http.Client, acct string) ([]Item, error) {
	tokens, err := cfGet[cfAPIToken](ctx, s, client, "/accounts/"+acct+"/tokens")
	if err != nil {
		return nil, err
	}
	return s.tokenItems(tokens, "cloudflare:account-token"), nil
}

// tokenItems turns either store's tokens into items, so the two cannot drift.
func (s *CloudflareSource) tokenItems(tokens []cfAPIToken, source string) []Item {
	var items []Item
	for _, t := range tokens {
		name := t.Name
		if name == "" {
			name = t.ID
		}
		labels := map[string]string{"status": t.Status}
		if !strings.EqualFold(t.Status, "active") {
			labels[LabelInUse] = "false"
		}

		expires, ok := cfTime(t.ExpiresOn)
		if !ok {
			// A token with no expiry has no deadline to miss, and that is
			// exactly what makes it worth watching — it is the one credential
			// that will still be valid the day it leaks. Reporting it needs a
			// date, and the only honest one is a rotation policy the operator
			// chose: maxKeyAgeDays, applied to the day it was issued.
			//
			// No default, for the reason rotation.go gives about access keys:
			// a deadline nobody chose is not a policy, and inventing one puts
			// a date in the report that no human ever agreed to. Unset, these
			// stay out — as they were before, but now by choice rather than
			// because the field was empty.
			issued, iok := cfTime(t.IssuedOn)
			if s.MaxKeyAgeDays <= 0 || !iok {
				continue
			}
			expires = issued.Add(time.Duration(s.MaxKeyAgeDays) * 24 * time.Hour)
			// The same three labels rotationItem writes, so a synthesised
			// deadline reads identically wherever it came from.
			labels["created"] = issued.Format(time.RFC3339)
			labels["policy.days"] = strconv.Itoa(s.MaxKeyAgeDays)
			labels["deadline"] = "rotation policy"
		}

		items = append(items, Item{
			Kind:    KindIAMKey,
			Name:    "token/" + name,
			Expires: expires,
			Source:  source,
			Labels:  labels,
		})
	}
	return items
}

// cfTime parses Cloudflare's timestamps. They are RFC 3339 with six fractional
// digits ("2027-03-07T23:26:12.000000Z"), which time.RFC3339 already accepts.
// An empty or unparsable value means there is no deadline to report, not an
// error — the same rule Secrets Manager rotation follows.
func cfTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
