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

// GitHubSource reports the two things GitHub actually exposes with a date.
//
// This source is deliberately thin, and the gap is the useful part to state:
// the GitHub credentials that hurt when they lapse are **not readable by any
// API**. A GitHub App's private key, an App client secret and a classic
// personal access token have no list endpoint at all, so nothing can discover
// them and they belong in `manual` with a `renew-at` label. Shipping a fatter
// adapter would imply a coverage that does not exist.
//
// What is readable: fine-grained personal access tokens with access to an
// organization, which an owner can list, and a user's GPG keys.
type GitHubSource struct {
	// Token never comes from the config file; Load fills it from
	// $GITHUB_TOKEN. Listing org tokens needs an owner-level credential.
	Token string
	// Orgs to inspect. Empty skips the token listing — there is nothing to
	// enumerate without one, and guessing is not a read-only posture.
	Orgs []string
	// BaseURL supports GitHub Enterprise Server, and is the test seam.
	BaseURL string
	Timeout time.Duration

	SkipOrgTokens bool
	SkipGPGKeys   bool
}

const githubAPI = "https://api.github.com"

// Name identifies this source in an item's Source field.
func (s *GitHubSource) Name() string { return "github" }

// Collect reads org fine-grained tokens and the user's GPG keys.
func (s *GitHubSource) Collect(ctx context.Context) ([]Item, error) {
	if s.Token == "" {
		return nil, fmt.Errorf("github source is enabled but $GITHUB_TOKEN is not set")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	items, _, err := collectUnits([]collectUnit{
		{"org-tokens", s.SkipOrgTokens || len(s.Orgs) == 0,
			func() ([]Item, error) { return s.orgTokens(ctx, client) }},
		{"gpg-keys", s.SkipGPGKeys, func() ([]Item, error) { return s.gpgKeys(ctx, client) }},
	})
	return items, err
}

func ghGet[T any](ctx context.Context, s *GitHubSource, client *http.Client, path string) ([]T, error) {
	base := s.BaseURL
	if base == "" {
		base = githubAPI
	}
	base = strings.TrimSuffix(base, "/")
	// api.github.com has no path prefix; Enterprise Server serves the same API
	// under /api/v3. Without this a GHES host configured per the docs 404s on
	// every call, and ghGet would report that as "not visible to this token".
	if s.BaseURL != "" && !strings.HasSuffix(base, "/api/v3") {
		base += "/api/v3"
	}

	var out []T
	for page := 1; page <= 20; page++ {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		u := base + path + sep + "per_page=100&page=" + strconv.Itoa(page)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return out, err
		}
		req.Header.Set("Authorization", "Bearer "+s.Token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := client.Do(req)
		if err != nil {
			return out, err
		}
		var batch []T
		decErr := json.NewDecoder(resp.Body).Decode(&batch)
		status, code := resp.Status, resp.StatusCode
		//nolint:errcheck // the body is read or abandoned either way.
		_ = resp.Body.Close()

		switch code {
		case http.StatusOK:
		case http.StatusUnauthorized, http.StatusForbidden:
			return out, fmt.Errorf("GET %s: %s — listing an organization's tokens needs an owner credential",
				path, status)
		case http.StatusNotFound:
			return out, fmt.Errorf("GET %s: not found, or not visible to this token", path)
		default:
			return out, fmt.Errorf("GET %s: %s", path, status)
		}
		if decErr != nil {
			return out, fmt.Errorf("GET %s: %w", path, decErr)
		}

		out = append(out, batch...)
		if len(batch) < 100 {
			return out, nil
		}
	}
	return out, fmt.Errorf("GET %s: stopped after 20 pages", path)
}

type ghOrgToken struct {
	ID                  int    `json:"id"`
	TokenName           string `json:"token_name"`
	TokenExpired        bool   `json:"token_expired"`
	TokenExpiresAt      string `json:"token_expires_at"`
	RepositorySelection string `json:"repository_selection"`
	Owner               struct {
		Login string `json:"login"`
	} `json:"owner"`
}

func (s *GitHubSource) orgTokens(ctx context.Context, client *http.Client) ([]Item, error) {
	var items []Item
	var warnings []string
	for _, org := range s.Orgs {
		got, err := ghGet[ghOrgToken](ctx, s, client,
			"/orgs/"+url.PathEscape(org)+"/personal-access-tokens")
		if err != nil {
			warnings = append(warnings, org+": "+err.Error())
			continue
		}
		for _, t := range got {
			expires, ok := cfTime(t.TokenExpiresAt)
			if !ok {
				continue // a fine-grained token can be set never to expire
			}
			name := t.TokenName
			if name == "" {
				name = strconv.Itoa(t.ID)
			}
			labels := map[string]string{}
			labels = label(labels, "owner", t.Owner.Login)
			labels = label(labels, "repository-selection", t.RepositorySelection)
			// A token granted across every repository in the org is a wider
			// blast radius than one scoped to a few, and GitHub says which.
			if strings.EqualFold(t.RepositorySelection, "all") {
				labels = label(labels, LabelBlastRadius, "0.80")
			}
			if t.TokenExpired {
				labels[LabelInUse] = "false"
			}
			items = append(items, Item{
				Kind:      KindIAMKey,
				Name:      org + "/" + name,
				Expires:   expires,
				Source:    "github:org-token",
				Namespace: org,
				Labels:    labels,
			})
		}
	}
	return items, joinErrs(warnings)
}

type ghGPGKey struct {
	KeyID     string `json:"key_id"`
	Name      string `json:"name"`
	ExpiresAt string `json:"expires_at"`
	Revoked   bool   `json:"revoked"`
	Emails    []struct {
		Email string `json:"email"`
	} `json:"emails"`
}

// gpgKeys reports signing keys with an expiry. When one lapses, commit
// signatures stop verifying and every protected branch that requires signed
// commits starts rejecting pushes.
func (s *GitHubSource) gpgKeys(ctx context.Context, client *http.Client) ([]Item, error) {
	keys, err := ghGet[ghGPGKey](ctx, s, client, "/user/gpg_keys")
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, k := range keys {
		expires, ok := cfTime(k.ExpiresAt)
		if !ok {
			continue // a GPG key with no expiry set has no deadline to miss
		}
		emails := make([]string, 0, len(k.Emails))
		for _, e := range k.Emails {
			emails = append(emails, e.Email)
		}
		labels := map[string]string{}
		labels = label(labels, "emails", strings.Join(emails, ","))
		if k.Revoked {
			labels[LabelInUse] = "false"
		}
		name := k.Name
		if name == "" {
			name = k.KeyID
		}
		items = append(items, Item{
			Kind:    KindSecret,
			Name:    "gpg/" + name,
			Expires: expires,
			Source:  "github:gpg",
			Labels:  labels,
		})
	}
	return items, nil
}
