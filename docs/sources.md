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
| `vault` | the token's own TTL, and certificates in PKI mounts | `VAULT_TOKEN`, read + list |
| `aws` | ACM certificates, IAM access key age, Secrets Manager rotation | standard credential chain |
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

`skipSecrets: true` turns the default collector off. `meshSigningSecrets`
without `trustAnchors` is rejected at load rather than quietly collecting
nothing: granting read access to private keys and getting no findings for it is
the worst of both.

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
