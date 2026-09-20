package source

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeP8 mints a real P-256 key in the .p8 shape Apple hands out, so the
// assertion under test is signed the way a live one would be.
func writeP8(t *testing.T) (string, *ecdsa.PublicKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "AuthKey.p8")
	if err := os.WriteFile(path,
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, &key.PublicKey
}

// The signature encoding is the part that silently breaks: JWS wants r and s
// as a fixed-width pair, not the ASN.1 sequence most Go examples produce, and
// Apple rejects the difference with an unhelpful 401.
func TestAppleSignsAVerifiableES256Assertion(t *testing.T) {
	keyFile, pub := writeP8(t)
	s := &AppleSource{IssuerID: "issuer", KeyID: "KEY123", PrivateKeyFile: keyFile}

	token, err := s.assertion()
	if err != nil {
		t.Fatalf("assertion: %v", err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %q", token)
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("signature is not base64url: %v", err)
	}
	if len(sig) != 64 {
		t.Fatalf("ES256 signature is 64 bytes (r||s), got %d — ASN.1 encoding would be variable",
			len(sig))
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r := new(big.Int).SetBytes(sig[:32])
	sv := new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(pub, sum[:], r, sv) {
		t.Fatal("the signature does not verify against the key that made it")
	}

	var header map[string]string
	raw, _ := base64.RawURLEncoding.DecodeString(parts[0])
	_ = json.Unmarshal(raw, &header)
	if header["alg"] != "ES256" || header["kid"] != "KEY123" {
		t.Errorf("header = %v", header)
	}

	var claims map[string]any
	raw, _ = base64.RawURLEncoding.DecodeString(parts[1])
	_ = json.Unmarshal(raw, &claims)
	if claims["aud"] != appleAudience || claims["iss"] != "issuer" {
		t.Errorf("claims = %v", claims)
	}
	// Apple refuses an assertion valid for more than twenty minutes.
	life := time.Duration(claims["exp"].(float64)-claims["iat"].(float64)) * time.Second
	if life > 20*time.Minute {
		t.Errorf("assertion life is %v; Apple caps it at 20 minutes", life)
	}
}

func TestAppleDistributionCertificatesOutrankDevelopmentOnes(t *testing.T) {
	keyFile, _ := writeP8(t)
	soon := time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/certificates") {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
				map[string]any{"id": "c1", "attributes": map[string]any{
					"displayName": "Acme Distribution", "certificateType": "DISTRIBUTION",
					"expirationDate": soon, "serialNumber": "AAA"}},
				map[string]any{"id": "c2", "attributes": map[string]any{
					"displayName": "Dev Machine", "certificateType": "DEVELOPMENT",
					"expirationDate": soon}},
			}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer srv.Close()

	s := &AppleSource{IssuerID: "i", KeyID: "k", PrivateKeyFile: keyFile, BaseURL: srv.URL}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	dist, ok := itemNamed(items, "certificate/Acme Distribution")
	if !ok {
		t.Fatalf("want both certificates: %+v", items)
	}
	dev, _ := itemNamed(items, "certificate/Dev Machine")
	if dist.Labels[LabelBlastRadius] != "0.75" {
		t.Errorf("a distribution certificate blocks every release, got %q",
			dist.Labels[LabelBlastRadius])
	}
	if dev.Labels[LabelBlastRadius] != "" {
		t.Error("a development certificate inconveniences one machine")
	}
}

func TestAppleInvalidProfilesAreNotSigningAnything(t *testing.T) {
	keyFile, _ := writeP8(t)
	soon := time.Now().Add(20 * 24 * time.Hour).Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/profiles") {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
				map[string]any{"id": "p1", "attributes": map[string]any{
					"name": "Acme AppStore", "profileType": "IOS_APP_STORE",
					"profileState": "ACTIVE", "expirationDate": soon}},
				map[string]any{"id": "p2", "attributes": map[string]any{
					"name": "Stale", "profileType": "IOS_APP_STORE",
					"profileState": "INVALID", "expirationDate": soon}},
			}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer srv.Close()

	s := &AppleSource{IssuerID: "i", KeyID: "k", PrivateKeyFile: keyFile, BaseURL: srv.URL}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	active, _ := itemNamed(items, "profile/Acme AppStore")
	stale, _ := itemNamed(items, "profile/Stale")
	if active.Labels[LabelInUse] == "false" {
		t.Error("an active profile is signing releases")
	}
	if stale.Labels[LabelInUse] != "false" {
		t.Error("Apple marks a lapsed profile INVALID; it is already not signing")
	}
}

func TestAppleRejectsAKeyThatIsNotAP8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notakey.p8")
	if err := os.WriteFile(path, []byte("this is not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &AppleSource{IssuerID: "i", KeyID: "k", PrivateKeyFile: path}
	_, err := s.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not a PEM") {
		t.Fatalf("want a clear complaint about the key file, got %v", err)
	}
}

// An annual clock makes silent truncation expensive: the rest of the profiles
// are found when the build breaks.
func TestAppleFollowsItsNextLink(t *testing.T) {
	keyFile, _ := writeP8(t)
	soon := time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	var pages int
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/certificates") {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
			return
		}
		pages++
		body := map[string]any{"data": []any{
			map[string]any{"id": "c" + r.URL.Query().Get("cursor"), "attributes": map[string]any{
				"displayName": "cert-" + r.URL.Query().Get("cursor"), "expirationDate": soon}},
		}}
		if r.URL.Query().Get("cursor") == "" {
			body["links"] = map[string]any{"next": srv.URL + "/v1/certificates?cursor=p2"}
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	s := &AppleSource{IssuerID: "i", KeyID: "k", PrivateKeyFile: keyFile,
		BaseURL: srv.URL, SkipProfiles: true}
	items, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if pages != 2 {
		t.Errorf("fetched %d pages, want 2", pages)
	}
	if _, ok := itemNamed(items, "certificate/cert-p2"); !ok {
		t.Fatalf("the second page's certificate is missing: %+v", items)
	}
}
