package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RotationSource inventories credentials that have no expiry at all.
//
// Anthropic, OpenAI and Docker Hub all issue keys that simply never expire.
// There is no date to read, so there is nothing to discover in the sense the
// rest of this tool means it — what there is instead is an age, and a policy
// the operator holds about how old a key may get. This source turns the two
// into the deadline the ranking can order.
//
// That deadline is a **policy** deadline and must never read as an issuer's.
// Every item carries `created` and `policy.days` so the report says where the
// date came from, exactly as the AWS IAM adapter does for access keys — AWS
// will happily serve a five-year-old key, and so will these.
//
// Deliberately not a declarative HTTP driver. Three adapters sharing a core is
// the honest amount of machinery for three providers; a generic descriptor is
// worth building when the fourth and fifth arrive and the shape has held.
type RotationSource struct {
	Providers []RotationProvider
	Timeout   time.Duration
	// BaseURLs overrides a provider's endpoint, keyed by provider name. A test
	// seam: there is no reason to point these at anything else in production.
	BaseURLs map[string]string
}

// RotationProvider is one credential inventory to read.
type RotationProvider struct {
	// Name selects the adapter: see RotationProviders.
	Name string `json:"name"`
	// Token never comes from the config file; Load fills it from the
	// provider's environment variable.
	Token string `json:"-"`
	// MaxKeyAgeDays is the rotation policy. There is no default: a deadline
	// nobody chose is not a policy, and inventing one would put a date in the
	// report that no human ever agreed to.
	MaxKeyAgeDays int `json:"maxKeyAgeDays"`
	// Labels are operator hints, merged into every item from this provider —
	// `environment`, or a blast-radius override for a production key.
	Labels map[string]string `json:"labels"`
}

// RotationProviders lists the adapters, and the environment variable each
// reads its credential from.
var RotationProviders = map[string]string{
	"anthropic": "ANTHROPIC_ADMIN_KEY",
	"openai":    "OPENAI_ADMIN_KEY",
	"dockerhub": "DOCKERHUB_TOKEN",
}

// Name identifies this source in an item's Source field.
func (s *RotationSource) Name() string { return "rotation" }

// ValidateRotation rejects a provider that could never produce a usable
// deadline, at load rather than at collect.
func ValidateRotation(providers []RotationProvider) error {
	for i, p := range providers {
		where := fmt.Sprintf("rotation[%d]", i)
		if p.Name == "" {
			return fmt.Errorf("%s: name is required (one of %s)", where, knownRotationNames())
		}
		if _, ok := RotationProviders[p.Name]; !ok {
			return fmt.Errorf("%s: unknown provider %q, want one of %s", where, p.Name, knownRotationNames())
		}
		if p.MaxKeyAgeDays <= 0 {
			return fmt.Errorf(
				"%s (%s): maxKeyAgeDays is required and must be positive — these keys never expire, so the rotation policy is the only deadline there is",
				where, p.Name)
		}
	}
	return nil
}

func knownRotationNames() string {
	names := make([]string, 0, len(RotationProviders))
	for n := range RotationProviders {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// Collect reads every configured provider's credential inventory.
func (s *RotationSource) Collect(ctx context.Context) ([]Item, error) {
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	units := make([]collectUnit, 0, len(s.Providers))
	for _, p := range s.Providers {
		units = append(units, collectUnit{
			Name:    p.Name,
			Skipped: false,
			Collect: func() ([]Item, error) { return s.provider(ctx, client, p) },
		})
	}
	items, _, err := collectUnits(units)
	return items, err
}

// rotationKey is one credential, normalised across the three APIs.
type rotationKey struct {
	ID      string
	Name    string
	Created time.Time
	// Expires is set only when the provider actually states one. Docker Hub
	// grew expiring tokens later than the others, so both shapes occur.
	Expires *time.Time
	Active  bool
	Extra   map[string]string
}

func (s *RotationSource) provider(ctx context.Context, client *http.Client, p RotationProvider) ([]Item, error) {
	if p.Token == "" {
		return nil, fmt.Errorf("no credential: set $%s", RotationProviders[p.Name])
	}
	var (
		keys []rotationKey
		err  error
	)
	switch p.Name {
	case "anthropic":
		keys, err = s.anthropicKeys(ctx, client, p)
	case "openai":
		keys, err = s.openAIKeys(ctx, client, p)
	case "dockerhub":
		keys, err = s.dockerHubKeys(ctx, client, p)
	default:
		return nil, fmt.Errorf("unknown provider %q", p.Name)
	}
	if err != nil {
		return nil, err
	}

	maxAge := time.Duration(p.MaxKeyAgeDays) * 24 * time.Hour
	items := make([]Item, 0, len(keys))
	for _, k := range keys {
		items = append(items, rotationItem(p, k, maxAge))
	}
	return items, nil
}

// rotationItem synthesises the deadline and says so in the labels.
func rotationItem(p RotationProvider, k rotationKey, maxAge time.Duration) Item {
	labels := map[string]string{}
	for key, v := range p.Labels {
		labels = label(labels, key, v)
	}
	for key, v := range k.Extra {
		labels = label(labels, key, v)
	}
	if !k.Active {
		// An inactive key has already stopped working; its age is not a
		// deadline anybody has to meet.
		labels[LabelInUse] = "false"
	}

	expires := k.Expires
	if expires == nil {
		// No issuer date exists, so the policy is the deadline — and the
		// labels have to make clear that this is a date the operator chose,
		// not one the provider stated.
		d := k.Created.Add(maxAge)
		expires = &d
		labels = label(labels, "created", k.Created.Format(time.RFC3339))
		labels = label(labels, "policy.days", strconv.Itoa(p.MaxKeyAgeDays))
		labels = label(labels, "deadline", "rotation policy")
	} else {
		labels = label(labels, "deadline", "issuer")
	}

	name := k.Name
	if name == "" {
		name = k.ID
	}
	return Item{
		Kind:      KindIAMKey,
		Name:      name,
		Expires:   *expires,
		Source:    "rotation:" + p.Name,
		Namespace: p.Name,
		Labels:    labels,
	}
}

func (s *RotationSource) base(provider, fallback string) string {
	if u, ok := s.BaseURLs[provider]; ok && u != "" {
		return strings.TrimSuffix(u, "/")
	}
	return fallback
}

func rotationGet(ctx context.Context, client *http.Client, url string,
	headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	//nolint:errcheck // the body is read or abandoned either way.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%s: %s — listing keys needs an admin credential, not an ordinary API key", url, resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// anthropicKeys reads the organization's API keys. They carry a creation date
// and a status and no expiry, which is the whole reason this source exists.
func (s *RotationSource) anthropicKeys(ctx context.Context, client *http.Client,
	p RotationProvider) ([]rotationKey, error) {
	var body struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			CreatedAt   string `json:"created_at"`
			Status      string `json:"status"`
			WorkspaceID string `json:"workspace_id"`
		} `json:"data"`
	}
	url := s.base("anthropic", "https://api.anthropic.com") + "/v1/organizations/api_keys?limit=100"
	err := rotationGet(ctx, client, url, map[string]string{
		"x-api-key":         p.Token,
		"anthropic-version": "2023-06-01",
	}, &body)
	if err != nil {
		return nil, err
	}

	var out []rotationKey
	for _, k := range body.Data {
		created, ok := cfTime(k.CreatedAt)
		if !ok {
			continue // with no creation date there is no age to police
		}
		out = append(out, rotationKey{
			ID: k.ID, Name: k.Name, Created: created,
			Active: strings.EqualFold(k.Status, "active"),
			Extra:  map[string]string{"workspace": k.WorkspaceID, "status": k.Status},
		})
	}
	return out, nil
}

// openAIKeys reads the organization's admin keys. Timestamps here are Unix
// seconds rather than RFC 3339, which is the one real difference between the
// three adapters.
func (s *RotationSource) openAIKeys(ctx context.Context, client *http.Client,
	p RotationProvider) ([]rotationKey, error) {
	var body struct {
		Data []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			CreatedAt  int64  `json:"created_at"`
			LastUsedAt int64  `json:"last_used_at"`
		} `json:"data"`
	}
	url := s.base("openai", "https://api.openai.com") + "/v1/organization/admin_api_keys?limit=100"
	err := rotationGet(ctx, client, url, map[string]string{
		"Authorization": "Bearer " + p.Token,
	}, &body)
	if err != nil {
		return nil, err
	}

	var out []rotationKey
	for _, k := range body.Data {
		if k.CreatedAt <= 0 {
			continue
		}
		extra := map[string]string{}
		if k.LastUsedAt > 0 {
			extra["last-used"] = time.Unix(k.LastUsedAt, 0).UTC().Format(time.RFC3339)
		} else {
			// Never used and never expiring is a key to delete, not rotate.
			extra["last-used"] = "never"
			extra[LabelInUse] = "false"
		}
		out = append(out, rotationKey{
			ID: k.ID, Name: k.Name, Created: time.Unix(k.CreatedAt, 0).UTC(),
			Active: true, Extra: extra,
		})
	}
	return out, nil
}

// dockerHubKeys reads personal access tokens. Docker Hub grew expiring tokens
// later than it grew tokens, so both shapes are live and both are handled: a
// token that states an expiry is reported on the issuer's date, one that does
// not falls back to the policy.
func (s *RotationSource) dockerHubKeys(ctx context.Context, client *http.Client,
	p RotationProvider) ([]rotationKey, error) {
	var body struct {
		Results []struct {
			UUID       string   `json:"uuid"`
			TokenLabel string   `json:"token_label"`
			CreatedAt  string   `json:"created_at"`
			ExpiresAt  string   `json:"expires_at"`
			IsActive   bool     `json:"is_active"`
			Scopes     []string `json:"scopes"`
		} `json:"results"`
	}
	url := s.base("dockerhub", "https://hub.docker.com") + "/v2/access-tokens?page_size=100"
	err := rotationGet(ctx, client, url, map[string]string{
		"Authorization": "Bearer " + p.Token,
	}, &body)
	if err != nil {
		return nil, err
	}

	var out []rotationKey
	for _, k := range body.Results {
		created, ok := cfTime(k.CreatedAt)
		if !ok {
			continue
		}
		key := rotationKey{
			ID: k.UUID, Name: k.TokenLabel, Created: created, Active: k.IsActive,
			Extra: map[string]string{"scopes": strings.Join(k.Scopes, ",")},
		}
		if exp, ok := cfTime(k.ExpiresAt); ok {
			key.Expires = &exp
		}
		out = append(out, key)
	}
	return out, nil
}
