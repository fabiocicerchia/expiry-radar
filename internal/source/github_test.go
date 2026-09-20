package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func ghServer(routes map[string]any, deny map[string]int) (*httptest.Server, *[]string) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		for k, code := range deny {
			if strings.Contains(r.URL.Path, k) {
				w.WriteHeader(code)
				_ = json.NewEncoder(w).Encode([]any{})
				return
			}
		}
		for k, v := range routes {
			if strings.HasSuffix(r.URL.Path, k) {
				_ = json.NewEncoder(w).Encode(v)
				return
			}
		}
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	return srv, &seen
}

// A token granted across every repository in the org is a wider blast radius
// than one scoped to a few, and GitHub states which rather than implying it.
func TestGitHubAllRepositoryTokensOutrankSelectedOnes(t *testing.T) {
	soon := time.Now().Add(20 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := ghServer(map[string]any{
		"/personal-access-tokens": []any{
			map[string]any{"id": 1, "token_name": "wide", "token_expires_at": soon,
				"repository_selection": "all", "owner": map[string]any{"login": "alice"}},
			map[string]any{"id": 2, "token_name": "narrow", "token_expires_at": soon,
				"repository_selection": "selected", "owner": map[string]any{"login": "bob"}},
		},
	}, nil)
	defer srv.Close()

	s := &GitHubSource{Token: "t", BaseURL: srv.URL, Orgs: []string{"acme"}, SkipGPGKeys: true}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	wide, ok := itemNamed(items, "acme/wide")
	if !ok {
		t.Fatalf("want both tokens: %+v", items)
	}
	narrow, _ := itemNamed(items, "acme/narrow")
	if wide.Labels[LabelBlastRadius] != "0.80" {
		t.Errorf("an all-repository token should carry the wider radius, got %q",
			wide.Labels[LabelBlastRadius])
	}
	if narrow.Labels[LabelBlastRadius] != "" {
		t.Error("a selected-repository token should be left to normal inference")
	}
	if wide.Labels["owner"] != "alice" {
		t.Errorf("the owner should be recorded so somebody can be asked: %v", wide.Labels)
	}
}

func TestGitHubTokensSetNeverToExpireAreNotDeadlines(t *testing.T) {
	srv, _ := ghServer(map[string]any{
		"/personal-access-tokens": []any{
			map[string]any{"id": 1, "token_name": "forever", "repository_selection": "all"},
		},
	}, nil)
	defer srv.Close()

	s := &GitHubSource{Token: "t", BaseURL: srv.URL, Orgs: []string{"acme"}, SkipGPGKeys: true}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("a token with no expiry has no deadline to miss: %+v", items)
	}
}

func TestGitHubGPGKeysAreReported(t *testing.T) {
	soon := time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := ghServer(map[string]any{
		"/user/gpg_keys": []any{
			map[string]any{"key_id": "ABC123", "name": "signing", "expires_at": soon,
				"emails": []any{map[string]any{"email": "dev@example.com"}}},
			map[string]any{"key_id": "DEF456", "name": "old", "expires_at": soon, "revoked": true},
		},
	}, nil)
	defer srv.Close()

	items, err := (&GitHubSource{Token: "t", BaseURL: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	live, ok := itemNamed(items, "gpg/signing")
	if !ok {
		t.Fatalf("want the signing key: %+v", items)
	}
	if live.Labels["emails"] != "dev@example.com" {
		t.Errorf("emails = %v", live.Labels)
	}
	revoked, _ := itemNamed(items, "gpg/old")
	if revoked.Labels[LabelInUse] != "false" {
		t.Error("a revoked key has already stopped signing")
	}
}

// Listing an org's tokens needs owner, and a token that merely belongs to the
// org gets a 403 — which is worth saying rather than reporting an empty org.
func TestGitHubExplainsTheOwnerRequirement(t *testing.T) {
	srv, _ := ghServer(nil, map[string]int{"/personal-access-tokens": http.StatusForbidden})
	defer srv.Close()

	s := &GitHubSource{Token: "t", BaseURL: srv.URL, Orgs: []string{"acme"}, SkipGPGKeys: true}
	_, err := s.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "owner credential") {
		t.Fatalf("want the requirement named, got %v", err)
	}
}

func TestGitHubDoesNotGuessAtOrganizations(t *testing.T) {
	srv, seen := ghServer(nil, nil)
	defer srv.Close()

	if _, err := (&GitHubSource{Token: "t", BaseURL: srv.URL}).Collect(context.Background()); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, p := range *seen {
		if strings.Contains(p, "/orgs/") {
			t.Errorf("enumerated %s without being told which org to read", p)
		}
	}
}
