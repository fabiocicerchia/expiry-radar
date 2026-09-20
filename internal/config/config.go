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
	Manual     []source.ManualItem `json:"manual"`
	K8s        *K8s                `json:"k8s"`
	Vault      *Vault              `json:"vault"`
	AWS        *AWS                `json:"aws"`
	Cloudflare *Cloudflare         `json:"cloudflare"`
	GitLab     *GitLab             `json:"gitlab"`
	GitHub     *GitHub             `json:"github"`
	GCP        *GCP                `json:"gcp"`
	Azure      *Azure              `json:"azure"`
	Okta       *Okta               `json:"okta"`
	Fastly     *Fastly             `json:"fastly"`
	Hetzner    *Hetzner            `json:"hetzner"`
	Harbor     *Harbor             `json:"harbor"`
	JFrog      *JFrog              `json:"jfrog"`
	Apple      *Apple              `json:"apple"`
	// Federation reads IdP metadata and needs no credential at all.
	Federation   []source.FederationProvider `json:"federation"`
	DigitalOcean *DigitalOcean               `json:"digitalocean"`
	Scaleway     *Scaleway                   `json:"scaleway"`
	Namecheap    *Namecheap                  `json:"namecheap"`
	// Registrars inventory domain registrations; see source.RegistrarSource.
	Registrars []source.RegistrarProvider `json:"registrars"`
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

// Namecheap points the Namecheap source at an account. Note clientIp: the
// Namecheap API requires the calling machine's public IP to be allowlisted in
// the account, which is a deployment constraint worth knowing before wiring
// this into CI.
type Namecheap struct {
	Enabled  bool   `json:"enabled"`
	APIUser  string `json:"apiUser"`
	UserName string `json:"userName"`
	// APIKey never comes from the file. Load fills it from $NAMECHEAP_API_KEY.
	APIKey string `json:"-"`
	// ClientIP must be this machine's allowlisted public IP.
	ClientIP    string `json:"clientIp"`
	Sandbox     bool   `json:"sandbox"`
	SkipDomains bool   `json:"skipDomains"`
	SkipSSL     bool   `json:"skipSSL"`
}

// GitHub points the GitHub source at organizations. Deliberately small: the
// GitHub credentials that hurt when they lapse — App private keys, App client
// secrets, classic PATs — have no list endpoint, so they belong in `manual`.
type GitHub struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"baseUrl"` // empty = api.github.com; set for Enterprise Server
	// Token never comes from the file. Load fills it from $GITHUB_TOKEN.
	Token string `json:"-"`
	// Orgs to inspect. Listing an org's fine-grained tokens needs owner.
	Orgs          []string `json:"orgs"`
	SkipOrgTokens bool     `json:"skipOrgTokens"`
	SkipGPGKeys   bool     `json:"skipGpgKeys"`
}

// GCP points the Google Cloud source at projects. Credentials come from the
// metadata server when running on GCP, which holds no key at all, or from a
// service account key file named by $GOOGLE_APPLICATION_CREDENTIALS.
type GCP struct {
	Enabled bool `json:"enabled"`
	// Projects are named rather than discovered: enumerating them needs
	// resourcemanager permissions most read-only roles do not carry.
	Projects []string `json:"projects"`
	// Locations for Certificate Manager, which is regional. Default: global.
	Locations []string `json:"locations"`
	// CredentialsFile is filled from $GOOGLE_APPLICATION_CREDENTIALS; empty
	// means ask the metadata server.
	CredentialsFile string `json:"-"`
	// MaxKeyAgeDays turns service account key age into a rotation deadline.
	// A user-managed key is valid for about ten years by default, which is not
	// a deadline anybody means. 0 reports the issuer's date as-is.
	MaxKeyAgeDays   int  `json:"maxKeyAgeDays"`
	SkipCertManager bool `json:"skipCertManager"`
	SkipCompute     bool `json:"skipCompute"`
	SkipSecrets     bool `json:"skipSecrets"`
	SkipKeys        bool `json:"skipKeys"`
}

// Azure points the Entra ID source at a tenant, and optionally at Key Vaults.
type Azure struct {
	Enabled  bool   `json:"enabled"`
	TenantID string `json:"tenantId"`
	ClientID string `json:"clientId"`
	// ClientSecret never comes from the file. Load fills it from
	// $AZURE_CLIENT_SECRET.
	ClientSecret string `json:"-"`
	// Vaults are Key Vault names, not URLs. Enumerating them would need ARM
	// permissions this source deliberately does not ask for.
	Vaults           []string `json:"vaults"`
	SkipApplications bool     `json:"skipApplications"`
	SkipPrincipals   bool     `json:"skipPrincipals"`
	SkipVaults       bool     `json:"skipVaults"`
}

// Okta points the Okta source at a tenant.
type Okta struct {
	Enabled bool   `json:"enabled"`
	OrgURL  string `json:"orgUrl"`
	// Token never comes from the file. Load fills it from $OKTA_API_TOKEN.
	Token      string `json:"-"`
	SkipTokens bool   `json:"skipTokens"`
	SkipApps   bool   `json:"skipApps"`
}

// Fastly points the Fastly source at an account.
type Fastly struct {
	Enabled bool `json:"enabled"`
	// Token never comes from the file. Load fills it from $FASTLY_API_TOKEN.
	Token            string `json:"-"`
	SkipCertificates bool   `json:"skipCertificates"`
	SkipTokens       bool   `json:"skipTokens"`
}

// Hetzner points the Hetzner Cloud source at a project.
type Hetzner struct {
	Enabled bool `json:"enabled"`
	// Token never comes from the file. Load fills it from $HCLOUD_TOKEN.
	Token string `json:"-"`
}

// Harbor points the Harbor source at a registry instance.
type Harbor struct {
	Enabled  bool   `json:"enabled"`
	BaseURL  string `json:"baseUrl"`
	Username string `json:"username"`
	// Password never comes from the file. Load fills it from $HARBOR_PASSWORD.
	Password string `json:"-"`
}

// JFrog points the Artifactory source at a platform.
type JFrog struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"baseUrl"`
	// Token never comes from the file. Load fills it from
	// $JFROG_ACCESS_TOKEN.
	Token string `json:"-"`
}

// Apple points the App Store Connect source at a team. The .p8 key is a file
// path rather than a secret in the config: Apple lets you download it exactly
// once, so it already lives on disk somewhere.
type Apple struct {
	Enabled  bool   `json:"enabled"`
	IssuerID string `json:"issuerId"`
	KeyID    string `json:"keyId"`
	// PrivateKeyFile is filled from $APP_STORE_CONNECT_KEY_FILE when the
	// config does not name one.
	PrivateKeyFile   string `json:"privateKeyFile"`
	SkipCertificates bool   `json:"skipCertificates"`
	SkipProfiles     bool   `json:"skipProfiles"`
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
	// Registrars are list-shaped too, and each reads its own credentials.
	if err := source.ValidateRegistrars(f.Registrars); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i := range f.Registrars {
		envs := source.RegistrarProviders[f.Registrars[i].Name]
		f.Registrars[i].Token = os.Getenv(envs[0]) //nolint:forbidigo // FC-GEN-055: this is the startup read
		if f.Registrars[i].Token == "" {
			return nil, fmt.Errorf("%s: registrar %q is configured but $%s is not set",
				path, f.Registrars[i].Name, envs[0])
		}
		if envs[1] != "" {
			f.Registrars[i].Secret = os.Getenv(envs[1]) //nolint:forbidigo // FC-GEN-055: this is the startup read
			if f.Registrars[i].Secret == "" {
				return nil, fmt.Errorf("%s: registrar %q is configured but $%s is not set",
					path, f.Registrars[i].Name, envs[1])
			}
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
	if err := source.ValidateFederation(f.Federation); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Azure != nil && f.Azure.Enabled {
		if f.Azure.TenantID == "" || f.Azure.ClientID == "" {
			return nil, fmt.Errorf("%s: azure.tenantId and azure.clientId are both required", path)
		}
		f.Azure.ClientSecret = os.Getenv("AZURE_CLIENT_SECRET") //nolint:forbidigo // FC-GEN-055: this is the startup read
		if f.Azure.ClientSecret == "" {
			return nil, fmt.Errorf("%s: azure source is enabled but $AZURE_CLIENT_SECRET is not set", path)
		}
	}
	if f.Fastly != nil && f.Fastly.Enabled {
		f.Fastly.Token = os.Getenv("FASTLY_API_TOKEN") //nolint:forbidigo // FC-GEN-055: this is the startup read
		if f.Fastly.Token == "" {
			return nil, fmt.Errorf("%s: fastly source is enabled but $FASTLY_API_TOKEN is not set", path)
		}
	}
	if f.Hetzner != nil && f.Hetzner.Enabled {
		f.Hetzner.Token = os.Getenv("HCLOUD_TOKEN") //nolint:forbidigo // FC-GEN-055: this is the startup read
		if f.Hetzner.Token == "" {
			return nil, fmt.Errorf("%s: hetzner source is enabled but $HCLOUD_TOKEN is not set", path)
		}
	}
	if f.Harbor != nil && f.Harbor.Enabled {
		if f.Harbor.BaseURL == "" || f.Harbor.Username == "" {
			return nil, fmt.Errorf("%s: harbor.baseUrl and harbor.username are both required", path)
		}
		f.Harbor.Password = os.Getenv("HARBOR_PASSWORD") //nolint:forbidigo // FC-GEN-055: this is the startup read
		if f.Harbor.Password == "" {
			return nil, fmt.Errorf("%s: harbor source is enabled but $HARBOR_PASSWORD is not set", path)
		}
	}
	if f.JFrog != nil && f.JFrog.Enabled {
		if f.JFrog.BaseURL == "" {
			return nil, fmt.Errorf("%s: jfrog.baseUrl is required, e.g. https://acme.jfrog.io", path)
		}
		f.JFrog.Token = os.Getenv("JFROG_ACCESS_TOKEN") //nolint:forbidigo // FC-GEN-055: this is the startup read
		if f.JFrog.Token == "" {
			return nil, fmt.Errorf("%s: jfrog source is enabled but $JFROG_ACCESS_TOKEN is not set", path)
		}
	}
	if f.Apple != nil && f.Apple.Enabled {
		if f.Apple.PrivateKeyFile == "" {
			f.Apple.PrivateKeyFile = os.Getenv("APP_STORE_CONNECT_KEY_FILE") //nolint:forbidigo // FC-GEN-055: this is the startup read
		}
		if f.Apple.IssuerID == "" || f.Apple.KeyID == "" || f.Apple.PrivateKeyFile == "" {
			return nil, fmt.Errorf(
				"%s: apple needs issuerId, keyId and privateKeyFile (or $APP_STORE_CONNECT_KEY_FILE)", path)
		}
	}
	if f.Okta != nil && f.Okta.Enabled {
		if f.Okta.OrgURL == "" {
			return nil, fmt.Errorf("%s: okta.orgUrl is required, e.g. https://acme.okta.com", path)
		}
		f.Okta.Token = os.Getenv("OKTA_API_TOKEN") //nolint:forbidigo // FC-GEN-055: this is the startup read
		if f.Okta.Token == "" {
			return nil, fmt.Errorf("%s: okta source is enabled but $OKTA_API_TOKEN is not set", path)
		}
	}
	if f.GCP != nil && f.GCP.Enabled {
		// Not required: on GCP the metadata server answers and no key file
		// exists, which is the better posture of the two.
		f.GCP.CredentialsFile = os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") //nolint:forbidigo // FC-GEN-055: this is the startup read
		if len(f.GCP.Projects) == 0 {
			return nil, fmt.Errorf("%s: gcp source is enabled but gcp.projects is empty", path)
		}
	}
	if f.GitHub != nil && f.GitHub.Enabled {
		f.GitHub.Token = os.Getenv("GITHUB_TOKEN") //nolint:forbidigo // FC-GEN-055: this is the startup read
		if f.GitHub.Token == "" {
			return nil, fmt.Errorf("%s: github source is enabled but $GITHUB_TOKEN is not set", path)
		}
	}
	if f.Namecheap != nil && f.Namecheap.Enabled {
		// What the file itself got wrong is checked before what the
		// environment is missing: a config error is the operator's to fix
		// either way, and reporting it first keeps the message about the file
		// they are looking at.
		if f.Namecheap.APIUser == "" || f.Namecheap.UserName == "" {
			return nil, fmt.Errorf("%s: namecheap.apiUser and namecheap.userName are both required", path)
		}
		if f.Namecheap.ClientIP == "" {
			return nil, fmt.Errorf(
				"%s: namecheap.clientIp is required — it must be this machine's public IP, allowlisted in the account", path)
		}
		f.Namecheap.APIKey = os.Getenv("NAMECHEAP_API_KEY") //nolint:forbidigo // FC-GEN-055: this is the startup read
		if f.Namecheap.APIKey == "" {
			return nil, fmt.Errorf("%s: namecheap source is enabled but $NAMECHEAP_API_KEY is not set", path)
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
	if len(f.Federation) > 0 {
		out = append(out, &source.FederationSource{Providers: f.Federation})
	}
	if f.Azure != nil && f.Azure.Enabled {
		out = append(out, &source.AzureSource{
			TenantID:         f.Azure.TenantID,
			ClientID:         f.Azure.ClientID,
			ClientSecret:     f.Azure.ClientSecret,
			Vaults:           f.Azure.Vaults,
			SkipApplications: f.Azure.SkipApplications,
			SkipPrincipals:   f.Azure.SkipPrincipals,
			SkipVaults:       f.Azure.SkipVaults,
		})
	}
	if f.Fastly != nil && f.Fastly.Enabled {
		out = append(out, &source.FastlySource{
			Token:            f.Fastly.Token,
			SkipCertificates: f.Fastly.SkipCertificates,
			SkipTokens:       f.Fastly.SkipTokens,
		})
	}
	if f.Hetzner != nil && f.Hetzner.Enabled {
		out = append(out, &source.HetznerSource{Token: f.Hetzner.Token})
	}
	if f.Harbor != nil && f.Harbor.Enabled {
		out = append(out, &source.HarborSource{
			BaseURL:  f.Harbor.BaseURL,
			Username: f.Harbor.Username,
			Password: f.Harbor.Password,
		})
	}
	if f.JFrog != nil && f.JFrog.Enabled {
		out = append(out, &source.JFrogSource{BaseURL: f.JFrog.BaseURL, Token: f.JFrog.Token})
	}
	if f.Apple != nil && f.Apple.Enabled {
		out = append(out, &source.AppleSource{
			IssuerID:         f.Apple.IssuerID,
			KeyID:            f.Apple.KeyID,
			PrivateKeyFile:   f.Apple.PrivateKeyFile,
			SkipCertificates: f.Apple.SkipCertificates,
			SkipProfiles:     f.Apple.SkipProfiles,
		})
	}
	if f.Okta != nil && f.Okta.Enabled {
		out = append(out, &source.OktaSource{
			OrgURL:     f.Okta.OrgURL,
			Token:      f.Okta.Token,
			SkipTokens: f.Okta.SkipTokens,
			SkipApps:   f.Okta.SkipApps,
		})
	}
	if f.GCP != nil && f.GCP.Enabled {
		out = append(out, &source.GCPSource{
			Projects:        f.GCP.Projects,
			Locations:       f.GCP.Locations,
			CredentialsFile: f.GCP.CredentialsFile,
			MaxKeyAgeDays:   f.GCP.MaxKeyAgeDays,
			SkipCertManager: f.GCP.SkipCertManager,
			SkipCompute:     f.GCP.SkipCompute,
			SkipSecrets:     f.GCP.SkipSecrets,
			SkipKeys:        f.GCP.SkipKeys,
		})
	}
	if f.GitHub != nil && f.GitHub.Enabled {
		out = append(out, &source.GitHubSource{
			Token:         f.GitHub.Token,
			BaseURL:       f.GitHub.BaseURL,
			Orgs:          f.GitHub.Orgs,
			SkipOrgTokens: f.GitHub.SkipOrgTokens,
			SkipGPGKeys:   f.GitHub.SkipGPGKeys,
		})
	}
	if f.Namecheap != nil && f.Namecheap.Enabled {
		out = append(out, &source.NamecheapSource{
			APIUser:     f.Namecheap.APIUser,
			UserName:    f.Namecheap.UserName,
			APIKey:      f.Namecheap.APIKey,
			ClientIP:    f.Namecheap.ClientIP,
			Sandbox:     f.Namecheap.Sandbox,
			SkipDomains: f.Namecheap.SkipDomains,
			SkipSSL:     f.Namecheap.SkipSSL,
		})
	}
	if len(f.Registrars) > 0 {
		out = append(out, &source.RegistrarSource{Providers: f.Registrars})
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
