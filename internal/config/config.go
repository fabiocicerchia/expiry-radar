// Package config turns one JSON file into the set of read-only sources to run.
//
// JSON, not YAML, so the binary keeps a zero-dependency config path — nothing
// here is worth a parser dependency. Credentials are never read from the file:
// they come from the environment (VAULT_TOKEN, the AWS credential chain, the
// mounted service account token), so the config can live in git.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/fabiocicerchia/expiry-radar/internal/rank"
	"github.com/fabiocicerchia/expiry-radar/internal/source"
)

type File struct {
	Endpoints []source.Endpoint `json:"endpoints"`
	Domains   []string          `json:"domains"`
	// Things that expire that no source can discover. See source.ManualItem.
	Manual    []source.ManualItem `json:"manual"`
	K8s       *K8s                `json:"k8s"`
	Vault     *Vault              `json:"vault"`
	AWS       *AWS                `json:"aws"`
	Overrides []rank.Override     `json:"overrides"`
}

type K8s struct {
	Enabled    bool     `json:"enabled"`
	Server     string   `json:"server"` // empty = in-cluster; use http://127.0.0.1:8001 with `kubectl proxy`
	CAFile     string   `json:"caFile"`
	Namespaces []string `json:"namespaces"`
	Insecure   bool     `json:"insecure"`
}

type Vault struct {
	Enabled   bool     `json:"enabled"`
	Addr      string   `json:"addr"` // empty = $VAULT_ADDR
	Namespace string   `json:"namespace"`
	PKIMounts []string `json:"pkiMounts"`
	MaxCerts  int      `json:"maxCerts"`
}

type AWS struct {
	Enabled       bool   `json:"enabled"`
	Region        string `json:"region"`
	Profile       string `json:"profile"`
	MaxKeyAgeDays int    `json:"maxKeyAgeDays"` // access keys have no expiry; this is the rotation policy
	SkipACM       bool   `json:"skipACM"`
	SkipIAM       bool   `json:"skipIAM"`
	SkipSecrets   bool   `json:"skipSecrets"`
}

func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields() // a typo in a config file must not silently disable a source
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// Rejected at load, not at rank time: a malformed glob never matches, so an
	// unvalidated override fails by quietly not applying — exactly the case the
	// operator wrote it to prevent.
	if err := rank.ValidateOverrides(f.Overrides); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// Rejected at load for the same reason: a manual item exists because
	// nothing else can find the thing. One that silently fails to parse leaves
	// no trace anywhere, which is the one outcome it was written to prevent.
	if err := source.ValidateManual(f.Manual); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &f, nil
}

// Sources builds the enabled sources. Nothing is enabled implicitly: a source
// that needs credentials is only constructed when the config asks for it.
func (f *File) Sources() []source.Source {
	var out []source.Source
	if len(f.Endpoints) > 0 {
		out = append(out, &source.TLSSource{Endpoints: f.Endpoints})
	}
	if len(f.Domains) > 0 {
		out = append(out, &source.DomainSource{Domains: f.Domains})
	}
	if len(f.Manual) > 0 {
		out = append(out, &source.ManualSource{Items: f.Manual})
	}
	if f.K8s != nil && f.K8s.Enabled {
		out = append(out, &source.K8sSource{
			Server:     f.K8s.Server,
			CAFile:     f.K8s.CAFile,
			Namespaces: f.K8s.Namespaces,
			Insecure:   f.K8s.Insecure,
		})
	}
	if f.Vault != nil && f.Vault.Enabled {
		addr := f.Vault.Addr
		if addr == "" {
			addr = os.Getenv("VAULT_ADDR")
		}
		out = append(out, &source.VaultSource{
			Addr:      addr,
			Token:     os.Getenv("VAULT_TOKEN"),
			Namespace: f.Vault.Namespace,
			PKIMounts: f.Vault.PKIMounts,
			MaxCerts:  f.Vault.MaxCerts,
		})
	}
	if f.AWS != nil && f.AWS.Enabled {
		out = append(out, &source.AWSSource{
			Region:     f.AWS.Region,
			Profile:    f.AWS.Profile,
			MaxKeyAge:  time.Duration(f.AWS.MaxKeyAgeDays) * 24 * time.Hour,
			SkipACM:    f.AWS.SkipACM,
			SkipIAM:    f.AWS.SkipIAM,
			SkipSecret: f.AWS.SkipSecrets,
		})
	}
	return out
}
