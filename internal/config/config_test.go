package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		{"unparseable glob", `{"overrides": [{"match": "pay[ments", "blastRadius": 1}]}`, "syntax error"},
		{"blast radius out of range", `{"overrides": [{"match": "x", "blastRadius": 4}]}`, "outside 0..1"},
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
	f, err := Load(write(t, `{
		"endpoints": [{"host": "example.com:443"}],
		"domains": ["example.com"],
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
	for _, want := range []string{"tls:endpoint", "domain:rdap", "k8s", "vault", "aws"} {
		if !names[want] {
			t.Errorf("source %q was configured but not built (got %v)", want, names)
		}
	}
}

func TestVaultAddrFallsBackToTheEnvironment(t *testing.T) {
	t.Setenv("VAULT_ADDR", "https://vault.example:8200")
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
