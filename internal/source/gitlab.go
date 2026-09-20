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

// GitLabSource reports the credentials and certificates a GitLab installation
// knows expire.
//
// Unlike most providers, everything here carries a real date: GitLab caps
// personal access token lifetimes and has done for long enough that these
// genuinely lapse rather than living forever. The ones that break things
// quietly are project and group access tokens — CI stops authenticating on a
// Tuesday morning and the pipeline log says 401.
//
// Blast radius comes from scope rather than from hostnames. A group token with
// `api` is not a project deploy key with `read_repository`, and the ranking
// reflects that: breadth of access is what an expiry costs you here.
type GitLabSource struct {
	// BaseURL is the instance, e.g. https://gitlab.com. Empty uses gitlab.com.
	BaseURL string
	// Token never comes from the config file; Load fills it from
	// $GITLAB_TOKEN. `read_api` is enough.
	Token string
	// Projects and Groups are full paths ("acme/payments") or numeric IDs.
	// Empty means only the personal tokens are read — there is no cheap way to
	// enumerate every project a token can see without hammering the API.
	Projects []string
	Groups   []string
	Timeout  time.Duration

	SkipPersonal bool
	SkipProjects bool
	SkipGroups   bool
}

const gitlabDotCom = "https://gitlab.com"

// Name identifies this source in an item's Source field.
func (s *GitLabSource) Name() string { return "gitlab" }

// Collect reads tokens, deploy tokens and Pages certificates.
func (s *GitLabSource) Collect(ctx context.Context) ([]Item, error) {
	if s.Token == "" {
		return nil, fmt.Errorf("gitlab source is enabled but $GITLAB_TOKEN is not set")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	items, _, err := collectUnits([]collectUnit{
		{"personal", s.SkipPersonal, func() ([]Item, error) { return s.personalTokens(ctx, client) }},
		{"projects", s.SkipProjects || len(s.Projects) == 0,
			func() ([]Item, error) { return s.projectItems(ctx, client) }},
		{"groups", s.SkipGroups || len(s.Groups) == 0,
			func() ([]Item, error) { return s.groupTokens(ctx, client) }},
	})
	return items, err
}

func glGet[T any](ctx context.Context, s *GitLabSource, client *http.Client, path string) ([]T, error) {
	base := s.BaseURL
	if base == "" {
		base = gitlabDotCom
	}
	base = strings.TrimSuffix(base, "/") + "/api/v4"

	var out []T
	for page := 1; page <= 40; page++ {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		u := base + path + sep + "page=" + strconv.Itoa(page) + "&per_page=100"

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return out, err
		}
		req.Header.Set("PRIVATE-TOKEN", s.Token)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return out, err
		}
		var batch []T
		decErr := json.NewDecoder(resp.Body).Decode(&batch)
		next := resp.Header.Get("X-Next-Page")
		status, statusCode := resp.Status, resp.StatusCode
		//nolint:errcheck // the body is read or abandoned either way.
		_ = resp.Body.Close()

		switch statusCode {
		case http.StatusOK:
		case http.StatusUnauthorized, http.StatusForbidden:
			return out, fmt.Errorf("GET %s: %s — the token needs read_api, and owner or maintainer on the resource",
				path, status)
		case http.StatusNotFound:
			// GitLab answers 404 rather than 403 for a resource the token
			// cannot see, so this is "not visible to this token" as often as
			// it is "not there". Either way it is worth saying.
			return out, fmt.Errorf("GET %s: not found, or not visible to this token", path)
		default:
			return out, fmt.Errorf("GET %s: %s", path, status)
		}
		if decErr != nil {
			return out, fmt.Errorf("GET %s: %w", path, decErr)
		}

		out = append(out, batch...)
		// GitLab paginates with headers, not a body envelope.
		if next == "" || len(batch) == 0 {
			return out, nil
		}
	}
	return out, fmt.Errorf("GET %s: stopped after 40 pages", path)
}

type glToken struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Revoked     bool     `json:"revoked"`
	Active      bool     `json:"active"`
	ExpiresAt   string   `json:"expires_at"`
	Scopes      []string `json:"scopes"`
	AccessLevel int      `json:"access_level"`
}

func (s *GitLabSource) personalTokens(ctx context.Context, client *http.Client) ([]Item, error) {
	tokens, err := glGet[glToken](ctx, s, client, "/personal_access_tokens?state=active")
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, t := range tokens {
		if it, ok := s.tokenItem(t, "gitlab:pat", "", "pat/"+t.Name); ok {
			items = append(items, it)
		}
	}
	return items, nil
}

func (s *GitLabSource) projectItems(ctx context.Context, client *http.Client) ([]Item, error) {
	var items []Item
	var warnings []string
	for _, p := range s.Projects {
		id := url.PathEscape(p) // a full path needs its slashes escaped
		got, err := glGet[glToken](ctx, s, client, "/projects/"+id+"/access_tokens")
		if err != nil {
			warnings = append(warnings, p+" access tokens: "+err.Error())
		}
		for _, t := range got {
			if it, ok := s.tokenItem(t, "gitlab:project-token", p, p+"/"+t.Name); ok {
				items = append(items, it)
			}
		}

		deploy, err := glGet[glToken](ctx, s, client, "/projects/"+id+"/deploy_tokens")
		if err != nil {
			warnings = append(warnings, p+" deploy tokens: "+err.Error())
		}
		for _, t := range deploy {
			if it, ok := s.tokenItem(t, "gitlab:deploy-token", p, p+"/deploy/"+t.Name); ok {
				items = append(items, it)
			}
		}

		pages, err := glGet[glPagesDomain](ctx, s, client, "/projects/"+id+"/pages/domains")
		if err != nil {
			warnings = append(warnings, p+" pages domains: "+err.Error())
		}
		for _, d := range pages {
			if it, ok := pagesItem(d, p); ok {
				items = append(items, it)
			}
		}
	}
	return items, joinErrs(warnings)
}

func (s *GitLabSource) groupTokens(ctx context.Context, client *http.Client) ([]Item, error) {
	var items []Item
	var warnings []string
	for _, g := range s.Groups {
		got, err := glGet[glToken](ctx, s, client, "/groups/"+url.PathEscape(g)+"/access_tokens")
		if err != nil {
			warnings = append(warnings, g+": "+err.Error())
			continue
		}
		for _, t := range got {
			if it, ok := s.tokenItem(t, "gitlab:group-token", g, g+"/"+t.Name); ok {
				items = append(items, it)
			}
		}
	}
	return items, joinErrs(warnings)
}

// tokenItem turns a token into a finding, and its scopes into blast radius.
func (s *GitLabSource) tokenItem(t glToken, src, namespace, name string) (Item, bool) {
	expires, ok := glTime(t.ExpiresAt)
	if !ok {
		return Item{}, false // no expiry set means no deadline to miss
	}
	labels := map[string]string{}
	labels = label(labels, "scopes", strings.Join(t.Scopes, ","))
	if t.AccessLevel > 0 {
		labels = label(labels, "access-level", strconv.Itoa(t.AccessLevel))
	}
	// A revoked token has already stopped working, so its expiry is not a
	// deadline anybody has to meet.
	if t.Revoked {
		labels[LabelInUse] = "false"
	}
	labels = label(labels, LabelBlastRadius, scopeBlastRadius(t.Scopes))

	if name == "" {
		name = strconv.Itoa(t.ID)
	}
	return Item{
		Kind:      KindIAMKey,
		Name:      name,
		Expires:   expires,
		Source:    src,
		Namespace: namespace,
		Labels:    labels,
	}, true
}

// scopeBlastRadius reads breadth of access straight off the token, which is the
// one honest blast-radius signal a forge can give. A token that can write the
// API can do anything the owner can; one that can only read a registry cannot.
//
// Returned as the operator-style label so it bypasses inference entirely —
// there is nothing to infer when the provider states the scope outright.
func scopeBlastRadius(scopes []string) string {
	best := ""
	for _, s := range scopes {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "api":
			return "0.95" // full read-write API: everything the owner can do
		case "write_repository", "sudo", "admin_mode":
			best = "0.85"
		case "read_api":
			if best == "" {
				best = "0.55"
			}
		case "read_repository", "read_registry", "write_registry":
			if best == "" {
				best = "0.40"
			}
		}
	}
	return best
}

type glPagesDomain struct {
	Domain      string `json:"domain"`
	AutoSSL     bool   `json:"auto_ssl_enabled"`
	Certificate struct {
		Expiration string `json:"expiration"`
		Expired    bool   `json:"expired"`
	} `json:"certificate"`
}

func pagesItem(d glPagesDomain, project string) (Item, bool) {
	expires, ok := glTime(d.Certificate.Expiration)
	if !ok {
		return Item{}, false
	}
	labels := map[string]string{LabelPublic: "true"}
	labels = label(labels, LabelHosts, d.Domain)
	// Let's Encrypt via GitLab renews itself; an uploaded certificate does not.
	if d.AutoSSL {
		labels[LabelRenewal] = RenewalManaged
	}
	return Item{
		Kind:      KindTLSCert,
		Name:      d.Domain,
		Expires:   expires,
		Source:    "gitlab:pages",
		Namespace: project,
		Labels:    labels,
	}, true
}

// glTime parses GitLab's two shapes: a bare date for token expiry, and a full
// timestamp for certificates. A bare day means its start in UTC, which errs
// towards warning early — the same choice manual items make.
func glTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, dateOnly} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
