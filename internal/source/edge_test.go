package source

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFastlyScopedTokensDoNotCarryGlobalBlastRadius(t *testing.T) {
	soon := time.Now().Add(20 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := jsonServer(map[string]any{
		"/tokens": []any{
			map[string]any{"id": "1", "name": "global-token", "expires_at": soon, "scope": "global"},
			map[string]any{"id": "2", "name": "one-service", "expires_at": soon,
				"scope": "purge_select", "services": []string{"svc1"}},
		},
	}, nil)
	defer srv.Close()

	s := &FastlySource{Token: "t", BaseURL: srv.URL, SkipCertificates: true}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	global, _ := itemNamed(items, "token/global-token")
	scoped, _ := itemNamed(items, "token/one-service")
	if global.Labels[LabelBlastRadius] != "0.85" {
		t.Errorf("a global token reaches everything, got %q", global.Labels[LabelBlastRadius])
	}
	if scoped.Labels[LabelBlastRadius] != "" {
		t.Error("a token pinned to one service must not claim the global radius")
	}
	if scoped.Labels["services"] != "svc1" {
		t.Errorf("the services should be recorded: %v", scoped.Labels)
	}
}

// Fastly is JSON:API, so the fields live under attributes rather than at the
// top level — getting that wrong yields rows with no dates and no complaint.
func TestFastlyReadsTheJSONAPIAttributesEnvelope(t *testing.T) {
	soon := time.Now().Add(15 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := jsonServer(map[string]any{
		"/tls/certificates": map[string]any{"data": []any{
			map[string]any{"id": "c1", "type": "tls_certificate", "attributes": map[string]any{
				"name": "shop", "not_after": soon, "issued_to": "shop.example.com", "issuer": "Certainly",
			}},
		}},
	}, nil)
	defer srv.Close()

	s := &FastlySource{Token: "t", BaseURL: srv.URL, SkipTokens: true}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want the certificate: %+v", items)
	}
	if items[0].Labels[LabelHosts] != "shop.example.com" || items[0].Expires.IsZero() {
		t.Errorf("attributes were not read: %+v", items[0])
	}
}

func TestHetznerManagedCertificatesAreDeRankedOnlyWhenRenewalIsHealthy(t *testing.T) {
	soon := time.Now().Add(20 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := jsonServer(map[string]any{
		"/v1/certificates": map[string]any{
			"certificates": []any{
				map[string]any{"id": 1, "name": "healthy", "type": "managed",
					"not_valid_after": soon, "domain_names": []string{"a.example.com"},
					"status": map[string]any{"renewal": "scheduled"}},
				map[string]any{"id": 2, "name": "failing", "type": "managed",
					"not_valid_after": soon, "status": map[string]any{"renewal": "failed"}},
				map[string]any{"id": 3, "name": "uploaded", "type": "uploaded",
					"not_valid_after": soon},
			},
			"meta": map[string]any{"pagination": map[string]any{"total_entries": 3}},
		},
	}, nil)
	defer srv.Close()

	items, err := (&HetznerSource{Token: "t", BaseURL: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	healthy, _ := itemNamed(items, "healthy")
	failing, _ := itemNamed(items, "failing")
	uploaded, _ := itemNamed(items, "uploaded")
	if healthy.Labels[LabelRenewal] != RenewalManaged {
		t.Error("a managed certificate with a scheduled renewal is being renewed")
	}
	if failing.Labels[LabelRenewal] != RenewalStuck {
		t.Errorf("managed but failing has not earned the de-rank, got %q", failing.Labels[LabelRenewal])
	}
	if uploaded.Labels[LabelRenewal] == RenewalManaged {
		t.Error("an uploaded certificate is nobody's job but yours")
	}
}

// Harbor spells "never expires" as -1, which read as a Unix timestamp lands in
// 1969 and would top the report as decades overdue.
func TestHarborNeverExpiringRobotsAreNotOverdue(t *testing.T) {
	soon := time.Now().Add(30 * 24 * time.Hour).Unix()
	srv, _ := jsonServer(map[string]any{
		"/api/v2.0/robots": []any{
			map[string]any{"id": 1, "name": "robot$ci", "expires_at": soon, "level": "project"},
			map[string]any{"id": 2, "name": "robot$forever", "expires_at": -1, "level": "system"},
		},
	}, nil)
	defer srv.Close()

	s := &HarborSource{BaseURL: srv.URL, Username: "admin", Password: "p"}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 || items[0].Name != "robot$ci" {
		t.Fatalf("-1 means never, not 1969: %+v", items)
	}
}

func TestHarborSystemRobotsOutrankProjectOnes(t *testing.T) {
	soon := time.Now().Add(10 * 24 * time.Hour).Unix()
	srv, _ := jsonServer(map[string]any{
		"/api/v2.0/robots": []any{
			map[string]any{"id": 1, "name": "robot$wide", "expires_at": soon, "level": "system"},
			map[string]any{"id": 2, "name": "robot$narrow", "expires_at": soon, "level": "project"},
		},
	}, nil)
	defer srv.Close()

	items, err := (&HarborSource{BaseURL: srv.URL, Username: "a", Password: "p"}).
		Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	wide, _ := itemNamed(items, "robot$wide")
	narrow, _ := itemNamed(items, "robot$narrow")
	if wide.Labels[LabelBlastRadius] != "0.75" {
		t.Errorf("a system robot reaches every project, got %q", wide.Labels[LabelBlastRadius])
	}
	if narrow.Labels[LabelBlastRadius] != "" {
		t.Error("a project robot should be left to normal inference")
	}
}

// The credential must travel in a header, never anywhere an error could print.
func TestHarborSendsBasicAuthInAHeader(t *testing.T) {
	var auth, rawURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		rawURL = r.URL.String()
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	defer srv.Close()

	s := &HarborSource{BaseURL: srv.URL, Username: "admin", Password: "hunter2"}
	if _, err := s.Collect(context.Background()); err != nil {
		t.Fatalf("collect: %v", err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:hunter2"))
	if auth != want {
		t.Errorf("Authorization = %q", auth)
	}
	if strings.Contains(rawURL, "hunter2") {
		t.Fatalf("the password reached the URL: %q", rawURL)
	}
}

func TestJFrogNonExpiringTokensAreNotDeadlines(t *testing.T) {
	soon := time.Now().Add(45 * 24 * time.Hour).Unix()
	srv, _ := jsonServer(map[string]any{
		"/access/api/v1/tokens": map[string]any{"tokens": []any{
			map[string]any{"token_id": "1", "description": "ci", "expiry": soon,
				"issued_at": time.Now().Unix(), "scope": "applied-permissions/groups:readers"},
			map[string]any{"token_id": "2", "description": "forever", "subject": "admin"},
			map[string]any{"token_id": "3", "description": "admin-token", "expiry": soon,
				"scope": "applied-permissions/admin"},
		}},
	}, nil)
	defer srv.Close()

	items, err := (&JFrogSource{BaseURL: srv.URL, Token: "t"}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("only the two with an expiry are deadlines: %+v", items)
	}
	admin, _ := itemNamed(items, "admin-token")
	if admin.Labels[LabelBlastRadius] != "0.85" {
		t.Errorf("an admin-scoped token can do anything the platform can, got %q",
			admin.Labels[LabelBlastRadius])
	}
}

func TestEdgeSourcesNameTheirCredentialBeforeRunning(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  Source
		want string
	}{
		{"fastly", &FastlySource{}, "FASTLY_API_TOKEN"},
		{"hetzner", &HetznerSource{}, "HCLOUD_TOKEN"},
		{"harbor", &HarborSource{BaseURL: "https://x"}, "HARBOR_PASSWORD"},
		{"jfrog", &JFrogSource{BaseURL: "https://x"}, "JFROG_ACCESS_TOKEN"},
	} {
		_, err := tc.src.Collect(context.Background())
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want %s named, got %v", tc.name, tc.want, err)
		}
	}
}

// Hetzner reports several in-flight renewal states. Treating anything but
// "scheduled" as broken bumps the rank of certificates being renewed perfectly
// well; only an explicit failure should revoke the de-rank.
func TestHetznerInFlightRenewalsAreStillManaged(t *testing.T) {
	soon := time.Now().Add(20 * 24 * time.Hour).Format(time.RFC3339)
	for _, state := range []string{"pending", "unavailable", "", "scheduled"} {
		srv, _ := jsonServer(map[string]any{
			"/v1/certificates": map[string]any{
				"certificates": []any{
					map[string]any{"id": 1, "name": "c", "type": "managed",
						"not_valid_after": soon, "status": map[string]any{"renewal": state}},
				},
				"meta": map[string]any{"pagination": map[string]any{"total_entries": 1}},
			},
		}, nil)

		items, err := (&HetznerSource{Token: "t", BaseURL: srv.URL}).Collect(context.Background())
		srv.Close()
		if err != nil {
			t.Fatalf("%q: collect: %v", state, err)
		}
		if items[0].Labels[LabelRenewal] != RenewalManaged {
			t.Errorf("renewal %q is not a failure, got %q", state, items[0].Labels[LabelRenewal])
		}
	}
}

// An instance with more robots than one page would otherwise report the first
// hundred as though that were all of them.
func TestHarborPagesThroughItsRobots(t *testing.T) {
	soon := time.Now().Add(30 * 24 * time.Hour).Unix()
	var pages int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		page := r.URL.Query().Get("page")
		if page == "1" {
			full := make([]any, 0, 100)
			for i := range 100 {
				full = append(full, map[string]any{
					"id": i, "name": "robot$first", "expires_at": soon, "level": "project"})
			}
			_ = json.NewEncoder(w).Encode(full)
			return
		}
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"id": 101, "name": "robot$second-page", "expires_at": soon, "level": "project"},
		})
	}))
	defer srv.Close()

	items, err := (&HarborSource{BaseURL: srv.URL, Username: "a", Password: "p"}).
		Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if pages != 2 {
		t.Errorf("fetched %d pages, want 2", pages)
	}
	if _, ok := itemNamed(items, "robot$second-page"); !ok {
		t.Fatalf("the second page's robot is missing: %d items", len(items))
	}
}
