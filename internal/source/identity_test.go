package source

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// derCert mints a certificate and returns it base64-DER, which is how both
// SAML metadata and an Okta JWK x5c carry one — not PEM.
func derCert(t *testing.T, cn string, notAfter time.Time) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		Issuer:       pkix.Name{CommonName: cn},
		NotBefore:    notAfter.Add(-365 * 24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

// ---------- Azure ----------

func azureServer(t *testing.T, routes map[string]any) (*httptest.Server, *[]string) {
	t.Helper()
	var scopes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			_ = r.ParseForm()
			scopes = append(scopes, r.FormValue("scope"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok-" + r.FormValue("scope"), "expires_in": 3600,
			})
			return
		}
		for k, v := range routes {
			if strings.Contains(r.URL.Path, k) {
				_ = json.NewEncoder(w).Encode(v)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"value": []any{}})
	}))
	return srv, &scopes
}

func azureTestSource(srvURL string) *AzureSource {
	return &AzureSource{
		TenantID: "tenant", ClientID: "client", ClientSecret: "secret",
		Endpoints: map[string]string{"login": srvURL, "graph": srvURL, "vault": srvURL},
	}
}

// The thing nobody watches: an app registration's client secret, which expires
// on its own schedule and surfaces only when an integration stops working.
func TestAzureReportsAppRegistrationSecretsAndCertificates(t *testing.T) {
	soon := time.Now().Add(20 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := azureServer(t, map[string]any{
		"/applications": map[string]any{"value": []any{
			map[string]any{
				"id": "1", "appId": "app-guid", "displayName": "payments-api",
				"passwordCredentials": []any{
					map[string]any{"keyId": "k1", "displayName": "ci-secret",
						"endDateTime": soon, "hint": "abc"},
				},
				"keyCredentials": []any{
					map[string]any{"keyId": "k2", "displayName": "signing-cert",
						"endDateTime": soon, "usage": "Sign"},
				},
			},
		}},
	})
	defer srv.Close()

	s := azureTestSource(srv.URL)
	s.SkipPrincipals, s.SkipVaults = true, true
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	secret, ok := itemNamed(items, "payments-api/ci-secret")
	if !ok {
		t.Fatalf("want the client secret: %+v", items)
	}
	if secret.Kind != KindSecret || secret.Labels["credential"] != "client secret" {
		t.Errorf("secret reported as %q / %v", secret.Kind, secret.Labels)
	}
	cert, ok := itemNamed(items, "payments-api/signing-cert")
	if !ok {
		t.Fatalf("want the certificate credential: %+v", items)
	}
	if cert.Kind != KindTLSCert {
		t.Errorf("certificate kind = %q", cert.Kind)
	}
}

// A service principal's Verify key is the SAML signing certificate: when it
// lapses every sign-in through the app stops at once.
func TestAzureSAMLSigningCertificatesAreTrustAnchors(t *testing.T) {
	soon := time.Now().Add(15 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := azureServer(t, map[string]any{
		"/servicePrincipals": map[string]any{"value": []any{
			map[string]any{"id": "2", "appId": "sp-guid", "displayName": "sso-app",
				"keyCredentials": []any{
					map[string]any{"keyId": "k3", "displayName": "saml-signing",
						"endDateTime": soon, "usage": "Verify"},
				}},
		}},
	})
	defer srv.Close()

	s := azureTestSource(srv.URL)
	s.SkipApplications, s.SkipVaults = true, true
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	it, ok := itemNamed(items, "sso-app/saml-signing")
	if !ok {
		t.Fatalf("want the signing certificate: %+v", items)
	}
	if it.Kind != KindTrustAnchor {
		t.Errorf("kind = %q, want %q — every sign-in validates against this", it.Kind, KindTrustAnchor)
	}
}

// Graph and Key Vault are separate resources; one token cannot address both.
func TestAzureAcquiresATokenPerAudience(t *testing.T) {
	exp := time.Now().Add(40 * 24 * time.Hour).Unix()
	srv, scopes := azureServer(t, map[string]any{
		"/applications": map[string]any{"value": []any{}},
		"/certificates": map[string]any{"value": []any{
			map[string]any{"id": "https://v.vault.azure.net/certificates/tls",
				"attributes": map[string]any{"exp": exp, "enabled": true}},
		}},
	})
	defer srv.Close()

	s := azureTestSource(srv.URL)
	s.Vaults = []string{"acme-prod"}
	s.SkipPrincipals = true
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	var graph, vault bool
	for _, sc := range *scopes {
		if strings.Contains(sc, "graph.microsoft.com") {
			graph = true
		}
		if strings.Contains(sc, "vault.azure.net") {
			vault = true
		}
	}
	if !graph || !vault {
		t.Errorf("scopes requested = %v; both audiences are needed", *scopes)
	}
	if _, ok := itemNamed(items, "acme-prod/tls"); !ok {
		t.Fatalf("want the Key Vault certificate: %+v", items)
	}
}

func TestAzureVaultObjectsWithNoExpiryAreNotDeadlines(t *testing.T) {
	srv, _ := azureServer(t, map[string]any{
		"/secrets": map[string]any{"value": []any{
			map[string]any{"id": "https://v.vault.azure.net/secrets/forever",
				"attributes": map[string]any{"enabled": true}},
		}},
	})
	defer srv.Close()

	s := azureTestSource(srv.URL)
	s.Vaults = []string{"acme-prod"}
	s.SkipApplications, s.SkipPrincipals = true, true
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("nothing here expires: %+v", items)
	}
}

// ---------- Okta ----------

func TestOktaAppSigningCertificatesAreTrustAnchors(t *testing.T) {
	notAfter := time.Now().Add(25 * 24 * time.Hour)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/credentials/keys"):
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"kid": "kid1", "x5c": []string{derCert(t, "okta-signing", notAfter)}},
			})
		case strings.HasSuffix(r.URL.Path, "/apps"):
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": "a1", "label": "Workday", "status": "ACTIVE", "signOnMode": "SAML_2_0"},
				// Not SAML: no signing certificate, and not worth a request.
				map[string]any{"id": "a2", "label": "Bookmark", "status": "ACTIVE", "signOnMode": "BOOKMARK"},
			})
		default:
			_ = json.NewEncoder(w).Encode([]any{})
		}
	}))
	defer srv.Close()

	s := &OktaSource{OrgURL: srv.URL, Token: "t", SkipTokens: true}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("only the SAML app has a signing certificate: %+v", items)
	}
	if items[0].Kind != KindTrustAnchor {
		t.Errorf("kind = %q, want %q", items[0].Kind, KindTrustAnchor)
	}
	if items[0].Name != "saml/Workday" {
		t.Errorf("name = %q", items[0].Name)
	}
	// The date comes out of the certificate itself, not a field beside it.
	if d := time.Until(items[0].Expires).Hours() / 24; d < 24 || d > 26 {
		t.Errorf("expiry read as %v, %.0f days out", items[0].Expires, d)
	}
}

func TestOktaAPITokensCarryTheirOwnBlastRadius(t *testing.T) {
	soon := time.Now().Add(10 * 24 * time.Hour).Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api-tokens") {
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": "t1", "name": "automation", "expiresAt": soon},
			})
			return
		}
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	defer srv.Close()

	items, err := (&OktaSource{OrgURL: srv.URL, Token: "t", SkipApps: true}).Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 || items[0].Labels[LabelBlastRadius] != "0.85" {
		t.Fatalf("an admin token reads the directory everything federates against: %+v", items)
	}
}

func TestOktaExplainsAnInsufficientToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := (&OktaSource{OrgURL: srv.URL, Token: "t"}).Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "read-only admin role") {
		t.Fatalf("want the requirement named, got %v", err)
	}
}

// ---------- Federation metadata ----------

func metadataServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/samlmetadata+xml")
		_, _ = w.Write([]byte(body))
	}))
}

// One document, two different deadlines, and nobody owns either.
func TestFederationReportsBothTheCertificateAndTheMetadataExpiry(t *testing.T) {
	certExp := time.Now().Add(40 * 24 * time.Hour)
	validUntil := time.Now().Add(14 * 24 * time.Hour).Format(time.RFC3339)
	srv := metadataServer(`<?xml version="1.0"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example/sso"
	validUntil="` + validUntil + `">
	<IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
		<KeyDescriptor use="signing">
			<KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#"><X509Data>
				<X509Certificate>` + derCert(t, "idp-signing", certExp) + `</X509Certificate>
			</X509Data></KeyInfo>
		</KeyDescriptor>
	</IDPSSODescriptor>
</EntityDescriptor>`)
	defer srv.Close()

	s := &FederationSource{Providers: []FederationProvider{{Name: "corp-idp", URL: srv.URL}}}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("the certificate and the metadata are separate deadlines: %+v", items)
	}
	meta, ok := itemNamed(items, "corp-idp metadata")
	if !ok {
		t.Fatalf("want the metadata expiry: %+v", items)
	}
	if meta.Kind != KindTrustAnchor {
		t.Errorf("metadata kind = %q", meta.Kind)
	}
	cert, ok := itemNamed(items, "corp-idp signing certificate")
	if !ok {
		t.Fatalf("want the signing certificate: %+v", items)
	}
	if d := time.Until(cert.Expires).Hours() / 24; d < 39 || d > 41 {
		t.Errorf("certificate expiry read as %v", cert.Expires)
	}
	if meta.Expires.After(cert.Expires) {
		t.Error("the metadata expires first here and should sort that way")
	}
}

// A federation publishes EntitiesDescriptor wrapping many providers.
func TestFederationReadsAMultiEntityDocument(t *testing.T) {
	exp := time.Now().Add(60 * 24 * time.Hour)
	srv := metadataServer(`<?xml version="1.0"?>
<EntitiesDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata">
	<EntityDescriptor entityID="https://a.example">
		<IDPSSODescriptor><KeyDescriptor use="signing"><KeyInfo><X509Data>
			<X509Certificate>` + derCert(t, "a-signing", exp) + `</X509Certificate>
		</X509Data></KeyInfo></KeyDescriptor></IDPSSODescriptor>
	</EntityDescriptor>
	<EntityDescriptor entityID="https://b.example">
		<IDPSSODescriptor><KeyDescriptor use="signing"><KeyInfo><X509Data>
			<X509Certificate>` + derCert(t, "b-signing", exp) + `</X509Certificate>
		</X509Data></KeyInfo></KeyDescriptor></IDPSSODescriptor>
	</EntityDescriptor>
</EntitiesDescriptor>`)
	defer srv.Close()

	s := &FederationSource{Providers: []FederationProvider{{Name: "fed", URL: srv.URL}}}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("both entities' certificates are findings: %+v", items)
	}
}

// A URL that serves something else must not read as a provider with nothing
// expiring — that is the clean-estate failure again.
func TestFederationSaysWhenAURLIsNotMetadata(t *testing.T) {
	srv := metadataServer(`<html><body>login</body></html>`)
	defer srv.Close()

	s := &FederationSource{Providers: []FederationProvider{{Name: "wrong", URL: srv.URL}}}
	_, err := s.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "metadata URL") {
		t.Fatalf("want an explicit complaint, got %v", err)
	}
}

func TestFederationDedupesACertificateListedTwice(t *testing.T) {
	exp := time.Now().Add(30 * 24 * time.Hour)
	same := derCert(t, "dual-use", exp)
	srv := metadataServer(`<?xml version="1.0"?>
<EntityDescriptor entityID="https://idp.example">
	<IDPSSODescriptor>
		<KeyDescriptor use="signing"><KeyInfo><X509Data>
			<X509Certificate>` + same + `</X509Certificate></X509Data></KeyInfo></KeyDescriptor>
		<KeyDescriptor use="encryption"><KeyInfo><X509Data>
			<X509Certificate>` + same + `</X509Certificate></X509Data></KeyInfo></KeyDescriptor>
	</IDPSSODescriptor>
</EntityDescriptor>`)
	defer srv.Close()

	s := &FederationSource{Providers: []FederationProvider{{Name: "idp", URL: srv.URL}}}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("one certificate listed under two uses is one finding: %+v", items)
	}
}

func TestFederationRejectsAURLItCouldNeverFetch(t *testing.T) {
	if err := ValidateFederation([]FederationProvider{{Name: "x"}}); err == nil {
		t.Error("a provider with no url must be rejected at load")
	}
	if err := ValidateFederation([]FederationProvider{{URL: "idp.example/metadata"}}); err == nil {
		t.Error("a url with no scheme must be rejected at load")
	}
}

// Real federation metadata puts validUntil on the EntitiesDescriptor root.
// Reading it only from the entities loses one of the two deadlines this source
// exists for — and loses it silently, because the certificates still produce
// rows.
func TestFederationReadsValidUntilFromAnAggregateRoot(t *testing.T) {
	exp := time.Now().Add(90 * 24 * time.Hour)
	validUntil := time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339)
	srv := metadataServer(`<?xml version="1.0"?>
<EntitiesDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" validUntil="` + validUntil + `">
	<EntityDescriptor entityID="https://a.example">
		<IDPSSODescriptor><KeyDescriptor use="signing"><KeyInfo><X509Data>
			<X509Certificate>` + derCert(t, "a-signing", exp) + `</X509Certificate>
		</X509Data></KeyInfo></KeyDescriptor></IDPSSODescriptor>
	</EntityDescriptor>
</EntitiesDescriptor>`)
	defer srv.Close()

	s := &FederationSource{Providers: []FederationProvider{{Name: "fed", URL: srv.URL}}}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	var found bool
	for _, it := range items {
		if it.Source == "federation:metadata" {
			found = true
			if d := time.Until(it.Expires).Hours() / 24; d < 6 || d > 8 {
				t.Errorf("the root validUntil was not used: %v", it.Expires)
			}
		}
	}
	if !found {
		t.Fatalf("the aggregate's own validUntil is a deadline too: %+v", items)
	}
}

// Two entities in one document must not produce indistinguishable rows — on
// screen, or in an iCal UID.
func TestFederationEntitiesAreTellableApart(t *testing.T) {
	exp := time.Now().Add(45 * 24 * time.Hour)
	srv := metadataServer(`<?xml version="1.0"?>
<EntitiesDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata">
	<EntityDescriptor entityID="https://a.example">
		<IDPSSODescriptor><KeyDescriptor use="signing"><KeyInfo><X509Data>
			<X509Certificate>` + derCert(t, "a-signing", exp) + `</X509Certificate>
		</X509Data></KeyInfo></KeyDescriptor></IDPSSODescriptor>
	</EntityDescriptor>
	<EntityDescriptor entityID="https://b.example">
		<IDPSSODescriptor><KeyDescriptor use="signing"><KeyInfo><X509Data>
			<X509Certificate>` + derCert(t, "b-signing", exp) + `</X509Certificate>
		</X509Data></KeyInfo></KeyDescriptor></IDPSSODescriptor>
	</EntityDescriptor>
</EntitiesDescriptor>`)
	defer srv.Close()

	s := &FederationSource{Providers: []FederationProvider{{Name: "fed", URL: srv.URL}}}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want both: %+v", items)
	}
	if items[0].Name == items[1].Name {
		t.Fatalf("two entities share the display name %q", items[0].Name)
	}
	joined := items[0].Name + " " + items[1].Name
	if !strings.Contains(joined, "a.example") || !strings.Contains(joined, "b.example") {
		t.Errorf("the entity ids should distinguish them, got %q", joined)
	}
}

// Okta paginates with a Link header. An org with more apps than one page would
// otherwise report a subset of its SAML signing certificates as though that
// were all of them.
func TestOktaFollowsTheLinkHeader(t *testing.T) {
	notAfter := time.Now().Add(25 * 24 * time.Hour)
	var appPages int
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/credentials/keys"):
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"kid": "k", "x5c": []string{derCert(t, "sign", notAfter)}},
			})
		case strings.HasSuffix(r.URL.Path, "/apps"):
			appPages++
			if r.URL.Query().Get("after") == "" {
				// Okta sends rel="self" too, so the relation has to be read
				// rather than taking whichever header arrives last.
				w.Header().Add("Link", `<`+srv.URL+`/api/v1/apps>; rel="self"`)
				w.Header().Add("Link", `<`+srv.URL+`/api/v1/apps?after=p2>; rel="next"`)
				_ = json.NewEncoder(w).Encode([]any{
					map[string]any{"id": "a1", "label": "One", "status": "ACTIVE", "signOnMode": "SAML_2_0"},
				})
				return
			}
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": "a2", "label": "Two", "status": "ACTIVE", "signOnMode": "SAML_2_0"},
			})
		default:
			_ = json.NewEncoder(w).Encode([]any{})
		}
	}))
	defer srv.Close()

	s := &OktaSource{OrgURL: srv.URL, Token: "t", SkipTokens: true}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if appPages != 2 {
		t.Errorf("fetched %d app pages, want 2 — the Link cursor was not followed", appPages)
	}
	if len(items) != 2 {
		t.Fatalf("want a certificate from each page: %+v", items)
	}
}

// Graph and Key Vault paginate with absolute URLs the response supplies.
// Re-attaching a bearer token to whatever host one names is a habit worth not
// having, even when the response is authentic.
func TestAzureWillNotFollowAContinuationURLToAnotherHost(t *testing.T) {
	soon := time.Now().Add(20 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := azureServer(t, map[string]any{
		"/applications": map[string]any{
			"value": []any{map[string]any{"id": "1", "displayName": "app",
				"passwordCredentials": []any{
					map[string]any{"keyId": "k", "displayName": "s", "endDateTime": soon},
				}}},
			"@odata.nextLink": "https://attacker.example/v1.0/applications",
		},
	})
	defer srv.Close()

	s := azureTestSource(srv.URL)
	s.SkipPrincipals, s.SkipVaults = true, true
	items, err := s.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "another host") {
		t.Fatalf("want the off-host continuation refused and reported, got %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("what was read before the refusal should still be reported: %+v", items)
	}
}

// Graph's `hint` is the opening characters of the client secret. Microsoft
// treats it as non-sensitive, but a tool that promises never to handle secrets
// should not put a prefix of one into an HTML report.
func TestAzureDoesNotPutASecretPrefixInTheReport(t *testing.T) {
	soon := time.Now().Add(20 * 24 * time.Hour).Format(time.RFC3339)
	srv, _ := azureServer(t, map[string]any{
		"/applications": map[string]any{"value": []any{
			map[string]any{"id": "1", "displayName": "app", "passwordCredentials": []any{
				map[string]any{"keyId": "k", "displayName": "s", "endDateTime": soon, "hint": "Xy9"},
			}},
		}},
	})
	defer srv.Close()

	s := azureTestSource(srv.URL)
	s.SkipPrincipals, s.SkipVaults = true, true
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, it := range items {
		for k, v := range it.Labels {
			if v == "Xy9" {
				t.Fatalf("label %q carries the secret hint into the report", k)
			}
		}
	}
}
