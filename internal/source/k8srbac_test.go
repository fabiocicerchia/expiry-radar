package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// docs/rbac-readonly.yaml is what users apply, so every request the default
// namespaced collectors make must be granted by its Role, and nothing the Role
// grants may go unused.
func TestRBACRoleDocCoversEveryRequest(t *testing.T) {
	raw, err := os.ReadFile("../../docs/rbac-readonly.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var live []string // the YAML that is applied; the ClusterRoles are commented out
	for _, l := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") {
			live = append(live, l)
		}
	}
	text := strings.Join(live, "\n")
	ns := regexp.MustCompile(`kind: Role\nmetadata:\n  name: \S+\n  namespace: (\S+)`).FindStringSubmatch(text)
	if ns == nil {
		t.Fatal("found no namespaced Role; the pattern no longer matches the doc")
	}
	rule := regexp.MustCompile(`apiGroups: \["([^"]*)"\]\s+resources: \["([^"]+)"\]\s+verbs: \["([^"]+)"\]`)
	granted := map[string]bool{} // "group resource verb"
	for _, m := range rule.FindAllStringSubmatch(text, -1) {
		granted[m[1]+" "+m[2]+" "+m[3]] = false
	}
	if len(granted) == 0 {
		t.Fatal("found no rules; the pattern no longer matches the doc")
	}

	secrets := secretList(t, selfSignedPEM(t, "shop.example.com", time.Now().Add(30*24*time.Hour)))
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/secrets") {
			_, _ = w.Write([]byte(secrets))
			return
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	s := &K8sSource{Server: srv.URL, Namespaces: []string{ns[1]}, CertManager: true}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := itemNamed(items, "prod/shop-tls"); !ok {
		t.Fatalf("want the TLS secret's certificate, got %+v", items)
	}

	// /api/v1/namespaces/NS/RES is the core group; /apis/GROUP/v1/namespaces/NS/RES the rest.
	path := regexp.MustCompile(`^/(?:api/v1|apis/([^/]+)/v1)/namespaces/([^/]+)/([^/]+)$`)
	for _, r := range requests {
		method, p, _ := strings.Cut(r, " ")
		m := path.FindStringSubmatch(p)
		if method != http.MethodGet || m == nil || m[2] != ns[1] {
			t.Errorf("%s is not a list in namespace %s, which is all the Role grants", r, ns[1])
			continue
		}
		key := m[1] + " " + m[3] + " list"
		if _, ok := granted[key]; !ok {
			t.Errorf("%s is not granted by the Role", r)
		}
		granted[key] = true
	}
	for k, used := range granted {
		if !used {
			t.Errorf("Role grants %q but the source never needs it", k)
		}
	}
}
