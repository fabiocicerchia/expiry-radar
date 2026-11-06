package config

import (
	"time"

	"github.com/fabiocicerchia/expiry-radar/internal/source"
)

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
			Token:             f.Cloudflare.Token,
			AccountID:         f.Cloudflare.AccountID,
			Zones:             f.Cloudflare.Zones,
			MaxKeyAgeDays:     f.Cloudflare.MaxKeyAgeDays,
			SkipZones:         f.Cloudflare.SkipZones,
			SkipAccount:       f.Cloudflare.SkipAccount,
			SkipUser:          f.Cloudflare.SkipUser,
			SkipAccountTokens: f.Cloudflare.SkipAccountTokens,
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
