package source

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

// fakeK8s serves canned responses keyed by the longest matching path fragment,
// so a named get under /secrets/ does not collide with the secrets list. Any
// path with no route answers 404, which is what a cluster without cert-manager
// or without a mesh actually does.
func fakeK8s(routes map[string]string, deny map[string]int) *httptest.Server {
	keys := make([]string, 0, len(routes))
	for k := range routes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })

	denied := make([]string, 0, len(deny))
	for k := range deny {
		denied = append(denied, k)
	}
	sort.Slice(denied, func(i, j int) bool { return len(denied[i]) > len(denied[j]) })

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		for _, d := range denied {
			if strings.Contains(path, d) {
				w.WriteHeader(deny[d])
				return
			}
		}
		for _, k := range keys {
			if strings.Contains(path, k) {
				_, _ = w.Write([]byte(routes[k]))
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func secretList(t *testing.T, namespace, name string, pemBytes []byte) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"items": []map[string]any{{
		"metadata": map[string]string{"name": name, "namespace": namespace},
		"type":     "kubernetes.io/tls",
		"data":     map[string][]byte{"tls.crt": pemBytes},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func webhookConfigList(t *testing.T, name string, pemBytes []byte) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"items": []map[string]any{{
		"metadata": map[string]string{"name": name},
		"webhooks": []map[string]any{{
			"name":         "w.example.com",
			"clientConfig": map[string]any{"caBundle": pemBytes},
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func itemNamed(items []Item, name string) (Item, bool) {
	for _, it := range items {
		if it.Name == name {
			return it, true
		}
	}
	return Item{}, false
}

// The rule nobody could check before: one denied resource class must not take
// the rest of the cluster's findings with it.
func TestOneDeniedResourceKeepsTheOthersFindings(t *testing.T) {
	certPEM, _ := selfSignedPEM(t, "shop.example.com", time.Now().Add(30*24*time.Hour))
	caPEM, _ := selfSignedPEM(t, "webhook-ca", time.Now().Add(10*24*time.Hour))

	srv := fakeK8s(map[string]string{
		"secrets":                         secretList(t, "prod", "shop-tls", certPEM),
		"validatingwebhookconfigurations": webhookConfigList(t, "cert-manager-webhook", caPEM),
	}, map[string]int{
		"validatingwebhookconfigurations": http.StatusForbidden,
	})
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if err == nil {
		t.Fatal("a denied resource must still be reported as an error")
	}
	if !strings.Contains(err.Error(), "webhooks:") {
		t.Errorf("the error should name the resource that failed, got %v", err)
	}
	if _, ok := itemNamed(items, "prod/shop-tls"); !ok {
		t.Fatalf("the denied webhook list lost the secrets' findings: %+v", items)
	}
}

func TestADeniedNamespaceKeepsTheOtherNamespacesSecrets(t *testing.T) {
	certPEM, _ := selfSignedPEM(t, "shop.example.com", time.Now().Add(30*24*time.Hour))

	srv := fakeK8s(map[string]string{
		"namespaces/prod/secrets": secretList(t, "prod", "shop-tls", certPEM),
	}, map[string]int{
		"namespaces/staging/secrets": http.StatusForbidden,
	})
	defer srv.Close()

	s := &K8sSource{Server: srv.URL, Namespaces: []string{"prod", "staging"}}
	items, err := s.Collect(context.Background())
	if err == nil {
		t.Fatal("the denied namespace should still surface as an error")
	}
	if _, ok := itemNamed(items, "prod/shop-tls"); !ok {
		t.Fatalf("one forbidden namespace lost the other namespace's secrets: %+v", items)
	}
}

func TestASkippedResourceIsNotAFailure(t *testing.T) {
	items, results, err := collectResources([]k8sResource{
		{"webhooks", true, func() ([]Item, error) { t.Fatal("a skipped resource must not be collected"); return nil, nil }},
		{"secrets", false, func() ([]Item, error) { return []Item{{Name: "prod/shop-tls"}}, nil }},
	})
	if err != nil {
		t.Fatalf("skipping is not an error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want the one collected item, got %d", len(items))
	}
	if !results[0].Skipped || results[0].Err != nil {
		t.Errorf("a skipped resource must stay distinguishable from a denied one: %+v", results[0])
	}
}

func TestPartialItemsFromAFailingResourceAreStillKept(t *testing.T) {
	items, results, err := collectResources([]k8sResource{
		{"secrets", false, func() ([]Item, error) {
			return []Item{{Name: "prod/a"}}, errors.New("namespace staging: forbidden")
		}},
	})
	if err == nil {
		t.Fatal("a partial read is still an error")
	}
	if len(items) != 1 {
		t.Fatalf("the items read before the failure were thrown away: %+v", items)
	}
	if results[0].Items != 1 || results[0].Err == nil {
		t.Errorf("the result should record both what was read and what failed: %+v", results[0])
	}
}

func TestAnEmptyResourceIsNotASkippedOne(t *testing.T) {
	_, results, err := collectResources([]k8sResource{
		{"certificates", false, func() ([]Item, error) { return nil, nil }},
	})
	if err != nil {
		t.Fatalf("an empty cluster is not a failure: %v", err)
	}
	if results[0].Skipped {
		t.Error("a cluster with no cert-manager Certificates must not read as one where the adapter was skipped")
	}
}

func certificateList(t *testing.T, namespace, name, secretName, notAfter, renewalTime, ready string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"items": []map[string]any{{
		"metadata": map[string]string{"name": name, "namespace": namespace},
		"spec": map[string]any{
			"secretName": secretName,
			"dnsNames":   []string{"shop.example.com"},
			"issuerRef":  map[string]string{"name": "letsencrypt", "kind": "ClusterIssuer"},
		},
		"status": map[string]any{
			"notAfter":    notAfter,
			"renewalTime": renewalTime,
			"conditions":  []map[string]string{{"type": "Ready", "status": ready}},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestAHealthyCertManagerCertificateMarksItsSecretManaged(t *testing.T) {
	now := time.Now()
	certPEM, _ := selfSignedPEM(t, "shop.example.com", now.Add(30*24*time.Hour))

	srv := fakeK8s(map[string]string{
		"secrets": secretList(t, "prod", "shop-tls", certPEM),
		"certificates": certificateList(t, "prod", "shop", "shop-tls",
			now.Add(30*24*time.Hour).Format(time.RFC3339),
			now.Add(15*24*time.Hour).Format(time.RFC3339), "True"),
	}, nil)
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("a Certificate and the secret it manages are one finding, got %d: %+v", len(items), items)
	}
	if got := items[0].Labels[LabelRenewal]; got != RenewalManaged {
		t.Errorf("renewal = %q, want %q", got, RenewalManaged)
	}
	if items[0].Labels["cert-manager"] != "shop" {
		t.Errorf("the managing Certificate should be named on the item: %v", items[0].Labels)
	}
}

func TestAStuckCertManagerRenewalIsNotMarkedManaged(t *testing.T) {
	now := time.Now()
	certPEM, _ := selfSignedPEM(t, "shop.example.com", now.Add(30*24*time.Hour))

	srv := fakeK8s(map[string]string{
		"secrets": secretList(t, "prod", "shop-tls", certPEM),
		// Ready=False is cert-manager saying the renewal it owns is failing.
		"certificates": certificateList(t, "prod", "shop", "shop-tls",
			now.Add(30*24*time.Hour).Format(time.RFC3339),
			now.Add(-2*24*time.Hour).Format(time.RFC3339), "False"),
	}, nil)
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if got := items[0].Labels[LabelRenewal]; got != RenewalStuck {
		t.Errorf("renewal = %q, want %q — a stuck renewal must not earn the de-rank", got, RenewalStuck)
	}
}

func TestACertificateWithNoIssuedSecretIsStillReported(t *testing.T) {
	now := time.Now()
	srv := fakeK8s(map[string]string{
		"secrets": `{"items":[]}`,
		"certificates": certificateList(t, "prod", "shop", "shop-tls",
			"", "", "False"),
	}, nil)
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL, now: func() time.Time { return now }}).
		Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	it, ok := itemNamed(items, "prod/shop-tls")
	if !ok {
		t.Fatalf("a Certificate that never produced its secret is a finding: %+v", items)
	}
	if it.Source != "k8s:cert-manager" {
		t.Errorf("source = %q", it.Source)
	}
	if !it.Expires.Equal(now) {
		t.Errorf("nothing was ever issued, so the deadline is now, got %v", it.Expires)
	}
}

func TestTheSameWebhookCAInTwoConfigurationsIsReportedOnce(t *testing.T) {
	caPEM, _ := selfSignedPEM(t, "cert-manager-ca", time.Now().Add(10*24*time.Hour))

	srv := fakeK8s(map[string]string{
		"validatingwebhookconfigurations": webhookConfigList(t, "cert-manager-validating", caPEM),
		"mutatingwebhookconfigurations":   webhookConfigList(t, "cert-manager-mutating", caPEM),
	}, nil)
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("one CA pinned into two configurations is one finding, got %d: %+v", len(items), items)
	}
	if items[0].Kind != KindTrustAnchor {
		t.Errorf("kind = %q, want %q", items[0].Kind, KindTrustAnchor)
	}
	used := items[0].Labels["used-by"]
	if !strings.Contains(used, "cert-manager-validating") || !strings.Contains(used, "cert-manager-mutating") {
		t.Errorf("both configurations should be named as relying on it, got %q", used)
	}
}

func TestAWebhookWithNoCABundleIsSkipped(t *testing.T) {
	srv := fakeK8s(map[string]string{
		"validatingwebhookconfigurations": `{"items":[{
			"metadata":{"name":"injected"},
			"webhooks":[{"name":"w.example.com","clientConfig":{}}]}]}`,
	}, nil)
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("CA injection means there is nothing to expire: %+v", items)
	}
}

func TestALocalAPIServiceIsSkipped(t *testing.T) {
	caPEM, _ := selfSignedPEM(t, "front-proxy-ca", time.Now().Add(20*24*time.Hour))
	body, err := json.Marshal(map[string]any{"items": []map[string]any{
		{"metadata": map[string]string{"name": "v1."}, "spec": map[string]any{}},
		{"metadata": map[string]string{"name": "v1beta1.metrics.k8s.io"}, "spec": map[string]any{
			"caBundle": caPEM,
			"service":  map[string]string{"name": "metrics-server", "namespace": "kube-system"},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	srv := fakeK8s(map[string]string{"apiservices": string(body)}, nil)
	defer srv.Close()

	items, cErr := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if cErr != nil {
		t.Fatalf("collect: %v", cErr)
	}
	if len(items) != 1 {
		t.Fatalf("only the aggregated APIService has a bundle that expires, got %+v", items)
	}
	if items[0].Name != "apiservice/v1beta1.metrics.k8s.io" {
		t.Errorf("name = %q", items[0].Name)
	}
	if items[0].Labels["service"] != "kube-system/metrics-server" {
		t.Errorf("the backing service should be recorded: %v", items[0].Labels)
	}
}

func TestAMissingMeshAnchorIsNotAnError(t *testing.T) {
	srv := fakeK8s(nil, nil) // everything 404s: no Linkerd, no Istio
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("a cluster with no service mesh is not a cluster with a problem: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("nothing should be reported: %+v", items)
	}
}

func TestAMeshTrustAnchorIsReadFromItsConfigMap(t *testing.T) {
	caPEM, _ := selfSignedPEM(t, "identity.linkerd.cluster.local", time.Now().Add(60*24*time.Hour))
	body, err := json.Marshal(map[string]any{"data": map[string]string{"ca-bundle.crt": string(caPEM)}})
	if err != nil {
		t.Fatal(err)
	}

	srv := fakeK8s(map[string]string{
		"configmaps/linkerd-identity-trust-roots": string(body),
	}, nil)
	defer srv.Close()

	items, cErr := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if cErr != nil {
		t.Fatalf("collect: %v", cErr)
	}
	if len(items) != 1 {
		t.Fatalf("want the trust root, got %+v", items)
	}
	if items[0].Kind != KindTrustAnchor || items[0].Labels["mesh"] != "linkerd" {
		t.Errorf("kind/mesh = %q / %q", items[0].Kind, items[0].Labels["mesh"])
	}
	if items[0].Labels["role"] != "trust-anchor" {
		t.Errorf("role = %q", items[0].Labels["role"])
	}
}

func TestClusterScopedResourcesDoNotGoThroughTheNamespacePaths(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, _ = (&K8sSource{Server: srv.URL, Namespaces: []string{"prod"}}).Collect(context.Background())
	for _, p := range seen {
		if strings.Contains(p, "webhookconfigurations") && strings.Contains(p, "namespaces/") {
			t.Errorf("a cluster-scoped resource was requested under a namespace: %s", p)
		}
	}
}
