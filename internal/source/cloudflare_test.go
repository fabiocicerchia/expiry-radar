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

// fakeCF serves the Cloudflare envelope for routed paths and an empty success
// for anything else, which is what a token with narrower scope sees.
func fakeCF(routes map[string]any, deny map[string]int) (*httptest.Server, *[]string) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		for k, code := range deny {
			if strings.Contains(r.URL.Path, k) {
				w.WriteHeader(code)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": false,
					"errors":  []map[string]any{{"code": 10000, "message": "Authentication error"}},
				})
				return
			}
		}
		result := any([]any{})
		for k, v := range routes {
			if strings.HasSuffix(r.URL.Path, k) {
				result = v
				break
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":     true,
			"errors":      []any{},
			"result":      result,
			"result_info": map[string]any{"page": 1, "total_pages": 1},
		})
	}))
	return srv, &seen
}

func cfTestSource(url string) *CloudflareSource {
	return &CloudflareSource{Token: "t", AccountID: "acct", BaseURL: url}
}

// zoneResult serves one zone. Its status is always active — a non-active zone
// is not what any of these tests vary; paused is.
func zoneResult(name string, paused bool) []any {
	return []any{map[string]any{"id": "z1", "name": name, "status": "active", "paused": paused}}
}

func TestCloudflareReportsEdgeAndCustomCertificates(t *testing.T) {
	soon := time.Now().Add(20 * 24 * time.Hour).Format(time.RFC3339)

	srv, _ := fakeCF(map[string]any{
		"/zones": zoneResult("shop.example.com", false),
		"/ssl/certificate_packs": []any{map[string]any{
			"id": "pack1", "type": "universal", "hosts": []string{"shop.example.com"},
			"certificates": []any{map[string]any{
				"id": "c1", "hosts": []string{"shop.example.com"}, "issuer": "LetsEncrypt",
				"status": "active", "expires_on": soon,
			}},
		}},
		"/custom_certificates": []any{map[string]any{
			"id": "cc1", "hosts": []string{"legacy.example.com"}, "issuer": "DigiCert",
			"status": "active", "expires_on": soon,
		}},
	}, nil)
	defer srv.Close()

	items, err := cfTestSource(srv.URL).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	edge, ok := itemNamed(items, "shop.example.com")
	if !ok {
		t.Fatalf("want the edge certificate, got %+v", items)
	}
	if edge.Labels[LabelPublic] != "true" {
		t.Error("a certificate on an active, unpaused zone is internet-facing by definition")
	}
	// Cloudflare renews a universal pack itself; an uploaded one nobody renews.
	if edge.Labels[LabelRenewal] != RenewalManaged {
		t.Errorf("universal pack renewal = %q, want %q", edge.Labels[LabelRenewal], RenewalManaged)
	}
	custom, ok := itemNamed(items, "legacy.example.com")
	if !ok {
		t.Fatalf("want the custom certificate, got %+v", items)
	}
	if custom.Labels[LabelRenewal] == RenewalManaged {
		t.Error("an uploaded certificate is exactly the one nobody renews; it must not be de-ranked")
	}
}

func TestCloudflareRegistrarAutoRenewIsDeRanked(t *testing.T) {
	soon := time.Now().Add(60 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := fakeCF(map[string]any{
		"/registrar/domains": []any{
			map[string]any{"id": "d1", "name": "auto.example", "expires_at": soon, "auto_renew": true},
			map[string]any{"id": "d2", "name": "manual.example", "expires_at": soon, "auto_renew": false},
		},
	}, nil)
	defer srv.Close()

	items, err := cfTestSource(srv.URL).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	auto, _ := itemNamed(items, "auto.example")
	manual, _ := itemNamed(items, "manual.example")
	if auto.Kind != KindDomain || manual.Kind != KindDomain {
		t.Fatalf("both should be domains: %+v", items)
	}
	if auto.Labels[LabelRenewal] != RenewalManaged {
		t.Error("auto-renew is the registrar's version of a healthy renewal")
	}
	if manual.Labels[LabelRenewal] == RenewalManaged {
		t.Error("a domain nobody renews automatically must keep its full blast radius")
	}
}

// The rule this source shares with AWS and Kubernetes.
func TestCloudflareOneDeniedScopeKeepsTheOthersFindings(t *testing.T) {
	soon := time.Now().Add(10 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := fakeCF(map[string]any{
		"/zones":                 zoneResult("shop.example.com", false),
		"/ssl/certificate_packs": []any{},
		"/custom_certificates": []any{map[string]any{
			"id": "cc1", "hosts": []string{"shop.example.com"}, "expires_on": soon, "status": "active",
		}},
	}, map[string]int{"/user/tokens": http.StatusForbidden})
	defer srv.Close()

	items, err := cfTestSource(srv.URL).Collect(context.Background())
	if err == nil {
		t.Fatal("a token missing a read permission must still be reported")
	}
	if !strings.Contains(err.Error(), "user:") {
		t.Errorf("the error should name the scope that failed, got %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("the denied scope lost the zone's findings: %+v", items)
	}
}

// 200 with success=false is common enough on this API that trusting the status
// code alone would swallow real errors.
func TestCloudflareTreatsSuccessFalseAsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"errors":  []map[string]any{{"code": 9109, "message": "Invalid access token"}},
			"result":  []any{},
		})
	}))
	defer srv.Close()

	_, err := cfTestSource(srv.URL).Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Invalid access token") {
		t.Fatalf("a 200 reporting failure must not read as success, got %v", err)
	}
}

func TestCloudflareSkipsThingsWithNoExpiry(t *testing.T) {
	srv, _ := fakeCF(map[string]any{
		// An API token with no expiry has no deadline to miss.
		"/user/tokens": []any{map[string]any{"id": "t1", "name": "forever", "status": "active"}},
	}, nil)
	defer srv.Close()

	items, err := cfTestSource(srv.URL).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("nothing here expires: %+v", items)
	}
}

func TestCloudflareSkipsTheAccountScopeWithoutAnAccountID(t *testing.T) {
	srv, seen := fakeCF(nil, nil)
	defer srv.Close()

	s := &CloudflareSource{Token: "t", BaseURL: srv.URL} // no AccountID
	if _, err := s.Collect(context.Background()); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, p := range *seen {
		if strings.Contains(p, "/accounts/") {
			t.Errorf("guessed at an account path with no account id: %s", p)
		}
	}
}

func TestCloudflareNeedsATokenBeforeItWillRun(t *testing.T) {
	_, err := (&CloudflareSource{}).Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "CLOUDFLARE_API_TOKEN") {
		t.Fatalf("want an actionable error naming the variable, got %v", err)
	}
}

func TestCloudflarePausedZoneCertificatesAreNotLoadBearing(t *testing.T) {
	soon := time.Now().Add(15 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := fakeCF(map[string]any{
		"/zones": zoneResult("paused.example.com", true),
		"/custom_certificates": []any{map[string]any{
			"id": "cc1", "hosts": []string{"paused.example.com"}, "expires_on": soon, "status": "active",
		}},
	}, nil)
	defer srv.Close()

	items, err := cfTestSource(srv.URL).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("want the certificate")
	}
	if items[0].Labels[LabelInUse] != "false" {
		t.Error("a paused zone is not serving, so its certificates are not load-bearing")
	}
	if items[0].Labels[LabelPublic] == "true" {
		t.Error("a paused zone is not internet-facing through Cloudflare")
	}
}

// A configured zone id that cannot be read must not cost the zones that can.
func TestCloudflareOneUnreadableZoneKeepsTheOthers(t *testing.T) {
	soon := time.Now().Add(12 * 24 * time.Hour).Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok := func(result any) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true, "errors": []any{}, "result": result,
				"result_info": map[string]any{"page": 1, "total_pages": 1},
			})
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/zones") && r.URL.Query().Get("id") == "goodzone":
			ok(zoneResult("shop.example.com", false))
		case strings.HasSuffix(r.URL.Path, "/zones"):
			// The other configured id is invisible to this token.
			ok([]any{})
		case strings.Contains(r.URL.Path, "custom_certificates"):
			ok([]any{map[string]any{"id": "cc1", "hosts": []string{"shop.example.com"},
				"expires_on": soon, "status": "active"}})
		default:
			ok([]any{})
		}
	}))
	defer srv.Close()

	s := cfTestSource(srv.URL)
	s.Zones = []string{"goodzone", "hiddenzone"}
	s.SkipAccount, s.SkipUser = true, true
	items, err := s.Collect(context.Background())
	if err == nil {
		t.Fatal("the zone that could not be read must still be reported")
	}
	if len(items) != 1 {
		t.Fatalf("one unreadable zone lost the other's certificates: %+v", items)
	}
}

// The account store is the half /user/tokens never returns, and on an account
// whose credentials were all made under Manage Account > API Tokens it is the
// only half with anything in it.
func TestCloudflareReportsAccountOwnedTokens(t *testing.T) {
	srv, seen := fakeCF(map[string]any{
		"/accounts/acct/tokens": []any{map[string]any{
			"id": "a1", "name": "terraform", "status": "active",
			"issued_on": "2026-08-09T00:00:00Z", "expires_on": "2026-12-20T00:00:00Z",
		}},
	}, nil)
	defer srv.Close()

	items, err := cfTestSource(srv.URL).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	var got *Item
	for i := range items {
		if items[i].Source == "cloudflare:account-token" {
			got = &items[i]
		}
	}
	if got == nil {
		t.Fatalf("the account's own tokens were not read: %+v (paths %v)", items, *seen)
	}
	if got.Name != "token/terraform" {
		t.Errorf("name = %q", got.Name)
	}
}

// A token that cannot expire is the one credential still valid the day it
// leaks, so it is worth reporting — but only against a deadline somebody
// chose. Without maxKeyAgeDays there is no such deadline and it stays out.
func TestCloudflareNonExpiringTokenNeedsARotationPolicy(t *testing.T) {
	routes := map[string]any{
		"/accounts/acct/tokens": []any{map[string]any{
			"id": "a1", "name": "forever", "status": "active",
			"issued_on": "2026-08-09T00:00:00Z", // no expires_on
		}},
	}

	srv, _ := fakeCF(routes, nil)
	defer srv.Close()
	items, err := cfTestSource(srv.URL).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("invented a deadline nobody chose: %+v", items)
	}

	s := cfTestSource(srv.URL)
	s.MaxKeyAgeDays = 365
	items, err = s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item with a policy deadline, got %+v", items)
	}
	if want := "2027-08-09"; items[0].Expires.Format("2006-01-02") != want {
		t.Errorf("deadline = %s, want %s (issued + 365)", items[0].Expires.Format("2006-01-02"), want)
	}
	// The labels have to say the date was chosen, not stated by Cloudflare.
	if items[0].Labels["deadline"] != "rotation policy" {
		t.Errorf("a synthesised deadline must say so: %v", items[0].Labels)
	}
}

// The account token read needs a permission the rest of the account scope does
// not, so being denied it must not cost the registrar its findings.
func TestCloudflareDeniedAccountTokensKeepsTheRestOfTheScope(t *testing.T) {
	srv, _ := fakeCF(map[string]any{
		"/registrar/domains": []any{map[string]any{
			"id": "d1", "name": "example.com", "expires_at": "2027-01-01T00:00:00Z",
		}},
	}, map[string]int{"/accounts/acct/tokens": http.StatusForbidden})
	defer srv.Close()

	items, err := cfTestSource(srv.URL).Collect(context.Background())
	if err == nil {
		t.Fatal("a denied read must be reported")
	}
	found := false
	for _, it := range items {
		if it.Source == "cloudflare:registrar" {
			found = true
		}
	}
	if !found {
		t.Fatalf("one denied permission lost the others' findings: %+v", items)
	}
}
