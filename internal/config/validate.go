package config

import (
	"fmt"
	"os"

	"github.com/fabiocicerchia/expiry-radar/internal/rank"
	"github.com/fabiocicerchia/expiry-radar/internal/source"
)

// validate checks the decoded file and fills credentials from the
// environment. The order of the checks is the order errors are reported in.
func (f *File) validate() error {
	// Rejected at load, not at rank time: a malformed glob never matches, so an
	// unvalidated override fails by quietly not applying — exactly the case the
	// operator wrote it to prevent.
	if err := rank.ValidateOverrides(f.Overrides); err != nil {
		return err
	}
	// Rejected at load for the same reason: a manual item exists because
	// nothing else can find the thing. One that silently fails to parse leaves
	// no trace anywhere, which is the one outcome it was written to prevent.
	if err := source.ValidateManual(f.Manual); err != nil {
		return err
	}
	if err := f.K8s.validate(); err != nil {
		return err
	}
	// Same rule as Vault: the credential is read once at load and validated
	// with everything else, so a source that cannot authenticate fails while
	// the operator is still looking at the command rather than on first
	// collect.
	if f.Cloudflare != nil && f.Cloudflare.Enabled {
		if err := requireEnv(&f.Cloudflare.Token, "cloudflare", "CLOUDFLARE_API_TOKEN"); err != nil {
			return err
		}
	}
	if f.GitLab != nil && f.GitLab.Enabled {
		if err := requireEnv(&f.GitLab.Token, "gitlab", "GITLAB_TOKEN"); err != nil {
			return err
		}
	}
	if err := f.loadRegistrars(); err != nil {
		return err
	}
	if err := f.loadRotation(); err != nil {
		return err
	}
	if err := source.ValidateFederation(f.Federation); err != nil {
		return err
	}
	if f.Azure != nil && f.Azure.Enabled {
		if f.Azure.TenantID == "" || f.Azure.ClientID == "" {
			return fmt.Errorf("azure.tenantId and azure.clientId are both required")
		}
		if err := requireEnv(&f.Azure.ClientSecret, "azure", "AZURE_CLIENT_SECRET"); err != nil {
			return err
		}
	}
	if f.Fastly != nil && f.Fastly.Enabled {
		if err := requireEnv(&f.Fastly.Token, "fastly", "FASTLY_API_TOKEN"); err != nil {
			return err
		}
	}
	if f.Hetzner != nil && f.Hetzner.Enabled {
		if err := requireEnv(&f.Hetzner.Token, "hetzner", "HCLOUD_TOKEN"); err != nil {
			return err
		}
	}
	if f.Harbor != nil && f.Harbor.Enabled {
		if f.Harbor.BaseURL == "" || f.Harbor.Username == "" {
			return fmt.Errorf("harbor.baseUrl and harbor.username are both required")
		}
		if err := requireEnv(&f.Harbor.Password, "harbor", "HARBOR_PASSWORD"); err != nil {
			return err
		}
	}
	if f.JFrog != nil && f.JFrog.Enabled {
		if f.JFrog.BaseURL == "" {
			return fmt.Errorf("jfrog.baseUrl is required, e.g. https://acme.jfrog.io")
		}
		if err := requireEnv(&f.JFrog.Token, "jfrog", "JFROG_ACCESS_TOKEN"); err != nil {
			return err
		}
	}
	if f.Apple != nil && f.Apple.Enabled {
		if f.Apple.PrivateKeyFile == "" {
			//nolint:forbidigo // FC-GEN-055: this is the startup read
			f.Apple.PrivateKeyFile = os.Getenv("APP_STORE_CONNECT_KEY_FILE")
		}
		if f.Apple.IssuerID == "" || f.Apple.KeyID == "" || f.Apple.PrivateKeyFile == "" {
			return fmt.Errorf("apple needs issuerId, keyId and privateKeyFile (or $APP_STORE_CONNECT_KEY_FILE)")
		}
	}
	if f.Okta != nil && f.Okta.Enabled {
		if f.Okta.OrgURL == "" {
			return fmt.Errorf("okta.orgUrl is required, e.g. https://acme.okta.com")
		}
		if err := requireEnv(&f.Okta.Token, "okta", "OKTA_API_TOKEN"); err != nil {
			return err
		}
	}
	if f.GCP != nil && f.GCP.Enabled {
		// Not required: on GCP the metadata server answers and no key file
		// exists, which is the better posture of the two.
		//nolint:forbidigo // FC-GEN-055: this is the startup read
		f.GCP.CredentialsFile = os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
		if len(f.GCP.Projects) == 0 {
			return fmt.Errorf("gcp source is enabled but gcp.projects is empty")
		}
	}
	if f.GitHub != nil && f.GitHub.Enabled {
		if err := requireEnv(&f.GitHub.Token, "github", "GITHUB_TOKEN"); err != nil {
			return err
		}
	}
	if f.Namecheap != nil && f.Namecheap.Enabled {
		// What the file itself got wrong is checked before what the
		// environment is missing: a config error is the operator's to fix
		// either way, and reporting it first keeps the message about the file
		// they are looking at.
		if f.Namecheap.APIUser == "" || f.Namecheap.UserName == "" {
			return fmt.Errorf("namecheap.apiUser and namecheap.userName are both required")
		}
		if f.Namecheap.ClientIP == "" {
			return fmt.Errorf(
				"namecheap.clientIp is required — it must be this machine's public IP, allowlisted in the account")
		}
		if err := requireEnv(&f.Namecheap.APIKey, "namecheap", "NAMECHEAP_API_KEY"); err != nil {
			return err
		}
	}
	if f.DigitalOcean != nil && f.DigitalOcean.Enabled {
		if err := requireEnv(&f.DigitalOcean.Token, "digitalocean", "DIGITALOCEAN_TOKEN"); err != nil {
			return err
		}
	}
	if f.Scaleway != nil && f.Scaleway.Enabled {
		if err := requireEnv(&f.Scaleway.SecretKey, "scaleway", "SCW_SECRET_KEY"); err != nil {
			return err
		}
	}
	return f.Vault.load()
}

// requireEnv fills dst from the variable env, refusing an enabled source whose
// credential is unset.
func requireEnv(dst *string, name, env string) error {
	*dst = os.Getenv(env) //nolint:forbidigo // FC-GEN-055: this is the startup read
	if *dst == "" {
		return fmt.Errorf("%s source is enabled but $%s is not set", name, env)
	}
	return nil
}

// validate rejects Kubernetes options that would be configured and never read.
func (k *K8s) validate() error {
	if k == nil {
		return nil
	}
	// And again for the same reason: a mesh anchor with a misspelt kind falls
	// through to the Secret branch and 404s into silence, so an unvalidated one
	// fails by quietly watching nothing.
	if err := source.ValidateMeshAnchors(k.MeshAnchors); err != nil {
		return err
	}
	// Rejected rather than ignored: meshSigningSecrets on its own collects
	// nothing, because the mesh collector it feeds is behind trustAnchors.
	// Granting read access to CA private keys and silently getting no
	// findings for it is the worst of both.
	// Same rule, same reason: the mesh collector these feed is behind
	// trustAnchors, so without it the anchors are configured and never read.
	if len(k.MeshAnchors) > 0 && !k.TrustAnchors {
		return fmt.Errorf("k8s.meshAnchors needs k8s.trustAnchors, or the anchors are never read")
	}
	if k.MeshSigningSecrets && !k.TrustAnchors {
		return fmt.Errorf("k8s.meshSigningSecrets needs k8s.trustAnchors, or it reads private keys for nothing")
	}
	return nil
}

// loadRegistrars validates the registrars and reads their credentials.
// Registrars are list-shaped too, and each reads its own credentials.
func (f *File) loadRegistrars() error {
	if err := source.ValidateRegistrars(f.Registrars); err != nil {
		return err
	}
	for i := range f.Registrars {
		r := &f.Registrars[i]
		envs := source.RegistrarProviders[r.Name]
		r.Token = os.Getenv(envs[0]) //nolint:forbidigo // FC-GEN-055: this is the startup read
		if r.Token == "" {
			return fmt.Errorf("registrar %q is configured but $%s is not set", r.Name, envs[0])
		}
		if envs[1] != "" {
			r.Secret = os.Getenv(envs[1]) //nolint:forbidigo // FC-GEN-055: this is the startup read
			if r.Secret == "" {
				return fmt.Errorf("registrar %q is configured but $%s is not set", r.Name, envs[1])
			}
		}
	}
	return nil
}

// loadRotation validates the rotation providers and reads their tokens.
// Rotation providers are list-shaped like endpoints and domains: naming one
// enables it. Each reads its own credential from its own variable.
func (f *File) loadRotation() error {
	if err := source.ValidateRotation(f.Rotation); err != nil {
		return err
	}
	for i := range f.Rotation {
		r := &f.Rotation[i]
		env := source.RotationProviders[r.Name]
		r.Token = os.Getenv(env) //nolint:forbidigo // FC-GEN-055: this is the startup read
		if r.Token == "" {
			return fmt.Errorf("rotation provider %q is configured but $%s is not set", r.Name, env)
		}
	}
	return nil
}

// load reads the Vault address and token. The environment is read here, once,
// and validated with the rest of the config: a Vault source that cannot
// authenticate should fail while the operator is still looking at the
// command, not on the first collect.
func (v *Vault) load() error {
	if v == nil || !v.Enabled {
		return nil
	}
	if v.Addr == "" {
		v.Addr = os.Getenv("VAULT_ADDR") //nolint:forbidigo // FC-GEN-055: this is the startup read
	}
	v.Token = os.Getenv("VAULT_TOKEN") //nolint:forbidigo // FC-GEN-055: this is the startup read
	if v.Addr == "" {
		return fmt.Errorf("vault source is enabled but neither vault.addr nor $VAULT_ADDR is set")
	}
	if v.Token == "" {
		return fmt.Errorf("vault source is enabled but $VAULT_TOKEN is not set")
	}
	return nil
}
