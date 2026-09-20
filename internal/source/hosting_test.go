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

func jsonServer(routes map[string]any, deny map[string]int) (*httptest.Server, *[]string) {
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
			if strings.Contains(r.URL.Path, k) {
				_ = json.NewEncoder(w).Encode(v)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	return srv, &seen
}

func soonRFC() string { return time.Now().Add(25 * 24 * time.Hour).Format(time.RFC3339) }

func TestDigitalOceanDeRanksItsOwnLetsEncryptRenewals(t *testing.T) {
	srv, _ := jsonServer(map[string]any{
		"/v2/certificates": map[string]any{"certificates": []any{
			map[string]any{"id": "1", "name": "auto", "not_after": soonRFC(),
				"type": "lets_encrypt", "state": "verified", "dns_names": []string{"a.example.com"}},
			map[string]any{"id": "2", "name": "uploaded", "not_after": soonRFC(),
				"type": "custom", "state": "verified", "dns_names": []string{"b.example.com"}},
		}},
	}, nil)
	defer srv.Close()

	items, err := (&DigitalOceanSource{Token: "t", BaseURL: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	auto, _ := itemNamed(items, "auto")
	uploaded, _ := itemNamed(items, "uploaded")
	if auto.Labels[LabelRenewal] != RenewalManaged {
		t.Error("DigitalOcean renews its own Let's Encrypt certificates")
	}
	if uploaded.Labels[LabelRenewal] == RenewalManaged {
		t.Error("an uploaded certificate is the one that actually lapses")
	}
}

func TestDigitalOceanReportsADeniedTokenRatherThanNothing(t *testing.T) {
	srv, _ := jsonServer(nil, map[string]int{"/v2/certificates": http.StatusUnauthorized})
	defer srv.Close()

	_, err := (&DigitalOceanSource{Token: "t", BaseURL: srv.URL}).Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "read access") {
		t.Fatalf("want an actionable permission error, got %v", err)
	}
}

// The reason to read a registrar API at all: RDAP knows the date, only the
// registrar knows whether the renewal is actually going to happen.
func TestScalewayAutoRenewIsTheDifferenceFromRDAP(t *testing.T) {
	srv, _ := jsonServer(map[string]any{
		"/domain/v2beta1/domains": map[string]any{"domains": []any{
			map[string]any{"domain": "auto.example", "expired_at": soonRFC(),
				"auto_renew_status": "enabled", "status": "active"},
			map[string]any{"domain": "manual.example", "expired_at": soonRFC(),
				"auto_renew_status": "disabled", "status": "active"},
		}},
	}, nil)
	defer srv.Close()

	items, err := (&ScalewaySource{SecretKey: "k", BaseURL: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	auto, _ := itemNamed(items, "auto.example")
	manual, _ := itemNamed(items, "manual.example")
	if auto.Kind != KindDomain {
		t.Fatalf("want domains: %+v", items)
	}
	if auto.Labels[LabelRenewal] != RenewalManaged {
		t.Error("auto-renew on means somebody else is meeting this date")
	}
	if manual.Labels[LabelRenewal] == RenewalManaged {
		t.Error("auto-renew off is exactly the domain that lapses")
	}
}

func TestScalewayOneDeniedZoneKeepsTheOthers(t *testing.T) {
	srv, _ := jsonServer(map[string]any{
		"/lb/v1/zones/fr-par-1/certificates": map[string]any{"certificates": []any{
			map[string]any{"id": "c1", "name": "edge", "not_valid_after": soonRFC(), "status": "ready"},
		}},
	}, map[string]int{"nl-ams-1": http.StatusForbidden})
	defer srv.Close()

	s := &ScalewaySource{SecretKey: "k", BaseURL: srv.URL,
		Zones: []string{"fr-par-1", "nl-ams-1"}, SkipDomains: true}
	items, err := s.Collect(context.Background())
	if err == nil {
		t.Fatal("the denied zone must still be reported")
	}
	if _, ok := itemNamed(items, "fr-par-1/edge"); !ok {
		t.Fatalf("one denied zone lost the other's certificates: %+v", items)
	}
}

func TestScalewaySkipsScopesItHasNoIdentifierFor(t *testing.T) {
	srv, seen := jsonServer(nil, nil)
	defer srv.Close()

	// No organization id and no zones: nothing to guess at.
	s := &ScalewaySource{SecretKey: "k", BaseURL: srv.URL}
	if _, err := s.Collect(context.Background()); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, p := range *seen {
		if strings.Contains(p, "/iam/") || strings.Contains(p, "/lb/") {
			t.Errorf("guessed at %s with no identifier for it", p)
		}
	}
}

// A key with no expiry is a rotation-policy question, not a deadline this
// source can read — the same line the AWS IAM adapter draws.
func TestScalewayKeysWithoutAnExpiryAreNotDeadlines(t *testing.T) {
	srv, _ := jsonServer(map[string]any{
		"/iam/v1alpha1/api-keys": map[string]any{"api_keys": []any{
			map[string]any{"access_key": "SCW1", "description": "forever", "created_at": soonRFC()},
			map[string]any{"access_key": "SCW2", "description": "bounded", "expires_at": soonRFC()},
		}},
	}, nil)
	defer srv.Close()

	s := &ScalewaySource{SecretKey: "k", BaseURL: srv.URL, OrganizationID: "org", SkipDomains: true}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 || items[0].Name != "bounded" {
		t.Fatalf("only the key with a real expiry is a finding: %+v", items)
	}
}

func TestHostingSourcesNeedTheirCredentialBeforeRunning(t *testing.T) {
	if _, err := (&DigitalOceanSource{}).Collect(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "DIGITALOCEAN_TOKEN") {
		t.Errorf("digitalocean: want the variable named, got %v", err)
	}
	if _, err := (&ScalewaySource{}).Collect(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "SCW_SECRET_KEY") {
		t.Errorf("scaleway: want the variable named, got %v", err)
	}
}

// Reading 100 of 240 and saying nothing looks exactly like an account with 100.
func TestDigitalOceanTruncationIsNotSilent(t *testing.T) {
	srv, _ := jsonServer(map[string]any{
		"/v2/certificates": map[string]any{
			"certificates": []any{
				map[string]any{"id": "1", "name": "one", "not_after": soonRFC(), "type": "custom"},
			},
			"meta": map[string]any{"total": 240},
		},
	}, nil)
	defer srv.Close()

	items, err := (&DigitalOceanSource{Token: "t", BaseURL: srv.URL}).Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "of 240") {
		t.Fatalf("a truncated read must say so, got %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("what was read should still be reported: %+v", items)
	}
}

func TestScalewayTruncationIsNotSilent(t *testing.T) {
	srv, _ := jsonServer(map[string]any{
		"/domain/v2beta1/domains": map[string]any{
			"domains": []any{
				map[string]any{"domain": "a.example", "expired_at": soonRFC(), "auto_renew_status": "enabled"},
			},
			"total_count": 500,
		},
	}, nil)
	defer srv.Close()

	items, err := (&ScalewaySource{SecretKey: "k", BaseURL: srv.URL}).Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "of 500") {
		t.Fatalf("a truncated read must say so, got %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("what was read should still be reported: %+v", items)
	}
}
