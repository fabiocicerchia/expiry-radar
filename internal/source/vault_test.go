package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// docs/vault-readonly-policy.hcl is what users attach, so every request the
// source makes must be granted by it, and nothing it grants may go unused.
func TestVaultPolicyDocCoversEveryRequest(t *testing.T) {
	raw, err := os.ReadFile("../../docs/vault-readonly-policy.hcl")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`path "([^"]+)" \{\s*capabilities = \[([^\]]*)\]`)
	granted := map[string]string{} // "path capability" -> path pattern
	for _, m := range block.FindAllStringSubmatch(string(raw), -1) {
		for _, c := range strings.Split(m[2], ",") {
			granted[m[1]+" "+strings.Trim(strings.TrimSpace(c), `"`)] = m[1]
		}
	}
	if len(granted) == 0 {
		t.Fatal("found no path blocks; the pattern no longer matches the policy")
	}

	expires := time.Now().Add(20 * 24 * time.Hour)
	certPEM := string(selfSignedPEM(t, "api.example.com", expires))
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Header.Get("X-Vault-Token") != "s.test" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var body any
		switch {
		case r.URL.Path == "/v1/auth/token/lookup-self":
			body = map[string]any{"data": map[string]string{
				"display_name": "token-radar", "expire_time": expires.Format(time.RFC3339)}}
		case r.Method == "LIST" && r.URL.Path == "/v1/pki/certs":
			body = map[string]any{"data": map[string][]string{"keys": {"01-02"}}}
		case r.URL.Path == "/v1/pki/cert/01-02":
			body = map[string]any{"data": map[string]string{"certificate": certPEM}}
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	s := &VaultSource{Addr: srv.URL, Token: "s.test", PKIMounts: []string{"pki"}}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Name != "token-radar" || items[1].Name != "api.example.com" {
		t.Fatalf("want the token and the PKI cert, got %+v", items)
	}

	capability := map[string]string{http.MethodGet: "read", "LIST": "list"}
	used := map[string]bool{}
	for _, r := range requests {
		method, path, _ := strings.Cut(r, " ")
		path = strings.TrimPrefix(path, "/v1/")
		match := ""
		for k, pattern := range granted {
			if !strings.HasSuffix(k, " "+capability[method]) {
				continue
			}
			// A trailing * is Vault's prefix glob.
			if p, glob := strings.CutSuffix(pattern, "*"); path == pattern || glob && strings.HasPrefix(path, p) {
				match = k
			}
		}
		if match == "" {
			t.Errorf("%s is not granted by the policy", r)
		}
		used[match] = true
	}
	for k := range granted {
		if !used[k] {
			t.Errorf("policy grants %q but the source never needs it", k)
		}
	}
}
