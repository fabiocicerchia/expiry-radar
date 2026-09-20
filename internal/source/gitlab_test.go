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

func fakeGitLab(routes map[string]any, deny map[string]int) (*httptest.Server, *[]string) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		for k, code := range deny {
			if strings.Contains(r.URL.Path, k) {
				w.WriteHeader(code)
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

func glDate(d int) string {
	return time.Now().Add(time.Duration(d) * 24 * time.Hour).Format("2006-01-02")
}

// Scope is the one honest blast-radius signal a forge can give: an api-scoped
// token can do everything its owner can, a read_registry one cannot.
func TestGitLabRanksTokensByScope(t *testing.T) {
	srv, _ := fakeGitLab(map[string]any{
		"/personal_access_tokens": []any{
			map[string]any{"id": 1, "name": "full", "expires_at": glDate(20), "scopes": []string{"api"}},
			map[string]any{"id": 2, "name": "narrow", "expires_at": glDate(20), "scopes": []string{"read_registry"}},
		},
	}, nil)
	defer srv.Close()

	items, err := (&GitLabSource{BaseURL: srv.URL, Token: "t"}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	full, ok := itemNamed(items, "pat/full")
	if !ok {
		t.Fatalf("want the api token: %+v", items)
	}
	narrow, _ := itemNamed(items, "pat/narrow")
	if full.Labels[LabelBlastRadius] != "0.95" {
		t.Errorf("api scope blast = %q, want 0.95", full.Labels[LabelBlastRadius])
	}
	if narrow.Labels[LabelBlastRadius] != "0.40" {
		t.Errorf("read_registry blast = %q, want 0.40", narrow.Labels[LabelBlastRadius])
	}
}

func TestGitLabTokensWithNoExpiryAreNotDeadlines(t *testing.T) {
	srv, _ := fakeGitLab(map[string]any{
		"/personal_access_tokens": []any{
			map[string]any{"id": 1, "name": "forever", "scopes": []string{"api"}},
		},
	}, nil)
	defer srv.Close()

	items, err := (&GitLabSource{BaseURL: srv.URL, Token: "t"}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("a token with no expiry has no deadline to miss: %+v", items)
	}
}

func TestGitLabOneUnreadableProjectKeepsTheOthers(t *testing.T) {
	srv, _ := fakeGitLab(map[string]any{
		"/access_tokens": []any{
			map[string]any{"id": 9, "name": "ci", "expires_at": glDate(5), "scopes": []string{"api"}},
		},
	}, map[string]int{"secret/project": http.StatusNotFound})
	defer srv.Close()

	s := &GitLabSource{BaseURL: srv.URL, Token: "t", Projects: []string{"acme/payments", "secret/project"}}
	items, err := s.Collect(context.Background())
	if err == nil {
		t.Fatal("a project the token cannot see must still be reported")
	}
	if !strings.Contains(err.Error(), "not visible to this token") {
		t.Errorf("GitLab answers 404 for both missing and hidden; say so. got %v", err)
	}
	if _, ok := itemNamed(items, "acme/payments/ci"); !ok {
		t.Fatalf("the unreadable project lost the readable one's findings: %+v", items)
	}
}

func TestGitLabPagesAutoSSLIsDeRanked(t *testing.T) {
	when := time.Now().Add(20 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := fakeGitLab(map[string]any{
		"/pages/domains": []any{
			map[string]any{"domain": "auto.example.com", "auto_ssl_enabled": true,
				"certificate": map[string]any{"expiration": when}},
			map[string]any{"domain": "byhand.example.com", "auto_ssl_enabled": false,
				"certificate": map[string]any{"expiration": when}},
		},
	}, nil)
	defer srv.Close()

	s := &GitLabSource{BaseURL: srv.URL, Token: "t", Projects: []string{"acme/site"}, SkipPersonal: true}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	auto, _ := itemNamed(items, "auto.example.com")
	byhand, _ := itemNamed(items, "byhand.example.com")
	if auto.Labels[LabelRenewal] != RenewalManaged {
		t.Error("Let's Encrypt via GitLab renews itself")
	}
	if byhand.Labels[LabelRenewal] == RenewalManaged {
		t.Error("an uploaded Pages certificate is nobody's job but yours")
	}
}

func TestGitLabRevokedTokensAreNotLoadBearing(t *testing.T) {
	srv, _ := fakeGitLab(map[string]any{
		"/personal_access_tokens": []any{
			map[string]any{"id": 1, "name": "old", "expires_at": glDate(3),
				"revoked": true, "scopes": []string{"api"}},
		},
	}, nil)
	defer srv.Close()

	items, err := (&GitLabSource{BaseURL: srv.URL, Token: "t"}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 || items[0].Labels[LabelInUse] != "false" {
		t.Fatalf("a revoked token has already stopped working: %+v", items)
	}
}

func TestGitLabNeedsATokenBeforeItWillRun(t *testing.T) {
	_, err := (&GitLabSource{}).Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "GITLAB_TOKEN") {
		t.Fatalf("want an actionable error naming the variable, got %v", err)
	}
}

func TestGitLabDoesNotGuessAtProjectsOrGroups(t *testing.T) {
	srv, seen := fakeGitLab(nil, nil)
	defer srv.Close()

	if _, err := (&GitLabSource{BaseURL: srv.URL, Token: "t"}).Collect(context.Background()); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, p := range *seen {
		if strings.Contains(p, "/projects/") || strings.Contains(p, "/groups/") {
			t.Errorf("enumerated %s without being told which to read", p)
		}
	}
}
