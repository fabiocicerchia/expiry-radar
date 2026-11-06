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
	Zones []string `json:"zones"`
	// MaxKeyAgeDays is the rotation policy for API tokens created without an
	// expiry. No default: the policy is the only deadline such a token has, and
	// a deadline nobody chose is not a policy. Unset, they stay out.
	MaxKeyAgeDays     int  `json:"maxKeyAgeDays"`
	SkipZones         bool `json:"skipZones"`
	SkipAccount       bool `json:"skipAccount"`
	SkipUser          bool `json:"skipUser"`
	SkipAccountTokens bool `json:"skipAccountTokens"`
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
	if err := f.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &f, nil
}
