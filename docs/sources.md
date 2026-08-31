# Sources and permissions

## Sources (all read-only)

| Source | What it reads | Credentials |
| --- | --- | --- |
| `tls:endpoint` | leaf certificates from a live handshake | none |
| `tls:chain` | every intermediate CA presented, deduplicated across hosts | none |
| `domain:rdap` | registrar expiry over RDAP (the protocol that replaced WHOIS) | none |
| `k8s` | TLS secrets, with ingress class/hosts for context | service account, two `list` verbs |
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
pulling in client-go: two GETs against two stable endpoints do not justify that
dependency tree. For laptop use, run `kubectl proxy` and point `k8s.server` at
`http://127.0.0.1:8001`.

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
`intermediate_ca`, `secret`, `iam_access_key`, `vault_lease`, `domain`.

**`kind` is not a label.** It picks the base blast radius, so it decides where
the item lands in the ranking — a `domain` starts at 0.85, a `vault_lease` at
0.40. A misspelt kind is rejected at load rather than quietly ranked on a
middling default, because a plausible wrong number is worse than an error.

Beyond that a manual item is treated exactly like a discovered one: `namespace`
and `labels` feed the same blast-radius evidence (`public`, `traffic`,
`ingress.class`, and `expiry-radar/blast-radius` to set it outright), and
`overrides` match it by name like anything else. Nothing sits at the bottom of
the report for having been typed in.

Both editor integrations can write these for you — see
[Editor integration](editors.md).

## Non-negotiable: read-only

expiry-radar never needs write access, and the shipped credentials say so:

- `docs/iam-readonly-policy.json` — five List/Describe actions, nothing else.
- `docs/rbac-readonly.yaml` — `list` on ingresses and secrets, namespace-scoped
  by default (note: `list` on secrets returns private keys, so scope it).
- `docs/vault-readonly-policy.hcl` — read + list only.

Deliberately **not** implemented: enumerating Vault dynamic leases. That endpoint
is a PUT needing `update` plus `sudo`, which would break the read-only promise
this tool is sold on. PKI mounts answer the same question with reads.
