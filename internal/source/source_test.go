package source

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTLSSourceReadsTheChainFromALiveHandshake(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	s := &TLSSource{Endpoints: []Endpoint{{Host: strings.TrimPrefix(srv.URL, "https://")}}}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want the leaf certificate, got %d items", len(items))
	}
	if items[0].Kind != KindTLSCert {
		t.Errorf("kind = %s, want %s", items[0].Kind, KindTLSCert)
	}
	if items[0].Expires.IsZero() || items[0].Expires.Before(time.Now()) {
		t.Errorf("expiry not read from the certificate: %v", items[0].Expires)
	}
	if items[0].Labels[LabelPublic] != "true" {
		t.Error("a host we successfully dialled should be marked reachable")
	}
}

// The tool exists to find expired certificates, so an expired certificate must
// not break the probe — this is why the dialler skips verification.
func TestTLSSourceStillReportsAnExpiredCertificate(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	cert, key := selfSigned(t, "expired.test", time.Now().Add(-48*time.Hour))
	srv.TLS = tlsConfigFor(t, cert, key)
	srv.StartTLS()
	defer srv.Close()

	s := &TLSSource{Endpoints: []Endpoint{{Host: strings.TrimPrefix(srv.URL, "https://"), ServerName: "expired.test"}}}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 || !items[0].Expires.Before(time.Now()) {
		t.Fatalf("an expired certificate must still be reported, got %+v", items)
	}
}

func TestTLSSourceReportsUnreachableHostsWithoutLosingTheRest(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	s := &TLSSource{
		Endpoints: []Endpoint{
			{Host: strings.TrimPrefix(srv.URL, "https://")},
			{Host: "127.0.0.1:1"}, // nothing listens here
		},
		Timeout: 2 * time.Second,
	}
	items, err := s.Collect(context.Background())
	if err == nil {
		t.Fatal("a failed endpoint must be reported")
	}
	if len(items) != 1 {
		t.Fatalf("the reachable endpoint must still be returned, got %d items", len(items))
	}
}

func TestMergeIntermediatesDeduplicatesAndCollectsHosts(t *testing.T) {
	items := []Item{
		{Kind: KindTLSCert, Name: "a.example", Labels: map[string]string{}},
		{Kind: KindIntermediate, Name: "Issuing CA", Labels: map[string]string{LabelSerial: "1", LabelHosts: "a.example"}},
		{Kind: KindIntermediate, Name: "Issuing CA", Labels: map[string]string{LabelSerial: "1", LabelHosts: "b.example"}},
		{Kind: KindIntermediate, Name: "Other CA", Labels: map[string]string{LabelSerial: "2", LabelHosts: "c.example"}},
	}
	got := mergeIntermediates(items)
	if len(got) != 3 {
		t.Fatalf("want leaf + 2 distinct intermediates, got %d", len(got))
	}
	if hosts := got[1].Labels[LabelHosts]; hosts != "a.example,b.example" {
		t.Errorf("hosts = %q, want both hosts merged onto one intermediate", hosts)
	}
}

func TestDomainSourceReadsTheRDAPExpirationEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/domain/example.com" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events": []map[string]string{
				{"eventAction": "registration", "eventDate": "2001-01-01T00:00:00Z"},
				{"eventAction": "expiration", "eventDate": "2027-03-04T05:06:07Z"},
			},
		})
	}))
	defer srv.Close()

	s := &DomainSource{Domains: []string{"example.com"}, Bootstrap: srv.URL}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 || items[0].Kind != KindDomain {
		t.Fatalf("want one domain item, got %+v", items)
	}
	if want := time.Date(2027, 3, 4, 5, 6, 7, 0, time.UTC); !items[0].Expires.Equal(want) {
		t.Errorf("expires = %v, want %v", items[0].Expires, want)
	}
}

func TestDomainSourceSaysSoWhenTheRegistryHidesTheDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"events": []map[string]string{{"eventAction": "registration", "eventDate": "2001-01-01T00:00:00Z"}}})
	}))
	defer srv.Close()

	_, err := (&DomainSource{Domains: []string{"example.com"}, Bootstrap: srv.URL}).Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no expiration event") {
		t.Fatalf("want a clear 'no expiration event' error, got %v", err)
	}
}

func TestDomainSourceFallsBackToWhoisWhenTheTLDHasNoRDAP(t *testing.T) {
	rdap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound) // .it is one of ~240 TLDs missing from IANA's bootstrap
	}))
	defer rdap.Close()

	// One listener plays both roles: IANA referral, then the registry itself.
	whois := whoisStub(t, map[string]string{
		"it":                "\nwhois:        127.0.0.1:PORT\n\n",
		"fabiocicerchia.it": "Domain:             fabiocicerchia.it\nExpire Date:        2026-08-18\n",
	})

	s := &DomainSource{Domains: []string{"fabiocicerchia.it"}, Bootstrap: rdap.URL, IANAWhois: whois, Timeout: 5 * time.Second}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 || items[0].Source != "domain:whois" {
		t.Fatalf("want one domain:whois item, got %+v", items)
	}
	if want := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC); !items[0].Expires.Equal(want) {
		t.Errorf("expires = %v, want %v", items[0].Expires, want)
	}
}

func TestWhoisExpirationReadsTheFormatsRegistriesActuallyUse(t *testing.T) {
	for text, want := range map[string]time.Time{
		"Expire Date:        2026-08-18":                       time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC),
		"    Expiry date:  13-Dec-2034":                        time.Date(2034, 12, 13, 0, 0, 0, 0, time.UTC),
		"paid-till:     2026-09-30T21:00:00Z":                  time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC),
		"Registry Expiry Date: 2027-03-04T05:06:07Z":           time.Date(2027, 3, 4, 5, 6, 7, 0, time.UTC),
		"Domain:  x.de\nStatus: connect\nChanged: today":       {}, // DENIC publishes no expiry at all
		"Expiry date: not available\nrenewal date: 2028-01-02": time.Date(2028, 1, 2, 0, 0, 0, 0, time.UTC),
	} {
		got, err := whoisExpiration(text)
		if want.IsZero() {
			if err == nil {
				t.Errorf("%q: want an error, got %v", text, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", text, err)
		} else if !got.Equal(want) {
			t.Errorf("%q: got %v, want %v", text, got, want)
		}
	}
}

// whoisStub serves canned port-43 responses keyed by query, and returns its
// address. "PORT" in a reply is replaced with the stub's own port so it can
// refer callers back to itself.
func whoisStub(t *testing.T, replies map[string]string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				q, err := bufio.NewReader(conn).ReadString('\n')
				if err != nil {
					return
				}
				reply := replies[strings.TrimSpace(q)]
				_, _ = io.WriteString(conn, strings.ReplaceAll(reply, "PORT", strings.TrimPrefix(ln.Addr().String(), "127.0.0.1:")))
			}()
		}
	}()
	return ln.Addr().String()
}

func TestK8sSourceTakesExpiryFromTheSecretAndContextFromTheIngress(t *testing.T) {
	certPEM, _ := selfSignedPEM(t, "shop.example.com", time.Now().Add(30*24*time.Hour))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "ingresses"):
			_, _ = w.Write([]byte(`{"items":[{
				"metadata":{"name":"shop","namespace":"prod"},
				"spec":{"ingressClassName":"nginx-public","tls":[{"hosts":["shop.example.com"],"secretName":"shop-tls"}]}
			}]}`))
		case strings.Contains(r.URL.Path, "secrets"):
			body, _ := json.Marshal(map[string]any{"items": []map[string]any{{
				"metadata": map[string]string{"name": "shop-tls", "namespace": "prod"},
				"type":     "kubernetes.io/tls",
				"data":     map[string][]byte{"tls.crt": certPEM},
			}}})
			_, _ = w.Write(body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	items, err := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want one item, got %d", len(items))
	}
	got := items[0]
	if got.Name != "prod/shop-tls" || got.Namespace != "prod" {
		t.Errorf("name/namespace = %s / %s", got.Name, got.Namespace)
	}
	if got.Labels[LabelIngressClass] != "nginx-public" || got.Labels[LabelPublic] != "true" {
		t.Errorf("ingress context missing: %v", got.Labels)
	}
	if d := time.Until(got.Expires).Hours() / 24; d < 29 || d > 31 {
		t.Errorf("expiry came from somewhere other than the certificate: %v days", d)
	}
}

func TestK8sSourceExplainsAForbiddenResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := (&K8sSource{Server: srv.URL}).Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rbac-readonly") {
		t.Fatalf("a 403 should point at the RBAC doc, got %v", err)
	}
}

func TestK8sSourceScopesPathsToNamespaces(t *testing.T) {
	s := &K8sSource{Namespaces: []string{"prod", "payments"}}
	got := s.paths("/api/v1", "secrets")
	want := []string{"/api/v1/namespaces/prod/secrets", "/api/v1/namespaces/payments/secrets"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path %d = %s, want %s", i, got[i], want[i])
		}
	}
	if all := s.paths("/api/v1", "secrets"); len(all) != 2 {
		t.Errorf("want one path per namespace, got %d", len(all))
	}
}

// --- helpers ---

func selfSigned(t *testing.T, cn string, notAfter time.Time) (*x509.Certificate, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    notAfter.Add(-365 * 24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, priv
}

func tlsConfigFor(t *testing.T, cert *x509.Certificate, key ed25519.PrivateKey) *tls.Config {
	t.Helper()
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{cert.Raw}, PrivateKey: key}}}
}

func selfSignedPEM(t *testing.T, cn string, notAfter time.Time) ([]byte, ed25519.PrivateKey) {
	t.Helper()
	cert, key := selfSigned(t, cn, notAfter)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), key
}
