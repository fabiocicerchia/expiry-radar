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

func registrarSource(name, srvURL string, p RegistrarProvider) *RegistrarSource {
	p.Name = name
	return &RegistrarSource{
		BaseURLs:  map[string]string{name: srvURL},
		Providers: []RegistrarProvider{p},
	}
}

// The one thing RDAP cannot tell you, across all four adapters.
func TestEveryRegistrarReportsAutoRenew(t *testing.T) {
	soon := time.Now().Add(50 * 24 * time.Hour)
	rfc := soon.Format(time.RFC3339)

	for _, tc := range []struct {
		name  string
		body  any
		token RegistrarProvider
	}{
		{"dnsimple", map[string]any{"data": []any{
			map[string]any{"name": "auto.example", "expires_at": rfc, "auto_renew": true},
			map[string]any{"name": "manual.example", "expires_at": rfc, "auto_renew": false},
		}}, RegistrarProvider{Token: "t", Account: "1"}},

		{"gandi", []any{
			// Gandi spells it as an object on some plans.
			map[string]any{"fqdn": "auto.example", "dates": map[string]any{"registry_ends_at": rfc},
				"autorenew": map[string]any{"enabled": true}},
			map[string]any{"fqdn": "manual.example", "dates": map[string]any{"registry_ends_at": rfc},
				"autorenew": false},
		}, RegistrarProvider{Token: "t"}},

		{"godaddy", []any{
			// GoDaddy sometimes sends the flag as a string.
			map[string]any{"domain": "auto.example", "expires": rfc, "renewAuto": "true"},
			map[string]any{"domain": "manual.example", "expires": rfc, "renewAuto": false},
		}, RegistrarProvider{Token: "k", Secret: "s"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(tc.body)
			}))
			defer srv.Close()

			items, err := registrarSource(tc.name, srv.URL, tc.token).Collect(context.Background())
			if err != nil {
				t.Fatalf("collect: %v", err)
			}
			auto, ok := itemNamed(items, "auto.example")
			if !ok {
				t.Fatalf("want both domains: %+v", items)
			}
			manual, _ := itemNamed(items, "manual.example")
			if auto.Kind != KindDomain {
				t.Errorf("kind = %q", auto.Kind)
			}
			if auto.Labels[LabelRenewal] != RenewalManaged {
				t.Error("auto-renew on means somebody else is meeting this date")
			}
			if manual.Labels[LabelRenewal] == RenewalManaged {
				t.Error("auto-renew off is exactly the domain that lapses")
			}
		})
	}
}

// Porkbun puts credentials in a JSON body, spells the flag "1", and uses a
// timestamp that is neither RFC 3339 nor a bare date.
func TestPorkbunBodyCredentialsAndItsOwnTimeFormat(t *testing.T) {
	expire := time.Now().Add(70 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "SUCCESS",
			"domains": []any{
				map[string]any{"domain": "auto.example", "expireDate": expire, "autoRenew": "1"},
				map[string]any{"domain": "manual.example", "expireDate": expire, "autoRenew": "0"},
			},
		})
	}))
	defer srv.Close()

	s := registrarSource("porkbun", srv.URL, RegistrarProvider{Token: "key", Secret: "sec"})
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if gotBody["apikey"] != "key" || gotBody["secretapikey"] != "sec" {
		t.Errorf("credentials did not reach the body: %v", gotBody)
	}
	auto, _ := itemNamed(items, "auto.example")
	manual, _ := itemNamed(items, "manual.example")
	if auto.Labels[LabelRenewal] != RenewalManaged {
		t.Error(`"1" means auto-renew is on`)
	}
	if manual.Labels[LabelRenewal] == RenewalManaged {
		t.Error(`"0" means it is not`)
	}
	if d := time.Until(auto.Expires).Hours() / 24; d < 69 || d > 71 {
		t.Errorf("its timestamp format did not parse: %v", auto.Expires)
	}
}

// Porkbun reports failure with a 200, the same trap Cloudflare and Namecheap
// set — trusting the status code would read an error as an empty account.
func TestPorkbunFailureInTheBodyIsNotAnEmptyAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ERROR", "message": "Invalid API key",
		})
	}))
	defer srv.Close()

	s := registrarSource("porkbun", srv.URL, RegistrarProvider{Token: "k", Secret: "s"})
	_, err := s.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Invalid API key") {
		t.Fatalf("a 200 carrying an error must not read as success, got %v", err)
	}
}

func TestOneUnreadableRegistrarKeepsTheOthers(t *testing.T) {
	rfc := time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"fqdn": "fine.example", "dates": map[string]any{"registry_ends_at": rfc},
				"autorenew": true},
		})
	}))
	defer ok.Close()
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer denied.Close()

	s := &RegistrarSource{
		BaseURLs: map[string]string{"gandi": ok.URL, "godaddy": denied.URL},
		Providers: []RegistrarProvider{
			{Name: "gandi", Token: "t"},
			{Name: "godaddy", Token: "k", Secret: "s"},
		},
	}
	items, err := s.Collect(context.Background())
	if err == nil {
		t.Fatal("the denied registrar must still be reported")
	}
	if len(items) != 1 {
		t.Fatalf("one denied registrar lost the other's domains: %+v", items)
	}
}

func TestRegistrarValidationRejectsWhatItCannotRead(t *testing.T) {
	if err := ValidateRegistrars([]RegistrarProvider{{Name: "notaregistrar"}}); err == nil ||
		!strings.Contains(err.Error(), "unknown registrar") {
		t.Errorf("an unknown registrar must be rejected at load, got %v", err)
	}
	// DNSimple's domain list is account-scoped; without the id there is
	// nothing to request.
	if err := ValidateRegistrars([]RegistrarProvider{{Name: "dnsimple"}}); err == nil ||
		!strings.Contains(err.Error(), "account id") {
		t.Errorf("dnsimple without an account must be rejected, got %v", err)
	}
	if err := ValidateRegistrars([]RegistrarProvider{{Name: "gandi"}}); err != nil {
		t.Errorf("a valid registrar must load: %v", err)
	}
}

func TestDNSimpleTruncationIsNotSilent(t *testing.T) {
	rfc := time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{
				map[string]any{"name": "a.example", "expires_at": rfc, "auto_renew": true},
			},
			"pagination": map[string]any{"total_entries": 400},
		})
	}))
	defer srv.Close()

	s := registrarSource("dnsimple", srv.URL, RegistrarProvider{Token: "t", Account: "1"})
	items, err := s.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "of 400") {
		t.Fatalf("a truncated read must say so, got %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("what was read should still be reported: %+v", items)
	}
}
