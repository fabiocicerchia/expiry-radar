package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fabiocicerchia/expiry-radar/internal/rank"
	"github.com/fabiocicerchia/expiry-radar/internal/source"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "expiry-radar.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// Every rejection below is a config that would otherwise run and quietly report
// less than the operator asked for — the failure mode this tool exists to avoid.
func TestLoadRejectsConfigsThatWouldSilentlyScanLess(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"malformed json", `{"domains": [`, "unexpected EOF"},
		{"wrong type", `{"domains": "example.com"}`, "cannot unmarshal"},
		{"typo in a source key", `{"vualt": {"enabled": true}}`, "unknown field"},
		{"override with no pattern", `{"overrides": [{"blastRadius": 1}]}`, "no match pattern"},
		{"unparsable glob", `{"overrides": [{"match": "pay[ments", "blastRadius": 1}]}`, "syntax error"},
		{"blast radius out of range", `{"overrides": [{"match": "x", "blastRadius": 4}]}`, "outside 0..1"},
		// A manual item exists because nothing else can find the thing, so one
		// that fails to load leaves no trace anywhere.
		{"manual item with no date", `{"manual": [{"name": "a", "kind": "domain"}]}`, "no expires date"},
		{"manual item with an unknown kind", `{"manual": [{"name": "a", "kind": "cert", "expires": "2027-03-01"}]}`,
			"unknown kind"},
		// A mesh anchor with a misspelt kind falls through to the Secret branch
		// and 404s into silence, so it watches nothing and says nothing.
		{"mesh anchor with a bad kind",
			`{"k8s": {"enabled": true, "meshAnchors": [{"mesh": "linkerd", "kind": "configmap",` +
				` "namespace": "linkerd", "name": "roots", "keys": [{"key": "ca.crt", "role": "issuer"}]}]}}`,
			`want "secrets" or "configmaps"`},
		{"mesh anchor with no keys",
			`{"k8s": {"enabled": true, "meshAnchors": [{"mesh": "istio", "kind": "secrets",` +
				` "namespace": "istio-system", "name": "cacerts", "keys": []}]}}`,
			"at least one key"},
		// Collect hard-fails without these, so the failure belongs at load
		// (exit 2) rather than at collect time (exit 3).
		{"namecheap without apiUser",
			`{"namecheap": {"enabled": true, "userName": "u", "clientIp": "1.2.3.4"}}`,
			"apiUser and namecheap.userName are both required"},
		// Anchors the mesh collector will never be asked to read.
		{"mesh anchors without trust anchors",
			`{"k8s": {"enabled": true, "meshAnchors": [{"mesh": "m", "kind": "secrets",` +
				` "namespace": "n", "name": "o", "keys": [{"key": "k", "role": "issuer"}]}]}}`,
			"needs k8s.trustAnchors"},
		// Granting read access to CA private keys and getting no findings for
		// it is the worst of both.
		{"mesh signing secrets without trust anchors",
			`{"k8s": {"enabled": true, "meshSigningSecrets": true}}`,
			"needs k8s.trustAnchors"},
		{"mesh anchor key with an unknown role",
			`{"k8s": {"enabled": true, "meshAnchors": [{"mesh": "istio", "kind": "secrets",` +
				` "namespace": "istio-system", "name": "cacerts", "keys": [{"key": "ca.pem", "role": "root"}]}]}}`,
			`want "trust-anchor" or "issuer"`},
		{"manual item with an unreadable date", `{"manual": [{"name": "a", "kind": "domain", "expires": "next march"}]}`,
			"neither"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(write(t, tc.body))
			if err == nil {
				t.Fatalf("expected %s to be rejected, it loaded", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error should say %q, got %v", tc.want, err)
			}
		})
	}
}

func TestLoadReportsAMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); !os.IsNotExist(err) {
		t.Fatalf("want a not-exist error, got %v", err)
	}
}

// An empty config is valid and enables nothing: a source that needs credentials
// is only ever constructed when the file asks for it.
func TestNothingIsEnabledImplicitly(t *testing.T) {
	f, err := Load(write(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Sources(); len(got) != 0 {
		t.Fatalf("empty config enabled %d source(s)", len(got))
	}

	f, err = Load(write(t, `{"k8s": {"enabled": false}, "vault": {"enabled": false}, "aws": {"enabled": false}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Sources(); len(got) != 0 {
		t.Fatalf("explicitly disabled sources still built %d source(s)", len(got))
	}
}

func TestSourcesBuildsOneSourcePerEnabledBlock(t *testing.T) {
	t.Setenv("VAULT_ADDR", "https://vault.example:8200")
	t.Setenv("VAULT_TOKEN", "test-token")
	f, err := Load(write(t, `{
		"endpoints": [{"host": "example.com:443"}],
		"domains": ["example.com"],
		"manual": [{"name": "acme-corp.co.uk", "kind": "domain", "expires": "2027-03-01"}],
		"k8s": {"enabled": true},
		"vault": {"enabled": true},
		"aws": {"enabled": true, "maxKeyAgeDays": 90}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, s := range f.Sources() {
		names[s.Name()] = true
	}
	for _, want := range []string{"tls:endpoint", "domain:rdap", "manual", "k8s", "vault", "aws"} {
		if !names[want] {
			t.Errorf("source %q was configured but not built (got %v)", want, names)
		}
	}
}

func TestVaultAddrFallsBackToTheEnvironment(t *testing.T) {
	t.Setenv("VAULT_ADDR", "https://vault.example:8200")
	t.Setenv("VAULT_TOKEN", "test-token")
	f, err := Load(write(t, `{"vault": {"enabled": true}}`))
	if err != nil {
		t.Fatal(err)
	}
	src := f.Sources()
	if len(src) != 1 {
		t.Fatalf("want 1 source, got %d", len(src))
	}
	// The addr lives on the concrete source; a config with neither addr nor
	// $VAULT_ADDR would silently probe nothing.
	if got := src[0].Name(); got != "vault" {
		t.Fatalf("want the vault source, got %q", got)
	}
}

// An enabled Vault source with no address or no token can only fail on the
// first collect, by which time the operator has walked away. Load refuses it
// while they are still looking at the command.
func TestVaultWithoutCredentialsIsRefusedAtLoad(t *testing.T) {
	if _, err := Load(write(t, `{"vault": {"enabled": true}}`)); err == nil {
		t.Fatal("vault enabled with neither addr nor $VAULT_ADDR was accepted")
	}
	t.Setenv("VAULT_ADDR", "https://vault.example:8200")
	if _, err := Load(write(t, `{"vault": {"enabled": true}}`)); err == nil {
		t.Fatal("vault enabled with no $VAULT_TOKEN was accepted")
	}
}

// A manual item is recorded because nothing can discover it; it still has to be
// ranked by the same rules as the rest of the estate, or it would sit at the
// bottom of every report for having been typed in rather than found.
func TestManualItemsAreRankedLikeAnythingElse(t *testing.T) {
	f, err := Load(write(t, `{
		"manual": [
			{"name": "payments.example.com", "kind": "tls_cert", "expires": "2027-03-01",
			 "labels": {"public": "true", "traffic": "2400"}},
			{"name": "sandbox.example.com", "kind": "tls_cert", "expires": "2027-03-01",
			 "namespace": "staging"}
		],
		"overrides": [{"match": "sandbox*", "blastRadius": 0.05}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	items, errs := source.CollectAll(context.Background(), f.Sources())
	if len(errs) != 0 {
		t.Fatalf("recording an item must not need anything that can fail: %v", errs)
	}

	scored := rank.Rank(items, f.Overrides, time.Now())
	if len(scored) != 2 {
		t.Fatalf("want 2 scored items, got %d", len(scored))
	}
	// Ranked, so the busy public one leads; and the override applies to a
	// manual item exactly as it does to a discovered one.
	if scored[0].Item.Name != "payments.example.com" {
		t.Errorf("expected the payment path to outrank the sandbox, got %q first", scored[0].Item.Name)
	}
	if scored[0].BlastRadius <= scored[1].BlastRadius {
		t.Errorf("blast radius did not separate them: %v vs %v", scored[0].BlastRadius, scored[1].BlastRadius)
	}
	if scored[1].Why != "override sandbox*" {
		t.Errorf("the override should be the stated reason, got %q", scored[1].Why)
	}
}

// Naming one extra anchor must not silently stop Linkerd and Istio being
// watched — the same reason -endpoints and -domains add to the config rather
// than replacing it.
func TestConfiguredMeshAnchorsDoNotDisableTheBuiltInOnes(t *testing.T) {
	p := write(t, `{"k8s": {"enabled": true, "trustAnchors": true, "meshAnchors": [
		{"mesh": "custom", "kind": "secrets", "namespace": "mesh", "name": "our-ca",
		 "keys": [{"key": "ca.pem", "role": "trust-anchor"}]}]}}`)
	f, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	srcs := f.Sources()
	if len(srcs) != 1 {
		t.Fatalf("want the k8s source, got %d", len(srcs))
	}
	k8s, ok := srcs[0].(*source.K8sSource)
	if !ok {
		t.Fatalf("want a *source.K8sSource, got %T", srcs[0])
	}

	var meshes []string
	for _, a := range k8s.Anchors() {
		meshes = append(meshes, a.Mesh)
	}
	joined := strings.Join(meshes, ",")
	for _, want := range []string{"linkerd", "istio", "custom"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q is not watched; anchors are %s", want, joined)
		}
	}
}

// Every provider the config knows about must actually be constructed. A source
// that parses but is never built is the quietest possible failure.
func TestEveryConfiguredProviderIsConstructed(t *testing.T) {
	for _, env := range []string{
		"CLOUDFLARE_API_TOKEN", "GITLAB_TOKEN", "GITHUB_TOKEN", "DIGITALOCEAN_TOKEN",
		"SCW_SECRET_KEY", "NAMECHEAP_API_KEY", "ANTHROPIC_ADMIN_KEY", "OPENAI_ADMIN_KEY",
		"DOCKERHUB_TOKEN", "VAULT_TOKEN", "VAULT_ADDR",
		"AZURE_CLIENT_SECRET", "OKTA_API_TOKEN",
	} {
		t.Setenv(env, "test-value")
	}

	p := write(t, `{
		"endpoints": [{"host": "a.example.com"}],
		"domains": ["example.com"],
		"manual": [{"name": "m", "kind": "domain", "expires": "2030-01-01"}],
		"k8s": {"enabled": true, "server": "http://127.0.0.1:8001"},
		"vault": {"enabled": true, "pkiMounts": ["pki"]},
		"aws": {"enabled": true, "region": "eu-west-1"},
		"cloudflare": {"enabled": true, "accountId": "acct"},
		"gitlab": {"enabled": true, "projects": ["acme/x"]},
		"github": {"enabled": true, "orgs": ["acme"]},
		"gcp": {"enabled": true, "projects": ["acme-prod"]},
		"azure": {"enabled": true, "tenantId": "t", "clientId": "c"},
		"okta": {"enabled": true, "orgUrl": "https://acme.okta.com"},
		"federation": [{"name": "corp", "url": "https://idp.example/metadata"}],
		"digitalocean": {"enabled": true},
		"scaleway": {"enabled": true, "organizationId": "org"},
		"namecheap": {"enabled": true, "apiUser": "u", "userName": "u", "clientIp": "1.2.3.4"},
		"rotation": [
			{"name": "anthropic", "maxKeyAgeDays": 90},
			{"name": "openai", "maxKeyAgeDays": 90},
			{"name": "dockerhub", "maxKeyAgeDays": 180}
		]
	}`)
	f, err := Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	got := map[string]bool{}
	for _, s := range f.Sources() {
		got[s.Name()] = true
	}
	want := []string{
		"tls:endpoint", "domain:rdap", "manual", "k8s", "vault", "aws",
		"cloudflare", "gitlab", "github", "gcp", "digitalocean", "scaleway",
		"namecheap", "rotation", "azure", "okta", "federation",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("%s is configured but never constructed", w)
		}
	}
	if len(f.Sources()) != len(want) {
		t.Errorf("built %d sources, expected %d: %v", len(f.Sources()), len(want), got)
	}
}
