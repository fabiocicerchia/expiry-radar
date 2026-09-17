package source

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

// fakeK8s serves canned responses keyed by a path suffix, so the secrets *list*
// at /api/v1/secrets and a named get at /api/v1/namespaces/linkerd/secrets/x
// cannot be confused for one another — a substring match routes the named get
// to the list body and makes the object look present but empty. Any path with
// no route answers 404, which is what a cluster without cert-manager or without
// a mesh actually does.
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
			if routeMatches(path, d) {
				w.WriteHeader(deny[d])
				return
			}
		}
		for _, k := range keys {
			if routeMatches(path, k) {
				_, _ = w.Write([]byte(routes[k]))
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

// routeMatches anchors on the end of the path: "secrets" is the list endpoint,
// never a named object under it.
func routeMatches(path, key string) bool {
	return path == key || strings.HasSuffix(path, "/"+key)
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

func configMapBody(t *testing.T, data map[string]string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func secretBody(t *testing.T, data map[string][]byte) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// A secret we cannot read is not a secret that is not there, and neither is a
// secret we never asked for. Conflating the two made a managed certificate
// vanish from the inventory with no error at all.
func TestACertificateWhoseSecretIsUnreadableIsStillReported(t *testing.T) {
	now := time.Now()
	corrupt := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not a certificate")})
	notAfter := now.Add(30 * 24 * time.Hour)

	srv := fakeK8s(map[string]string{
		"secrets": secretList(t, "prod", "shop-tls", corrupt),
		"certificates": certificateList(t, "prod", "shop", "shop-tls",
			notAfter.Format(time.RFC3339), now.Add(15*24*time.Hour).Format(time.RFC3339), "True"),
	}, nil)
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL, now: func() time.Time { return now }}).
		Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	it, ok := itemNamed(items, "prod/shop-tls")
	if !ok {
		t.Fatalf("an unreadable secret must not delete the certificate from the inventory: %+v", items)
	}
	if it.Labels["secret-state"] != "unreadable" {
		t.Errorf("secret-state = %q, want %q", it.Labels["secret-state"], "unreadable")
	}
	if !it.Expires.Equal(notAfter.Truncate(time.Second)) && it.Expires.Unix() != notAfter.Unix() {
		t.Errorf("the deadline should come from the Certificate, got %v want %v", it.Expires, notAfter)
	}
}

// Skipping secrets used to make every Certificate look like it had never been
// issued, which put the whole cluster at the top of the report as expired.
func TestSkippingSecretsDoesNotMakeEveryCertificateLookUnissued(t *testing.T) {
	now := time.Now()
	notAfter := now.Add(30 * 24 * time.Hour)

	srv := fakeK8s(map[string]string{
		"certificates": certificateList(t, "prod", "shop", "shop-tls",
			notAfter.Format(time.RFC3339), now.Add(15*24*time.Hour).Format(time.RFC3339), "True"),
	}, nil)
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL, SkipSecrets: true, now: func() time.Time { return now }}).
		Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	it, ok := itemNamed(items, "prod/shop-tls")
	if !ok {
		t.Fatalf("want the certificate, got %+v", items)
	}
	if it.Labels["secret-state"] == "missing" {
		t.Error(`claimed the secret is missing without ever having read the secrets`)
	}
	if it.Expires.Unix() == now.Unix() {
		t.Error("fell back to expires=now, which only the 'we looked and it is gone' case has earned")
	}
}

func TestUnissuedIsOnlyClaimedForANamespaceWeActuallyRead(t *testing.T) {
	now := time.Now()
	notAfter := now.Add(40 * 24 * time.Hour)

	srv := fakeK8s(map[string]string{
		"namespaces/prod/secrets": `{"items":[]}`,
		"namespaces/prod/certificates": certificateList(t, "prod", "shop", "shop-tls",
			notAfter.Format(time.RFC3339), "", "False"),
		"namespaces/staging/certificates": certificateList(t, "staging", "dash", "dash-tls",
			notAfter.Format(time.RFC3339), "", "False"),
	}, map[string]int{
		"namespaces/staging/secrets": http.StatusForbidden,
	})
	defer srv.Close()

	s := &K8sSource{Server: srv.URL, Namespaces: []string{"prod", "staging"}, now: func() time.Time { return now }}
	items, _ := s.Collect(context.Background())

	prod, ok := itemNamed(items, "prod/shop-tls")
	if !ok {
		t.Fatalf("want the prod certificate: %+v", items)
	}
	if prod.Labels["secret-state"] != "missing" {
		t.Errorf("prod secrets were read and the secret was absent, so it is missing; got %q",
			prod.Labels["secret-state"])
	}

	staging, ok := itemNamed(items, "staging/dash-tls")
	if !ok {
		t.Fatalf("want the staging certificate: %+v", items)
	}
	if staging.Labels["secret-state"] == "missing" {
		t.Error("staging secrets were denied, so nothing there can be called missing")
	}
}

func TestUnissuedCertificatesComeOutInAStableOrder(t *testing.T) {
	now := time.Now()
	body, err := json.Marshal(map[string]any{"items": []map[string]any{
		{"metadata": map[string]string{"name": "zeta", "namespace": "prod"},
			"spec": map[string]any{"secretName": "zeta-tls"}, "status": map[string]any{}},
		{"metadata": map[string]string{"name": "alpha", "namespace": "prod"},
			"spec": map[string]any{"secretName": "alpha-tls"}, "status": map[string]any{}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	srv := fakeK8s(map[string]string{"secrets": `{"items":[]}`, "certificates": string(body)}, nil)
	defer srv.Close()

	for i := range 5 {
		items, cErr := (&K8sSource{Server: srv.URL, now: func() time.Time { return now }}).
			Collect(context.Background())
		if cErr != nil {
			t.Fatalf("collect: %v", cErr)
		}
		if len(items) != 2 {
			t.Fatalf("want both, got %+v", items)
		}
		if items[0].Name != "prod/alpha-tls" || items[1].Name != "prod/zeta-tls" {
			t.Fatalf("run %d came out in a different order: %s, %s", i, items[0].Name, items[1].Name)
		}
	}
}

// Istio's cacerts holds the root and the intermediate under different keys, on
// different clocks. Reading one and labelling it "trust-anchor" reported the
// wrong certificate under the right name.
func TestBothIstioCACertsAreReportedWithTheirOwnRoles(t *testing.T) {
	rootPEM, _ := selfSignedPEM(t, "istio-root", time.Now().Add(300*24*time.Hour))
	caPEM, _ := selfSignedPEM(t, "istio-intermediate", time.Now().Add(20*24*time.Hour))

	srv := fakeK8s(map[string]string{
		"secrets/cacerts": secretBody(t, map[string][]byte{
			"root-cert.pem": rootPEM,
			"ca-cert.pem":   caPEM,
		}),
	}, nil)
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("both the root and the intermediate are findings, got %d: %+v", len(items), items)
	}
	roles := map[string]string{}
	for _, it := range items {
		roles[it.Labels["key"]] = it.Labels["role"]
	}
	if roles["root-cert.pem"] != "trust-anchor" {
		t.Errorf("root-cert.pem role = %q", roles["root-cert.pem"])
	}
	if roles["ca-cert.pem"] != "issuer" {
		t.Errorf("ca-cert.pem role = %q — the intermediate must not be labelled the anchor", roles["ca-cert.pem"])
	}
}

func TestAnAnchorWhoseKeyIsMissingWarnsRatherThanDisappearing(t *testing.T) {
	srv := fakeK8s(map[string]string{
		// The object is there; somebody renamed the key.
		"configmaps/linkerd-identity-trust-roots": configMapBody(t, map[string]string{"bundle.pem": "x"}),
	}, nil)
	defer srv.Close()

	_, err := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if err == nil {
		t.Fatal("a configured anchor that is present but unreadable must not be silently dropped")
	}
	if !strings.Contains(err.Error(), "ca-bundle.crt") {
		t.Errorf("the warning should name the keys it looked for, got %v", err)
	}
}

func TestTwoCAsWithTheSameIssuerCNAndSerialAreBothReported(t *testing.T) {
	// selfSigned mints every certificate with serial 1, so a same-CN pair is
	// exactly the collision an issuer+serial key cannot tell apart.
	a, _ := selfSignedPEM(t, "kubernetes", time.Now().Add(10*24*time.Hour))
	b, _ := selfSignedPEM(t, "kubernetes", time.Now().Add(200*24*time.Hour))

	srv := fakeK8s(map[string]string{
		"validatingwebhookconfigurations": webhookConfigList(t, "one", a),
		"mutatingwebhookconfigurations":   webhookConfigList(t, "two", b),
	}, nil)
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("two different CAs are two findings, got %d: %+v", len(items), items)
	}
}

func TestACASharedByAWebhookAndAnAPIServiceIsReportedOnce(t *testing.T) {
	caPEM, _ := selfSignedPEM(t, "front-proxy-ca", time.Now().Add(15*24*time.Hour))
	apis, err := json.Marshal(map[string]any{"items": []map[string]any{{
		"metadata": map[string]string{"name": "v1beta1.metrics.k8s.io"},
		"spec": map[string]any{"caBundle": caPEM,
			"service": map[string]string{"name": "metrics-server", "namespace": "kube-system"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	srv := fakeK8s(map[string]string{
		"validatingwebhookconfigurations": webhookConfigList(t, "aggregator", caPEM),
		"apiservices":                     string(apis),
	}, nil)
	defer srv.Close()

	items, cErr := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if cErr != nil {
		t.Fatalf("collect: %v", cErr)
	}
	if len(items) != 1 {
		t.Fatalf("one CA across two resource classes is one finding, got %d: %+v", len(items), items)
	}
	if used := items[0].Labels["used-by"]; !strings.Contains(used, "aggregator") ||
		!strings.Contains(used, "v1beta1.metrics.k8s.io") {
		t.Errorf("both objects should be named as relying on it, got %q", used)
	}
}

func TestACorruptLeafIsSkippedRatherThanReportedWithTheChainsExpiry(t *testing.T) {
	chainPEM, _ := selfSignedPEM(t, "intermediate", time.Now().Add(300*24*time.Hour))
	corrupt := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not a certificate")})

	srv := fakeK8s(map[string]string{
		"secrets": secretList(t, "prod", "shop-tls", append(corrupt, chainPEM...)),
	}, nil)
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("an unreadable leaf must not be reported with an intermediate's date standing in: %+v", items)
	}
}

// Ranking amplifies hosts — wildcards and multi-SAN coverage both add to blast
// radius. A CA's SANs are not hosts it fronts, so handing them over would earn
// a trust anchor a bonus meant for a leaf that actually serves those names.
func TestATrustAnchorCarriesNoHostsForRankingToAmplify(t *testing.T) {
	caPEM, _ := selfSignedPEM(t, "wildcard-ca", time.Now().Add(10*24*time.Hour))

	srv := fakeK8s(map[string]string{
		"validatingwebhookconfigurations": webhookConfigList(t, "wc", caPEM),
	}, nil)
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if got := items[0].Labels[LabelHosts]; got != "" {
		t.Errorf("hosts = %q, want none", got)
	}
}

// On a stock Istio cluster the sidecar-injector webhook pins the same root as
// istio-system/cacerts, and webhooks is collected first. The item must end up
// with what the mesh collector knew, not just what the webhook did.
func TestACAKnownToTwoCollectorsKeepsWhatBothKnew(t *testing.T) {
	rootPEM, _ := selfSignedPEM(t, "istio-root", time.Now().Add(100*24*time.Hour))

	srv := fakeK8s(map[string]string{
		"validatingwebhookconfigurations": webhookConfigList(t, "istio-sidecar-injector", rootPEM),
		"secrets/cacerts":                 secretBody(t, map[string][]byte{"root-cert.pem": rootPEM}),
	}, nil)
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("one CA is one finding, got %d: %+v", len(items), items)
	}
	got := items[0]
	if got.Namespace != "istio-system" {
		t.Errorf("namespace = %q, want istio-system — grouping and namespace overrides need it", got.Namespace)
	}
	for k, want := range map[string]string{"mesh": "istio", "role": "trust-anchor", "key": "root-cert.pem"} {
		if got.Labels[k] != want {
			t.Errorf("label %s = %q, want %q", k, got.Labels[k], want)
		}
	}
	if used := got.Labels["used-by"]; !strings.Contains(used, "istio-sidecar-injector") ||
		!strings.Contains(used, "cacerts") {
		t.Errorf("used-by = %q, want both objects", used)
	}
}

// cert-manager reporting Ready is a claim, not a fact. If the secret it was
// supposed to produce is gone, that claim is contradicted — and de-ranking on
// it inverts exactly what the renewal adjustment exists to do.
func TestADeletedSecretCannotBeCalledAHealthyRenewal(t *testing.T) {
	now := time.Now()
	srv := fakeK8s(map[string]string{
		"secrets": `{"items":[]}`,
		"certificates": certificateList(t, "prod", "shop", "shop-tls",
			now.Add(30*24*time.Hour).Format(time.RFC3339),
			now.Add(15*24*time.Hour).Format(time.RFC3339), "True"),
	}, nil)
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL, now: func() time.Time { return now }}).
		Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	it, ok := itemNamed(items, "prod/shop-tls")
	if !ok {
		t.Fatalf("want the certificate: %+v", items)
	}
	if it.Labels[LabelRenewal] == RenewalManaged {
		t.Error("the secret is gone; calling that a healthy renewal takes 0.25 off the wrong item")
	}
	if it.Labels[LabelRenewal] != RenewalStuck {
		t.Errorf("renewal = %q, want %q — failing to produce the secret is the failure",
			it.Labels[LabelRenewal], RenewalStuck)
	}
}

func TestEachCertificateInACABundleGetsItsOwnName(t *testing.T) {
	rootPEM, _ := selfSignedPEM(t, "bundle-root", time.Now().Add(300*24*time.Hour))
	intPEM, _ := selfSignedPEM(t, "bundle-intermediate", time.Now().Add(20*24*time.Hour))

	srv := fakeK8s(map[string]string{
		"validatingwebhookconfigurations": webhookConfigList(t, "wc", append(rootPEM, intPEM...)),
	}, nil)
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("a chain bundle is two findings, got %d: %+v", len(items), items)
	}
	if items[0].Name == items[1].Name {
		t.Fatalf("two rows on different dates share the display name %q", items[0].Name)
	}
	joined := items[0].Name + " " + items[1].Name
	for _, want := range []string{"bundle-root", "bundle-intermediate"} {
		if !strings.Contains(joined, want) {
			t.Errorf("names should identify the member, got %q", joined)
		}
	}
}

// The ingress list only supplies ranking context for secrets. Fetching it with
// secrets skipped is a wasted call and, worse, a 403 warning the operator has
// no way to silence — which is the whole point of the skip flags.
func TestSkippingSecretsAlsoSkipsTheIngressList(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := (&K8sSource{Server: srv.URL, SkipSecrets: true, SkipCertManager: true,
		SkipWebhooks: true, SkipAPIServices: true, SkipMesh: true}).Collect(context.Background())
	if err != nil {
		t.Fatalf("everything is skipped, so there is nothing to fail: %v", err)
	}
	for _, p := range seen {
		if strings.Contains(p, "ingresses") {
			t.Errorf("fetched %s with secrets skipped", p)
		}
	}
}
