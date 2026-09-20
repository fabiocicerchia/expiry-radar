package source

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gcpServer answers the token exchange and whatever API paths are routed.
func gcpServer(routes map[string]any) (*httptest.Server, *[]string) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		if strings.Contains(r.URL.Path, "/token") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "ya29.test", "expires_in": 3600,
			})
			return
		}
		for k, v := range routes {
			if strings.HasSuffix(r.URL.Path, k) {
				_ = json.NewEncoder(w).Encode(v)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	return srv, &seen
}

// writeKeyFile mints a real RSA service account key, so the assertion under
// test is signed the way a live one would be rather than stubbed out.
func writeKeyFile(t *testing.T, tokenURI string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{
		"type":         "service_account",
		"client_email": "radar@acme.iam.gserviceaccount.com",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"token_uri":    tokenURI,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sa.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func gcpSource(t *testing.T, srvURL string) *GCPSource {
	t.Helper()
	return &GCPSource{
		Projects:        []string{"acme-prod"},
		CredentialsFile: writeKeyFile(t, srvURL+"/token"),
		Endpoints: map[string]string{
			"certificatemanager": srvURL, "compute": srvURL,
			"secretmanager": srvURL, "iam": srvURL,
		},
	}
}

// The whole auth path, exercised end to end: a real RSA key signs a real
// assertion, which is exchanged for a token that is then used.
func TestGCPSignsItsOwnAssertionAndUsesTheToken(t *testing.T) {
	soon := time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	var authSeen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/token") {
			_ = r.ParseForm()
			if !strings.HasPrefix(r.FormValue("assertion"), "eyJ") {
				t.Errorf("assertion does not look like a JWT: %q", r.FormValue("assertion"))
			}
			if g := r.FormValue("grant_type"); g != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
				t.Errorf("grant_type = %q", g)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29.test", "expires_in": 3600})
			return
		}
		if authSeen == "" {
			authSeen = r.Header.Get("Authorization")
		}
		if strings.Contains(r.URL.Path, "sslCertificates") {
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{
				map[string]any{"name": "lb-cert", "expireTime": soon, "type": "MANAGED"},
			}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	items, err := gcpSource(t, srv.URL).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if authSeen != "Bearer ya29.test" {
		t.Errorf("the exchanged token was not used: %q", authSeen)
	}
	if _, ok := itemNamed(items, "lb-cert"); !ok {
		t.Fatalf("want the compute certificate: %+v", items)
	}
}

func TestGCPManagedCertificatesAreDeRanked(t *testing.T) {
	soon := time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := gcpServer(map[string]any{
		"sslCertificates": map[string]any{"items": []any{
			map[string]any{"name": "managed", "expireTime": soon, "type": "MANAGED"},
			map[string]any{"name": "uploaded", "expireTime": soon, "type": "SELF_MANAGED"},
		}},
	})
	defer srv.Close()

	items, err := gcpSource(t, srv.URL).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	managed, _ := itemNamed(items, "managed")
	uploaded, _ := itemNamed(items, "uploaded")
	if managed.Labels[LabelRenewal] != RenewalManaged {
		t.Error("Google renews a MANAGED certificate")
	}
	if uploaded.Labels[LabelRenewal] == RenewalManaged {
		t.Error("a self-managed certificate is nobody's job but yours")
	}
}

// A user-managed key is valid for about ten years, which is not a deadline
// anybody means — so the policy date wins and the label says it is a policy.
func TestGCPServiceAccountKeysUseThePolicyDate(t *testing.T) {
	created := time.Now().Add(-120 * 24 * time.Hour).Format(time.RFC3339)
	tenYears := time.Now().Add(3650 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := gcpServer(map[string]any{
		"serviceAccounts": map[string]any{"accounts": []any{
			map[string]any{"name": "projects/acme-prod/serviceAccounts/ci@acme.iam.gserviceaccount.com",
				"email": "ci@acme.iam.gserviceaccount.com"},
		}},
		"/keys": map[string]any{"keys": []any{
			map[string]any{"name": "projects/x/serviceAccounts/ci/keys/abc123",
				"validAfterTime": created, "validBeforeTime": tenYears, "keyType": "USER_MANAGED"},
		}},
	})
	defer srv.Close()

	s := gcpSource(t, srv.URL)
	s.MaxKeyAgeDays = 90
	s.SkipCertManager, s.SkipCompute, s.SkipSecrets = true, true, true

	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want the key: %+v", items)
	}
	it := items[0]
	if it.Labels["deadline"] != "rotation policy" {
		t.Errorf("deadline = %q; a ten-year validity is not the deadline anybody means",
			it.Labels["deadline"])
	}
	if it.Labels["policy.days"] != "90" || it.Labels["created"] == "" {
		t.Errorf("the labels must show where the date came from: %v", it.Labels)
	}
	// created + 90d on a 120-day-old key is already past.
	if it.Expires.After(time.Now()) {
		t.Errorf("a 120-day-old key on a 90-day policy is overdue, got %v", it.Expires)
	}
}

// Secret Manager carries two different dates and neither is the other.
func TestGCPSecretsReportExpiryAndRotationSeparately(t *testing.T) {
	expire := time.Now().Add(50 * 24 * time.Hour).Format(time.RFC3339)
	rotate := time.Now().Add(10 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := gcpServer(map[string]any{
		"/secrets": map[string]any{"secrets": []any{
			map[string]any{"name": "projects/acme-prod/secrets/db-password",
				"expireTime": expire,
				"rotation":   map[string]any{"nextRotationTime": rotate}},
		}},
	})
	defer srv.Close()

	s := gcpSource(t, srv.URL)
	s.SkipCertManager, s.SkipCompute, s.SkipKeys = true, true, true
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("an expiry and a rotation are two deadlines: %+v", items)
	}
	if _, ok := itemNamed(items, "db-password"); !ok {
		t.Error("want the expiry row")
	}
	if _, ok := itemNamed(items, "db-password (rotation)"); !ok {
		t.Error("want the rotation row")
	}
}

// An API that is simply not enabled on a project answers 404. That is an
// answer about the project, not a failure.
func TestGCPTreatsADisabledAPIAsAnAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/token") {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	items, err := gcpSource(t, srv.URL).Collect(context.Background())
	if err != nil {
		t.Fatalf("an API that is not enabled is not a problem: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("nothing to report: %+v", items)
	}
}

func TestGCPWithoutCredentialsSaysWhatToDo(t *testing.T) {
	s := &GCPSource{Projects: []string{"p"}, Timeout: time.Second,
		Endpoints: map[string]string{"metadata": "http://127.0.0.1:1/token"}}
	_, err := s.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "credentialsFile") {
		t.Fatalf("want an actionable error naming both routes, got %v", err)
	}
}
