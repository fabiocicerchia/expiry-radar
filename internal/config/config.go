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

// File is a config file as it is written on disk: what to watch, where to
// look for it, and how to weight what turns up.
type File struct {
	Endpoints []source.Endpoint `json:"endpoints"`
	Domains   []string          `json:"domains"`
	// Things that expire that no source can discover. See source.ManualItem.
	Manual       []source.ManualItem `json:"manual"`
	K8s          *K8s                `json:"k8s"`
	Vault        *Vault              `json:"vault"`
	AWS          *AWS                `json:"aws"`
	Cloudflare   *Cloudflare         `json:"cloudflare"`
	GitLab       *GitLab             `json:"gitlab"`
	DigitalOcean *DigitalOcean       `json:"digitalocean"`
	Scaleway     *Scaleway           `json:"scaleway"`
	// Rotation covers credentials with no expiry at all; see source.RotationSource.
	Rotation  []source.RotationProvider `json:"rotation"`
	Overrides []rank.Override           `json:"overrides"`
}

// K8s points the Kubernetes source at a cluster. An empty Server means
// in-cluster credentials.
type K8s struct {
	Enabled    bool     `json:"enabled"`
	Server     string   `json:"server"` // empty = in-cluster; use http://127.0.0.1:8001 with `kubectl proxy`
	CAFile     string   `json:"caFile"`
	Namespaces []string `json:"namespaces"`
	Insecure   bool     `json:"insecure"`
	// SkipSecrets turns off the TLS-secret collector, which is what this source
	// has always read and what an existing deployment already has permission
	// for.
	SkipSecrets bool `json:"skipSecrets"`
	// The rest are opt-in: each needs a permission the previous release did not
	// ask for, and turning them on by default would take a run that exited 0
	// and make it exit 3 on upgrade.
	CertManager  bool `json:"certManager"`  // list on cert-manager.io
	TrustAnchors bool `json:"trustAnchors"` // ClusterRole: webhooks, apiservices, mesh roots
	// MeshSigningSecrets also reads Secrets holding cluster-wide mTLS CA
	// private keys. Read docs/rbac-readonly.yaml before setting it.
	MeshSigningSecrets bool `json:"meshSigningSecrets"`
	// MeshAnchors are read in addition to the built-in Linkerd and Istio
	// locations, not instead of them.
	MeshAnchors []source.MeshAnchor `json:"meshAnchors"`
}

// Vault points the Vault source at a server and the PKI mounts to read.
type Vault struct {
	Enabled bool   `json:"enabled"`
	Addr    string `json:"addr"` // empty = $VAULT_ADDR
	// Token never comes from the file -- a token in a config file is a token
	// in a git repository. Load fills it from $VAULT_TOKEN.
	Token     string   `json:"-"`
	Namespace string   `json:"namespace"`
	PKIMounts []string `json:"pkiMounts"`
	MaxCerts  int      `json:"maxCerts"`
}

// AWS points the AWS source at an account, and says which of ACM, IAM and
// Secrets Manager to skip.
type AWS struct {
	Enabled       bool   `json:"enabled"`
	Region        string `json:"region"`
	Profile       string `json:"profile"`
	MaxKeyAgeDays int    `json:"maxKeyAgeDays"` // access keys have no expiry; this is the rotation policy
	SkipACM       bool   `json:"skipACM"`
	SkipIAM       bool   `json:"skipIAM"`
	SkipSecrets   bool   `json:"skipSecrets"`
	// These need IAM permissions the first three do not; each can be turned
	// off on its own rather than costing the whole source.
	SkipRDS      bool `json:"skipRDS"`
	SkipPCA      bool `json:"skipPCA"`
	SkipIAMCerts bool `json:"skipIAMCerts"`
	SkipDomains  bool `json:"skipDomains"`
}

// Cloudflare points the Cloudflare source at an account and its zones.
type Cloudflare struct {
	Enabled bool `json:"enabled"`
	// Token never comes from the file — a token in a config file is a token in
	// a git repository. Load fills it from $CLOUDFLARE_API_TOKEN.
	Token string `json:"-"`
	// AccountID is needed for the registrar and Zero Trust reads; without it
	// those are skipped rather than guessed at.
	AccountID string `json:"accountId"`
	// Zones limits the scan to these zone IDs. Empty means every zone the
	// token can see.
	Zones       []string `json:"zones"`
	SkipZones   bool     `json:"skipZones"`
	SkipAccount bool     `json:"skipAccount"`
	SkipUser    bool     `json:"skipUser"`
}

// GitLab points the GitLab source at an instance and the projects and groups
// whose tokens to read.
type GitLab struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"baseUrl"` // empty = https://gitlab.com
	// Token never comes from the file. Load fills it from $GITLAB_TOKEN.
	Token string `json:"-"`
	// Projects and Groups are full paths ("acme/payments") or numeric IDs.
	// There is no cheap way to enumerate everything a token can see, so these
	// are named rather than discovered.
	Projects     []string `json:"projects"`
	Groups       []string `json:"groups"`
	SkipPersonal bool     `json:"skipPersonal"`
	SkipProjects bool     `json:"skipProjects"`
	SkipGroups   bool     `json:"skipGroups"`
}

// DigitalOcean points the DigitalOcean source at an account.
type DigitalOcean struct {
	Enabled bool `json:"enabled"`
	// Token never comes from the file. Load fills it from $DIGITALOCEAN_TOKEN.
	Token string `json:"-"`
}

// Scaleway points the Scaleway source at an organization and its LB zones.
type Scaleway struct {
	Enabled bool `json:"enabled"`
	// SecretKey never comes from the file. Load fills it from $SCW_SECRET_KEY.
	SecretKey string `json:"-"`
	// OrganizationID scopes the IAM key listing; empty skips it.
	OrganizationID string `json:"organizationId"`
	// Zones are load-balancer zones ("fr-par-1"). Certificates there are
	// zonal, so there is nothing to list without one.
	Zones       []string `json:"zones"`
	SkipDomains bool     `json:"skipDomains"`
	SkipLB      bool     `json:"skipLoadBalancers"`
	SkipKeys    bool     `json:"skipKeys"`
}

// Load reads and validates a config file, refusing one it cannot act on
// rather than silently watching nothing.
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
	// And again for the same reason: a mesh anchor with a misspelt kind falls
	// through to the Secret branch and 404s into silence, so an unvalidated one
	// fails by quietly watching nothing.
	if f.K8s != nil {
		if err := source.ValidateMeshAnchors(f.K8s.MeshAnchors); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		// Rejected rather than ignored: meshSigningSecrets on its own collects
		// nothing, because the mesh collector it feeds is behind trustAnchors.
		// Granting read access to CA private keys and silently getting no
		// findings for it is the worst of both.
		// Same rule, same reason: the mesh collector these feed is behind
		// trustAnchors, so without it the anchors are configured and never read.
		if len(f.K8s.MeshAnchors) > 0 && !f.K8s.TrustAnchors {
			return nil, fmt.Errorf(
				"%s: k8s.meshAnchors needs k8s.trustAnchors, or the anchors are never read", path)
		}
		if f.K8s.MeshSigningSecrets && !f.K8s.TrustAnchors {
			return nil, fmt.Errorf(
				"%s: k8s.meshSigningSecrets needs k8s.trustAnchors, or it reads private keys for nothing", path)
		}
	}
	// Same rule as Vault: the credential is read once at load and validated
	// with everything else, so a source that cannot authenticate fails while
	// the operator is still looking at the command rather than on first
	// collect.
	if f.Cloudflare != nil && f.Cloudflare.Enabled {
		f.Cloudflare.Token = os.Getenv("CLOUDFLARE_API_TOKEN") //nolint:forbidigo // FC-GEN-055: this is the startup read
		if f.Cloudflare.Token == "" {
			return nil, fmt.Errorf("%s: cloudflare source is enabled but $CLOUDFLARE_API_TOKEN is not set", path)
		}
	}
	if f.GitLab != nil && f.GitLab.Enabled {
		f.GitLab.Token = os.Getenv("GITLAB_TOKEN") //nolint:forbidigo // FC-GEN-055: this is the startup read
		if f.GitLab.Token == "" {
			return nil, fmt.Errorf("%s: gitlab source is enabled but $GITLAB_TOKEN is not set", path)
		}
	}
	// Rotation providers are list-shaped like endpoints and domains: naming one
	// enables it. Each reads its own credential from its own variable.
	if err := source.ValidateRotation(f.Rotation); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i := range f.Rotation {
		env := source.RotationProviders[f.Rotation[i].Name]
		f.Rotation[i].Token = os.Getenv(env) //nolint:forbidigo // FC-GEN-055: this is the startup read
		if f.Rotation[i].Token == "" {
			return nil, fmt.Errorf("%s: rotation provider %q is configured but $%s is not set",
				path, f.Rotation[i].Name, env)
		}
	}
	if f.DigitalOcean != nil && f.DigitalOcean.Enabled {
		f.DigitalOcean.Token = os.Getenv("DIGITALOCEAN_TOKEN") //nolint:forbidigo // FC-GEN-055: this is the startup read
		if f.DigitalOcean.Token == "" {
			return nil, fmt.Errorf("%s: digitalocean source is enabled but $DIGITALOCEAN_TOKEN is not set", path)
		}
	}
	if f.Scaleway != nil && f.Scaleway.Enabled {
		f.Scaleway.SecretKey = os.Getenv("SCW_SECRET_KEY") //nolint:forbidigo // FC-GEN-055: this is the startup read
		if f.Scaleway.SecretKey == "" {
			return nil, fmt.Errorf("%s: scaleway source is enabled but $SCW_SECRET_KEY is not set", path)
		}
	}
	// The environment is read here, once, and validated with the rest of the
	// config: a Vault source that cannot authenticate should fail while the
	// operator is still looking at the command, not on the first collect.
	if f.Vault != nil && f.Vault.Enabled {
		if f.Vault.Addr == "" {
			f.Vault.Addr = os.Getenv("VAULT_ADDR") //nolint:forbidigo // FC-GEN-055: this is the startup read
		}
		f.Vault.Token = os.Getenv("VAULT_TOKEN") //nolint:forbidigo // FC-GEN-055: this is the startup read
		if f.Vault.Addr == "" {
			return nil, fmt.Errorf("%s: vault source is enabled but neither vault.addr nor $VAULT_ADDR is set", path)
		}
		if f.Vault.Token == "" {
			return nil, fmt.Errorf("%s: vault source is enabled but $VAULT_TOKEN is not set", path)
		}
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
			Server:             f.K8s.Server,
			CAFile:             f.K8s.CAFile,
			Namespaces:         f.K8s.Namespaces,
			Insecure:           f.K8s.Insecure,
			SkipSecrets:        f.K8s.SkipSecrets,
			CertManager:        f.K8s.CertManager,
			TrustAnchors:       f.K8s.TrustAnchors,
			MeshSigningSecrets: f.K8s.MeshSigningSecrets,
			MeshAnchors:        f.K8s.MeshAnchors,
		})
	}
	if f.Vault != nil && f.Vault.Enabled {
		out = append(out, &source.VaultSource{
			Addr:      f.Vault.Addr,
			Token:     f.Vault.Token,
			Namespace: f.Vault.Namespace,
			PKIMounts: f.Vault.PKIMounts,
			MaxCerts:  f.Vault.MaxCerts,
		})
	}
	if f.Cloudflare != nil && f.Cloudflare.Enabled {
		out = append(out, &source.CloudflareSource{
			Token:       f.Cloudflare.Token,
			AccountID:   f.Cloudflare.AccountID,
			Zones:       f.Cloudflare.Zones,
			SkipZones:   f.Cloudflare.SkipZones,
			SkipAccount: f.Cloudflare.SkipAccount,
			SkipUser:    f.Cloudflare.SkipUser,
		})
	}
	if f.GitLab != nil && f.GitLab.Enabled {
		out = append(out, &source.GitLabSource{
			BaseURL:      f.GitLab.BaseURL,
			Token:        f.GitLab.Token,
			Projects:     f.GitLab.Projects,
			Groups:       f.GitLab.Groups,
			SkipPersonal: f.GitLab.SkipPersonal,
			SkipProjects: f.GitLab.SkipProjects,
			SkipGroups:   f.GitLab.SkipGroups,
		})
	}
	if len(f.Rotation) > 0 {
		out = append(out, &source.RotationSource{Providers: f.Rotation})
	}
	if f.DigitalOcean != nil && f.DigitalOcean.Enabled {
		out = append(out, &source.DigitalOceanSource{Token: f.DigitalOcean.Token})
	}
	if f.Scaleway != nil && f.Scaleway.Enabled {
		out = append(out, &source.ScalewaySource{
			SecretKey:      f.Scaleway.SecretKey,
			OrganizationID: f.Scaleway.OrganizationID,
			Zones:          f.Scaleway.Zones,
			SkipDomains:    f.Scaleway.SkipDomains,
			SkipLB:         f.Scaleway.SkipLB,
			SkipKeys:       f.Scaleway.SkipKeys,
		})
	}
	if f.AWS != nil && f.AWS.Enabled {
		out = append(out, &source.AWSSource{
			Region:       f.AWS.Region,
			Profile:      f.AWS.Profile,
			MaxKeyAge:    time.Duration(f.AWS.MaxKeyAgeDays) * 24 * time.Hour,
			SkipACM:      f.AWS.SkipACM,
			SkipIAM:      f.AWS.SkipIAM,
			SkipSecret:   f.AWS.SkipSecrets,
			SkipRDS:      f.AWS.SkipRDS,
			SkipPCA:      f.AWS.SkipPCA,
			SkipIAMCerts: f.AWS.SkipIAMCerts,
			SkipDomains:  f.AWS.SkipDomains,
		})
	}
	return out
}
