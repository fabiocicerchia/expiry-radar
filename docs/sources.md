# Sources and permissions

## Sources (all read-only)

| Source | What it reads | Credentials |
| --- | --- | --- |
| `tls:endpoint` | leaf certificates from a live handshake | none |
| `tls:chain` | every intermediate CA presented, deduplicated across hosts | none |
| `domain:rdap` | registrar expiry over RDAP (the protocol that replaced WHOIS) | none |
| `k8s:secret` | TLS secrets, with ingress class/hosts for context | service account, `list` |
| `k8s:cert-manager` | cert-manager `Certificate` CRs — whether renewal is working | `list` on `cert-manager.io` |
| `k8s:webhook` | admission webhook CA bundles | ClusterRole, `list` |
| `k8s:apiservice` | aggregation-layer `APIService` CA bundles | ClusterRole, `list` |
| `k8s:mesh` | Linkerd and Istio trust anchors and issuers | ClusterRole, `get` by name — **reads Secrets holding CA private keys**, see [`rbac-readonly.yaml`](rbac-readonly.yaml) |
| `cloudflare:edge` | edge certificate packs, per signature algorithm | `CLOUDFLARE_API_TOKEN`, `*:read` |
| `cloudflare:custom` | uploaded custom certificates — the ones nobody renews | same token |
| `cloudflare:mtls` | zone mTLS client certificates | same token |
| `cloudflare:registrar` | Registrar domains, with auto-renew state | same token + `accountId` |
| `cloudflare:access` | Zero Trust service tokens (one-year default) | same token + `accountId` |
| `cloudflare:token` | the API tokens themselves, including this one | same token |
| `gitlab:pat` | personal access tokens | `GITLAB_TOKEN`, `read_api` |
| `gitlab:project-token` | project access tokens — the ones that break CI | same token, maintainer on the project |
| `gitlab:group-token` | group access tokens | same token, owner on the group |
| `gitlab:deploy-token` | deploy tokens | same token |
| `gitlab:pages` | Pages custom-domain certificates | same token |
| `gcp:certificatemanager` | Certificate Manager certificates, per location | metadata server or a key file |
| `gcp:compute` | classic global SSL certificates | same |
| `gcp:secretmanager` | secret expiry **and** next rotation, separately | same |
| `gcp:iam` | user-managed service account keys, by age | same + `maxKeyAgeDays` |
| `github:org-token` | fine-grained PATs with org access | `GITHUB_TOKEN`, org **owner** |
| `github:gpg` | GPG signing keys with an expiry | `GITHUB_TOKEN` |
| `fastly:certificate` | TLS platform certificates | `FASTLY_API_TOKEN` |
| `fastly:token` | API tokens, ranked by scope | same |
| `hetzner:certificate` | Cloud load-balancer certificates | `HCLOUD_TOKEN` |
| `harbor:robot` | robot accounts — the registry credential that does expire | `HARBOR_PASSWORD` + admin |
| `jfrog:token` | Artifactory access tokens | `JFROG_ACCESS_TOKEN` + admin |
| `namecheap:domain` | registered domains, with auto-renew state | `NAMECHEAP_API_KEY` + an allowlisted IP |
| `namecheap:ssl` | resold SSL certificates | same |
| `digitalocean:certificate` | load-balancer and app certificates | `DIGITALOCEAN_TOKEN`, read |
| `scaleway:domain` | registered domains, with auto-renew state | `SCW_SECRET_KEY` |
| `scaleway:lb` | load-balancer certificates, per zone | same key + `zones` |
| `scaleway:iam` | API keys that carry an expiry | same key + `organizationId` |
| `vault` | the token's own TTL, and certificates in PKI mounts | `VAULT_TOKEN`, read + list |
| `aws` | ACM certificates, IAM access key age, Secrets Manager rotation | standard credential chain |
| `aws:rds-ca` / `aws:rds` | the regional CA bundle, and the CA each instance is pinned to | `rds:Describe*` |
| `aws:acm-pca` | Private CA authorities — trust anchors, 0.95 | `acm-pca:ListCertificateAuthorities` |
| `aws:iam-server-cert` | legacy uploaded ELB certificates | `iam:ListServerCertificates` |
| `aws:iam-saml` | SAML provider metadata validity | `iam:ListSAMLProviders` |
| `aws:route53domains` | registrations, with auto-renew state | `route53domains:ListDomains` |
| `azure:app` | app-registration client secrets and certificates | `AZURE_CLIENT_SECRET` + tenant/client id |
| `azure:serviceprincipal` | service-principal credentials, incl. SAML signing | same |
| `azure:keyvault` | Key Vault certificates, secrets and keys | same + `vaults` |
| `okta:api-token` | org API tokens | `OKTA_API_TOKEN` |
| `okta:app-key` | SAML application signing certificates | same |
| `federation:certificate` | IdP signing certificates, from published metadata | **none** |
| `federation:metadata` | the metadata document's own `validUntil` | **none** |
| `rotation:anthropic` | org API keys — **no expiry exists**, so age vs. policy | `ANTHROPIC_ADMIN_KEY` |
| `rotation:openai` | org admin keys, same shape | `OPENAI_ADMIN_KEY` |
| `rotation:dockerhub` | personal access tokens, with or without expiry | `DOCKERHUB_TOKEN` |
| `manual` | what you recorded yourself, because nothing can discover it | none |

The endpoint prober deliberately skips certificate verification: an
already-expired certificate is exactly what this tool exists to find, and
verification would reject the handshake before the date could be read. Nothing
is sent over the connection and no trust decision is made from it.

IAM access keys have no expiry — AWS will happily serve a five-year-old key —
so `maxKeyAgeDays` (default 90) turns key age into the rotation deadline the
rest of the tool can rank. That is the secret-rotation calendar, merged in.

The Kubernetes source talks to the API server with `net/http` rather than
pulling in client-go: a handful of GETs against stable, versioned endpoints do
not justify that dependency tree. For laptop use, run `kubectl proxy` and point
`k8s.server` at `http://127.0.0.1:8001`.

Each resource class is collected independently, so one denied permission costs
that class and nothing else: a cluster that will not show you
`validatingwebhookconfigurations` still reports its TLS secrets, and one
forbidden namespace does not lose the others. A skipped class is not the same as
a denied one or an empty one — the run stays quiet about a permission you have
decided not to grant.

**Everything beyond the TLS secrets is opt-in**, because each of these needs a
permission earlier releases never asked for, and turning them on by default
would take a run that exited 0 and make it exit 3 on upgrade:

| Config | Turns on | Needs |
| --- | --- | --- |
| *(default)* | TLS secrets and the ingresses that give them context | `list` on secrets and ingresses — what this source always needed |
| `certManager: true` | cert-manager `Certificate` CRs | `list` on `cert-manager.io` |
| `trustAnchors: true` | webhook and `APIService` CA bundles, mesh trust roots | a ClusterRole; all three are cluster-scoped |
| `meshSigningSecrets: true` | the mesh objects that hold the signing key too | `get` on Secrets containing **CA private keys** — see [`rbac-readonly.yaml`](rbac-readonly.yaml) |

`skipSecrets: true` turns the default collector off. Both `meshSigningSecrets`
and `meshAnchors` are rejected at load without `trustAnchors`, rather than
quietly collecting nothing — the mesh collector they feed is behind that flag,
and granting read access to private keys and getting no findings for it is the
worst of both.

### Trust anchors

The webhook, `APIService` and mesh collectors all report `trust_anchor`s: the CA
bundles the cluster validates *itself* against. Nobody watches these, and the
failure does not look like a certificate problem. An expired admission webhook
CA makes the API server stop admitting anything; an expired aggregation-layer
bundle takes out metrics-server and everything served through it; an expired
mesh root fails every mTLS handshake at once.

A CA pinned into a dozen webhooks is reported once, and so is one shared between
a webhook and an `APIService` — deduplication is by the certificate itself, not
by a name or a serial that two hand-made CAs can share, and `used-by` lists
every object relying on it across all three collectors. A CA that more than one
collector finds keeps what all of them knew: the sidecar-injector webhook and
`istio-system/cacerts` pin the same root, and the resulting row carries the
namespace and the `mesh`/`role`/`key` labels even though the webhook was read
first. Where a `caBundle` holds a chain, each member is reported under its own
name, since two rows on different dates cannot share one. A webhook with an empty
`caBundle` (CA injection) and an `APIService` served locally by the API server
have nothing to expire and are skipped.

Mesh anchors are fetched by name. **The defaults are ConfigMaps only** —
Linkerd's `linkerd-identity-trust-roots` and Istio's `istio-ca-root-cert` — and
that is deliberate: a ConfigMap holds the certificate and nothing else, so
reading one cannot expose a key. Istio distributes `istio-ca-root-cert` to every
namespace and it carries the same root as `istio-ca-secret`, without the private
key beside it.

`meshSigningSecrets: true` adds `linkerd-identity-issuer`, `cacerts` and
`istio-ca-secret`, each of which returns a CA private key along with the
certificate. What it buys is worth stating: the Linkerd issuer lapses in a year
by default and in twenty-four hours under cert-manager, which makes it the mesh
certificate most likely to expire unnoticed, and no ConfigMap exposes it. What
it costs is on the tin — read [`rbac-readonly.yaml`](rbac-readonly.yaml) first.

Each key is reported separately with its own role, because they are separate
certificates on separate clocks: Istio's `cacerts` holds the root under
`root-cert.pem` and the intermediate under `ca-cert.pem`. An anchor object that is absent
means the mesh is not installed and is passed over in silence; one that is
present with none of its keys is a configured anchor going unwatched, and says
so. Anything listed under `meshAnchors` is read **in addition to** the built-in
Linkerd and Istio locations, never instead of them.

### cert-manager

The `Certificate` CR is not a second date for a certificate the secret already
reported — it is whether the renewal that was supposed to make that date a
non-event is working.

So a `Certificate` that is `Ready` with its renewal still ahead of it labels its
secret `renewal=managed`, and ranking takes **0.25 off** the blast radius: a
deadline something else is demonstrably meeting is not a deadline you have to
act on. Automation that is failing gets no penalty and no bonus — a stuck
renewal floats up because everything around it moved down, not because the tool
guessed at how likely it is to break.

A `Certificate` is also reported on its own account when its secret is not
simply there and readable, and the `secret-state` label says which case it is:

| `secret-state` | Meaning |
| --- | --- |
| *(absent)* | the secret is reported with its own date; this Certificate only contributed renewal evidence |
| `unreadable` | the secret exists but no certificate could be parsed from it, so the CR is now the only readable source. Neither renewal claim is supported, so no `renewal` label |
| `missing` | the secrets **were** read and it is not there — the deadline is now, and `renewal=stuck`, because failing to produce the secret *is* the failure however Ready the CR reports itself |
| *(no label, own date)* | secrets were skipped or denied for that namespace, so nothing is claimed about the secret at all |

That last row is the point: "not issued" is a claim, and it is only made after
actually looking.

### Not covered: kubeadm control-plane certificates

`admin.conf`, the kubelet client certificate and the etcd peer certificates
expire a year after `kubeadm init` and are a real outage. They are also in
`/etc/kubernetes/pki` on the nodes rather than behind the API, so reading them
needs an agent on every node — a different product shape, and a privilege level
this tool has promised not to need. Said plainly here rather than half-covered.

The API server's own serving certificate *is* reachable: point a `tls` endpoint
at `:6443`.

## Cloudflare

The richest single token in the tool, and the best-behaved for ranking. Every
other source has to *infer* whether something is internet-facing; Cloudflare
knows. A zone name is the hostname, and a zone that is active and unpaused is
served from the edge by definition, so `hosts` and `public` are facts rather
than guesses. A paused zone gets `in-use=false` for the same reason.

It also supplies the renewal signal twice over, reusing the rule cert-manager
introduced — a deadline something else is demonstrably meeting is not one you
have to act on:

- a **universal or advanced** certificate pack that is `active` is renewed by
  Cloudflare, so it is labelled `renewal=managed` and de-ranked by 0.25;
- an **uploaded custom** certificate is not, so it keeps its full blast radius.
  That asymmetry is the point of reading this API at all;
- a **registrar** domain with `auto_renew` set is de-ranked the same way.

`accountId` is required for the registrar and Zero Trust reads. Without it they
are skipped rather than guessed at. The token goes in `$CLOUDFLARE_API_TOKEN`,
never in the config file.

Each scope — zones, account, user — collects independently, so a token scoped
to certificates only reports its certificates and warns about the rest instead
of losing them. A `200` carrying `success: false`, which this API returns more
readily than most, is treated as the error it is.

## GitLab

Everything here carries a real date, because GitLab caps token lifetimes — so
unlike most credential inventories these genuinely lapse rather than living
forever. The ones that break things quietly are project and group access
tokens: CI stops authenticating on a Tuesday morning and the pipeline log says
401.

Blast radius comes from **scope**, which is the one honest signal a forge can
give — a hostname tells you nothing here, but breadth of access tells you
exactly what an expiry costs. `api` scores 0.95 (everything the owner can do),
`write_repository`/`sudo` 0.85, `read_api` 0.55, and the read-only registry and
repository scopes 0.40. That goes in as the operator-style
`expiry-radar/blast-radius` label rather than as inference, because there is
nothing to infer when the provider states the scope outright.

Pages domains follow the same renewal rule as everywhere else: `auto_ssl` is
GitLab renewing for you and is de-ranked, an uploaded certificate is not.
A revoked token gets `in-use=false` — it has already stopped working, so its
expiry is not a deadline anybody has to meet.

`projects` and `groups` are named rather than discovered. There is no cheap way
to enumerate everything a token can see, and hammering the API to find out is
not a read-only posture worth defending. GitLab answers `404` for a resource
the token cannot see as well as for one that is not there, so the warning says
both.

## The rest of AWS

The original three adapters covered ACM, IAM access keys and Secrets Manager
rotation. Four more services have dates and did not.

**RDS** is the one AWS itself publishes a Prescriptive Guidance pattern for
detecting, which says something about how often it bites. Two questions, both
asked: `DescribeCertificates` says which regional CA bundles exist and which is
the account default, and each instance's `CertificateDetails` says which one
that database will actually present. The second is what breaks — a driver that
verifies stops connecting — so a CA nothing is pinned to gets `in-use=false`
and a publicly accessible instance gets `public=true`, both straight from the
API rather than inferred.

**ACM Private CA** authorities are `trust_anchor`s at 0.95, for the reason the
kind exists: an expired private CA invalidates everything it ever signed at
once.

**IAM server certificates** are the legacy pre-ACM ELB uploads — put there once
by somebody who has since left, and invisible unless you go looking.
**SAML providers** are worse: when one lapses every federated login stops
together, and it does not look like a certificate problem.

**Route 53 Domains** adds what every registrar API adds over RDAP — the date is
the same, `auto_renew` is the part only the registrar knows.

Each is a separate unit behind the same seam as the original three, with its own
skip, so an account that denies `acm-pca` still reports its RDS certificates.
`docs/iam-readonly-policy.json` carries the six new read-only actions.

## Truncation, pagination and other ways to be quietly wrong

A few notes that apply across the provider sources, because the failure they
share is the one this tool exists to prevent — a report that looks clean because
something was not read.

- **Every list is paged to the end, or says it was not.** Google Cloud follows
  `nextPageToken`; Cloudflare, GitLab and GitHub follow their own cursors.
  Where an API returns a total instead of a cursor (DigitalOcean's `meta.total`,
  Scaleway's `total_count`), reading fewer than the total is a warning, because
  100 of 240 with nothing said looks exactly like an account that has 100.
- **A 404 is not an empty account.** Google Cloud reports it rather than
  swallowing it: a mistyped project would otherwise produce zero items, zero
  warnings and exit 0.
- **A 200 is not a success** on Cloudflare (`success: false`) or Namecheap
  (`Status="ERROR"`), and both are checked on the body rather than the code.
- **Route 53 Domains only exists in `us-east-1`**, so that client is pinned
  there regardless of the region being scanned.

## Google Cloud

The data here is ordinary REST; the cost of this source is authentication, and
it is paid in about eighty lines of `crypto/rsa` rather than by taking a
dependency. Two routes, which is all Google needs:

- **On GCP**, the metadata server answers and no key file exists — so none can
  leak. This is the better posture and the default when
  `GOOGLE_APPLICATION_CREDENTIALS` is unset.
- **Off GCP**, a service account key file signs its own JWT assertion and
  exchanges it for an access token. The scope requested is
  `cloud-platform.read-only`.

`projects` are named rather than discovered: enumerating them needs
`resourcemanager` permissions most read-only roles do not carry. An API that is
simply not enabled on a project answers `404`, which is an answer about the
project rather than a failure, so it passes quietly.

Two things are worth calling out. **Secret Manager carries two different dates**
— an outright `expireTime` and a `rotation.nextRotationTime` — and neither is
the other, so both are reported as their own row. And **a user-managed service
account key is valid for about ten years by default**, which is not a deadline
anybody means; `maxKeyAgeDays` synthesises the rotation deadline instead and the
row is labelled `deadline=rotation policy` with `created` and `policy.days`,
exactly as the AWS IAM and rotation sources do. Set `maxKeyAgeDays: 0` to report
Google's own date as-is. Google-managed keys are rotated for you and are skipped.

Managed certificates — `MANAGED` in Compute, an `ACTIVE` managed cert in
Certificate Manager — are de-ranked; self-managed uploads are not.

## GitHub, and what it cannot tell you

This source is deliberately thin, and the gap is the useful part.

**The GitHub credentials that hurt when they lapse are not readable by any
API.** A GitHub App's private key, an App client secret and a classic personal
access token have no list endpoint at all. Nothing can discover them, so they
belong in `manual` with a `renew-at` label — and a fatter adapter here would
imply coverage that does not exist.

For GitHub Enterprise Server, set `baseUrl` to the host; `/api/v3` is appended
for you, since that is where GHES serves the API and `api.github.com` has no
prefix.

What is readable: fine-grained PATs with access to an organization, which an
org **owner** can list (a token that merely belongs to the org gets a 403, and
the warning says so rather than reporting an empty org), and a user's GPG keys.
A token granted across `all` repositories carries a wider blast radius than one
scoped to `selected`, and GitHub states which rather than leaving it to be
inferred. When a GPG key lapses, commit signatures stop verifying and protected
branches that require signed commits start rejecting pushes.

Compare GitLab above, which exposes far more. That asymmetry is real and not
worth papering over.

## Edge and registry competitors

**Fastly** covers the same ground as Cloudflare with one difference worth
having: its API tokens are scoped, sometimes to individual services, so an
expiry is a blast-radius question and the token says how wide. A `global` token
carries 0.85; one pinned to named services is left to normal inference. The TLS
API is JSON:API, so the dates live under `attributes` rather than at the top
level.

**Hetzner Cloud** is small like DigitalOcean — not a registrar, no token
listing — so certificates are the whole of it. Its managed certificates carry a
renewal *status*, which makes the de-rank more honest than elsewhere: managed
**and** scheduled earns it, managed **and failing** gets `renewal=stuck`
instead.

**Harbor** is the Docker Hub competitor worth watching, because unlike Docker
Hub its robot accounts carry a real expiry and are routinely created once for a
pipeline and never looked at again. When one lapses, deploys stop pulling and
the error surfaces in the cluster rather than near the registry. Note Harbor
spells "never expires" as `-1`, which read as a Unix timestamp would land in
1969 and top the report as decades overdue; it is skipped instead. A
`system`-level robot reaches every project, so it carries 0.75.

**JFrog Artifactory** access tokens are the same story, and an
`applied-permissions/admin` scope carries 0.85.

## Namecheap

Two things make this source unlike the others, and both are worth knowing
before you wire it into CI.

Credentials travel in the **POST body**, not the query string, and that is a
security decision rather than a stylistic one: Go wraps transport failures in
`*url.Error`, which prints the full URL, and `net/http` redacts only userinfo
passwords. With the key in the URL, a single timeout would have written a
full-account registrar credential to stderr and into CI logs — on the failure
path an operator is most likely to paste into a ticket.

It requires **the calling machine's public IP to be allowlisted** in the
Namecheap account. Error `1011150` means the credentials are fine and the
machine is not — it reads like a bad API key and is not one, so the warning
says which it is. `clientIp` is required at load for the same reason.

And it answers **HTTP 200 for failures**, with `Status="ERROR"` in the body. The
status code alone would read every error as an empty account, so the attribute
is what decides.

As with Scaleway and Route 53, the date is not why you hold the credential —
RDAP gives you that for free. `AutoRenew` is. Dates come back as `MM/DD/YYYY`
with no timezone and are read as the start of that day in UTC, which errs
towards warning early; a certificate bought but never issued gets
`in-use=false`.

## DigitalOcean and Scaleway

**DigitalOcean is deliberately one endpoint.** It hosts DNS but is not a
registrar, so there are no registrations to report, and its personal access
tokens have no list endpoint. `/v2/certificates` is the honest extent of what
this API exposes that expires. A `lets_encrypt` certificate that is verified is
renewed by DigitalOcean and de-ranked; an uploaded `custom` one is not.

**Scaleway's value is that it is a registrar.** The `domains` source already
reports registry expiry for any domain over RDAP with no credentials at all, so
a second date would add nothing — what Scaleway adds is `auto_renew_status`,
which is the difference between a date existing and somebody having to act on
it. Enabled auto-renew is de-ranked like any other automated renewal.

Load-balancer certificates are zonal, so `zones` has to name them; IAM keys need
`organizationId`. Both are skipped rather than guessed at when the identifier is
missing. Only keys that carry a real `expires_at` are reported — Scaleway allows
keys without one, and those are a rotation-policy question rather than a
deadline this source can read, the same line the AWS IAM adapter draws.

## Identity: the things nobody owns

### Entra ID (Azure)

The biggest gap in most estates. Every app registration and service principal
carries client secrets and certificates on their own schedules, nothing
surfaces them until an integration stops authenticating, and Microsoft's own
answer is a PowerShell script you run by hand — which is a fair admission that
there was no good one.

Service principals are read as well as applications, because a principal's
`Verify` key credential is the **SAML signing certificate**: when that lapses,
every sign-in through the app stops at the same moment, so it is reported as a
`trust_anchor` rather than an ordinary certificate.

Two token audiences are needed and there is no way around it — Graph and Key
Vault are separate resources with separate scopes — so the source acquires and
caches one token each. Vaults are named rather than discovered: enumerating
them needs ARM permissions, and a source whose promise is read-only data-plane
access should not be asking for the control plane.

### Okta

Unusual in stating a real date for both things it reports. API tokens carry
`expiresAt` outright; an application's signing key comes back as a JWK whose
`x5c` is a real certificate, so the expiry is read from the certificate itself
rather than synthesised. Only SAML applications are visited — they are the ones
with a signing certificate, and that keeps this from becoming one request per
app in the org.

### Federation metadata — no credential at all

The best value-per-line in the tool. IdP metadata is *published to be fetched*,
so this needs nothing but a URL, and it reports two different deadlines from
one document:

- the **signing certificates**, which stop every federated login at once;
- the metadata's own **`validUntil`**, which relying parties are entitled to
  refuse after.

Both `EntityDescriptor` (one provider) and `EntitiesDescriptor` (a federation)
are handled, certificates listed under more than one `use` are reported once,
and a URL that parses as XML but holds no dates is an error rather than a
provider with nothing expiring — that would be the clean-estate failure again.

## Credentials with no expiry at all

Anthropic, OpenAI and Docker Hub issue keys that simply never expire. There is
no date to discover, so this source does not pretend to discover one: it reads
the **age**, takes the rotation policy you set, and reports the deadline that
follows.

That deadline is yours, not the provider's, and the report says so. Every
synthesised row carries `created`, `policy.days` and `deadline=rotation policy`,
exactly as the AWS IAM adapter labels access keys — AWS will happily serve a
five-year-old key, and so will these. Where a provider *does* state an expiry
(Docker Hub grew expiring tokens later than it grew tokens) that date is used
as-is and labelled `deadline=issuer`.

Naming a provider in `rotation` **enables** it, the way a non-empty `endpoints`
or `domains` list does — so the shipped example ships an empty array. Copying an
example that named three providers would fail to load until all three
environment variables were set, which is not a first-run experience worth
having.

`maxKeyAgeDays` is required and has no default. A deadline nobody chose is not a
policy, and inventing one would put a date in the report that no human ever
agreed to — so a provider without it is rejected at load.

Two de-ranks fall out for free: an inactive or revoked key gets `in-use=false`,
and an OpenAI key that has never been used is flagged the same way — a key never
used and never expiring is one to delete rather than rotate.

Mistral is not here. Its keys are console-only with no documented list endpoint,
so there is nothing to read; record them in `manual` instead of shipping a stub
that implies coverage.

## Recorded, or discovered

The sources split in two, and it decides how a thing gets into the inventory:

- **Discovered.** You grant read access to a system and it enumerates what is
  there: `k8s`, `vault`, `aws`, and the chain intermediates behind every
  endpoint. You never list these; that is the point, since the ones that bite
  are the ones nobody remembered to list.
- **Recorded.** You name it in `expiry-radar.json` and a source goes and reads
  its deadline: `endpoints` (dialled over TLS) and `domains` (looked up over
  RDAP).

Between the two sits everything that expires and that no system will tell you
about: a domain at a registrar with no RDAP, a credential rotated by hand, a
code-signing certificate on somebody's laptop, a support contract, a hardware
token. That is what `manual` is for.

```json
{
  "manual": [
    { "name": "acme-corp.co.uk", "kind": "domain", "expires": "2027-03-01",
      "labels": { "public": "true", "renew-at": "https://registrar.example/domains" } },
    { "name": "code-signing", "kind": "tls_cert", "expires": "2026-11-15",
      "namespace": "release" }
  ]
}
```

`expires` takes `YYYY-MM-DD` or a full RFC 3339 timestamp; a bare day means its
start in UTC, which errs towards warning early. `kind` is one of `tls_cert`,
`intermediate_ca`, `secret`, `iam_access_key`, `vault_lease`, `domain`,
`trust_anchor`.

**`kind` is not a label.** It picks the base blast radius, so it decides where
the item lands in the ranking — a `trust_anchor` starts at 0.95, a `domain` at
0.85, a `vault_lease` at 0.40. A misspelt kind is rejected at load rather than quietly ranked on a
middling default, because a plausible wrong number is worse than an error.

Beyond that a manual item is treated exactly like a discovered one: `namespace`
and `labels` feed the same blast-radius evidence (`public`, `traffic`,
`ingress.class`, and `expiry-radar/blast-radius` to set it outright), and
`overrides` match it by name like anything else. Nothing sits at the bottom of
the report for having been typed in.

Both editor integrations can write these for you — see
[Editor integration](editors.md).

## Verifying the AWS adapters

The ACM, IAM and Secrets Manager adapters compile and vet clean, and until now
had never run with real credentials. Nothing confirmed the field mappings or
the expiry semantics, and mocked responses cannot: they assert that the code
does what it was written to do, and the failure being guarded against is a
field meaning something other than what was assumed.

```sh
expiry-radar -verify-aws
```

Read-only, like everything else here — it runs the same three adapters and
reports what they returned.

### What it can decide

| check | how |
| --- | --- |
| each adapter ran | it returned items, or it did not, or it was denied, or it was skipped — four different outcomes, kept apart |
| a denied service degrades rather than fails the run | one service refused while the others still returned items |
| nothing without an expiry is reported as expiring | a zero `time.Time` reads as 1 January year 1 and would rank as the most urgent thing in the account |
| pagination past one page | a service returned more than a full page (100) |
| expiries span a range and sort | more than one dated item, in order |

**An inconclusive check is not a pass.** An account with nothing in it satisfies
every criterion written as "nothing was wrong", and reporting that as evidence
would be a lie — so an empty adapter, a skipped one, a run where nothing was
denied and a run with fewer than a page of results all print `?`, not `ok`.

### What it cannot

Nothing in the output can confirm that a date **means** what the adapter
assumed. That is the failure a live run exists to catch, and it needs the
console open beside the report:

- each expiry matches what the console shows for that resource
- an IAM key's "expiry" is its age against `maxKeyAgeDays`, not a date AWS
  reports — the console shows the **creation** date, so check the arithmetic
- a rotating secret's next rotation matches the schedule on the secret itself

The report prints that list every time, so it is not something a reader has to
remember.

### It is safe to paste into an issue

Counts, outcomes and expiry **offsets in days** — never ARNs, account ids,
domain names or secret names. The guarantee is structural rather than a filter:
the verdict type has nowhere to put an identifier, and a test asserts that an
ARN, an account id and a certificate's domain cannot reach the text.

## Non-negotiable: read-only

expiry-radar never needs write access, and the shipped credentials say so:

- `docs/iam-readonly-policy.json` — five List/Describe actions, nothing else.
- `docs/rbac-readonly.yaml` — `list` on ingresses and secrets, namespace-scoped
  by default (note: `list` on secrets returns private keys, so scope it).
- `docs/vault-readonly-policy.hcl` — read + list only.

Deliberately **not** implemented: enumerating Vault dynamic leases. That endpoint
is a PUT needing `update` plus `sudo`, which would break the read-only promise
this tool is sold on. PKI mounts answer the same question with reads.
