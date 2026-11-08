# Specifications of expiry-radar

What this code does, one atomic Gherkin scenario per behaviour, read from the code by code-atlas.
Tags: `@verified` a test asserts it · `@inferred` read from the code only · `@gap` an entry point no test covers · `@human` written or amended by a person, never rewritten.

## Certificates and trust
Finding TLS certificates, CAs and signing certificates before they lapse.

- **App signing**: [Tracking Apple signing certificates and provisioning profiles](certificates/app-signing/apple-signing-expiry.feature) (2)
- **Cluster trust anchors**: [Watching the trust anchors a cluster validates against](certificates/cluster-trust/cluster-trust-anchors.feature) (3)
- **Identity federation**: [Watching identity-provider metadata for expiry](certificates/identity-federation/identity-provider-metadata.feature) (4)
- **Kubernetes certificates**: [Checking that cert-manager renewal is actually working](certificates/kubernetes-certificates/cert-manager-renewal-evidence.feature) (5), [Inventorying TLS certificates held in a Kubernetes cluster](certificates/kubernetes-certificates/kubernetes-tls-secrets.feature) (1)
- **TLS endpoints**: [Reading certificates from live TLS endpoints](certificates/tls-endpoints/tls-endpoint-certificates.feature) (7)

## Cloud providers
Read-only inventory of expiring resources in each cloud and edge platform.

- **AWS**: [Collecting expiring items from AWS](cloud-providers/aws/aws-expiry-collection.feature) (4)
- **AWS extended coverage**: [Tracking AWS expiries beyond certificates, keys and secrets](cloud-providers/aws-coverage-gaps/aws-extended-coverage.feature) (6)
- **Azure**: [Finding what expires in Entra ID and Key Vault](cloud-providers/azure/azure-expiries.feature) (4)
- **Cloudflare**: [Reading the Cloudflare API](cloud-providers/cloudflare/cloudflare-api-access.feature) (4)
- **Google Cloud**: [Finding what expires in Google Cloud](cloud-providers/gcp/gcp-expiries.feature) (2)
- **Provider API access**: [Reading provider APIs with a credential](cloud-providers/http-api-client/provider-api-access.feature) (4)
- **Other clouds and CDNs**: [Tracking DigitalOcean certificates](cloud-providers/other-clouds/digitalocean-certificates.feature) (1), [Tracking Scaleway domains, load-balancer certificates and API keys](cloud-providers/other-clouds/scaleway-expiries.feature) (2)

## Configuration
Telling the tool what to scan and what each thing means to the business.

- **Config file**: [Loading the config file](configuration/config-file/loading-the-config-file.feature) (3)
- **Manual entries**: [Declaring expiries by hand](configuration/manual-inventory/manual-inventory-entries.feature) (4)
- **Source setup**: [Choosing which sources a scan runs](configuration/source-setup/source-opt-in.feature) (4)
- **Validation**: [Validating federation provider configuration](configuration/validation/federation-provider-validation.feature) (1), [Refusing source configuration that cannot scan](configuration/validation/refusing-unusable-source-config.feature) (1)

## Secrets and credentials
Tracking tokens, keys, leases and secrets that expire or must be rotated.

- **Code-hosting tokens**: [Tracking GitHub token and signing-key expiry](credentials/code-hosting-tokens/github-credentials.feature) (1), [Tracking GitLab tokens and Pages certificates](credentials/code-hosting-tokens/gitlab-tokens-and-pages-certificates.feature) (3)
- **Identity provider tokens**: [Tracking Okta API tokens and SAML signing certificates](credentials/identity-tokens/okta-tokens-and-signing-certificates.feature) (3)
- **Registry credentials**: [Tracking registry credential expiry](credentials/registry-credentials/registry-credential-expiry.feature) (1)
- **Rotation policy**: [Marking rotation-policy deadlines](credentials/rotation-policy/policy-deadline-labelling.feature) (1), [Giving never-expiring API keys a rotation deadline](credentials/rotation-policy/rotation-policy-deadlines.feature) (8)
- **Vault**: [Tracking what expires in Vault](credentials/vault/vault-expiry.feature) (1)

## Developer tooling
Building, testing, installing and verifying the tool itself.

- **Build and install**: [Installing expiry-radar from a checkout](developer-tooling/build-and-install/installing-from-a-checkout.feature) (5)
- **Live account verification**: [Verifying the AWS adapters against a live account](developer-tooling/live-verification/aws-live-verification.feature) (2), [Verifying the AWS adapters against a real account](developer-tooling/live-verification/verifying-aws-adapters.feature) (2)

## Domain names
Knowing when domain registrations lapse and whether they will auto-renew.

- **Public registration lookup**: [Reading domain registration expiry](domains/rdap-lookup/domain-registration-expiry.feature) (1)
- **Registrar accounts**: [Tracking domains and certificates held at Namecheap](domains/registrar-accounts/namecheap-registrar.feature) (1), [Reading domain renewal status from registrar accounts](domains/registrar-accounts/registrar-renewal-status.feature) (1)

## Editor integrations
Seeing and acting on the inventory from inside Neovim and VS Code.

- **Editing entries**: [Recording and removing entries from Neovim](editor-integrations/editing-entries/nvim-editing-entries.feature) (6)
- **Editor settings**: [Neovim plugin settings](editor-integrations/editor-settings/nvim-settings.feature) (5)
- **Health check**: [Diagnosing the Neovim setup](editor-integrations/health-check/nvim-health-check.feature) (5)
- **Inventory view**: [Browsing the ranked inventory in Neovim](editor-integrations/inventory-view/nvim-inventory-view.feature) (7)
- **Running scans**: [Running collections from Neovim](editor-integrations/scheduled-scans/nvim-running-scans.feature) (8)

## Blast-radius ranking
Ordering the inventory by how much breaks when each item lapses, not just by date.

- **Risk scoring**: [Ranking expiring items by blast radius](prioritisation/risk-scoring/blast-radius-ranking.feature) (8)

## Reports and exports
Presenting the ranked inventory to people and to other systems.

- **Calendar feed**: [Publishing expiries to the team calendar](reporting/calendar-feed/calendar-feed.feature) (6)
- **Format selection**: [Choosing a report format](reporting/format-selection/choosing-a-report-format.feature) (4)
- **HTML report**: [Sharing a standalone HTML report of the ranked inventory](reporting/html-report/html-report.feature) (8)
- **JSON export**: [Exporting the ranked inventory as JSON](reporting/json-export/json-export.feature) (5)
- **Prometheus metrics**: [Exposing expiries as Prometheus metrics](reporting/metrics/prometheus-metrics.feature) (4)
- **Terminal table**: [Reading the ranked inventory in the terminal](reporting/terminal-table/ranked-terminal-table.feature) (4)

## Scanning
How a run collects every expiring thing from every configured source and merges it into one inventory.

- **Concurrent collection**: [Collecting every source under one deadline](scanning/collection/concurrent-collection.feature) (2)
- **Partial failure**: [Reporting what a Kubernetes scan could not read](scanning/partial-failure/kubernetes-partial-failure.feature) (3), [Keeping findings when part of a scan fails](scanning/partial-failure/partial-collection-failure.feature) (4)
- **Run command**: [Running a scan from the command line](scanning/run-command/running-a-scan.feature) (8)
