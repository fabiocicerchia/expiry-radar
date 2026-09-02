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
