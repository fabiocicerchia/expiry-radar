package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The config file is the one input a user hand-writes, so it is the one most
// likely to be malformed. Load decodes it and then validates the rank
// overrides, and anything that gets past both is handed straight to Sources()
// -- so the property worth holding is that no byte sequence makes any of that
// panic. A parse error is a fine outcome; a crash on a typo is not.
func FuzzLoad(f *testing.F) {
	seeds := []string{
		`{}`,
		`{"endpoints":[{"host":"example.com:443"}]}`,
		`{"domains":["example.com"]}`,
		`{"k8s":{"enabled":true,"namespaces":["default"]}}`,
		`{"vault":{"enabled":true,"pkiMounts":["pki"],"maxCerts":10}}`,
		`{"aws":{"enabled":true,"region":"eu-west-1","maxKeyAgeDays":90}}`,
		`{"overrides":[{"match":"*.example.com"}]}`,
		`{"overrides":[{"match":"["}]}`, // a glob that does not compile
		`{"unknown":1}`,                 // DisallowUnknownFields must reject, not crash
	}
	if b, err := os.ReadFile("../../expiry-radar.example.json"); err == nil {
		seeds = append(seeds, string(b))
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		p := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Skip()
		}
		cfg, err := Load(p)
		if err != nil {
			return // rejecting bad input is the correct behaviour
		}
		if cfg == nil {
			t.Fatal("Load returned nil config and nil error")
		}
		// Whatever Load accepts, the rest of the program will call this on.
		_ = cfg.Sources()
	})
}
