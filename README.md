# expiry-radar

[![CI](https://github.com/fabiocicerchia/expiry-radar/actions/workflows/code-quality.yml/badge.svg)](https://github.com/fabiocicerchia/expiry-radar/actions/workflows/code-quality.yml)
[![Security](https://github.com/fabiocicerchia/expiry-radar/actions/workflows/security.yml/badge.svg)](https://github.com/fabiocicerchia/expiry-radar/actions/workflows/security.yml)
[![License](https://img.shields.io/badge/license-Apache_2.0-blue.svg)](LICENSE)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/fabiocicerchia/expiry-radar/badge)](https://securityscorecards.dev/viewer/?uri=github.com/fabiocicerchia/expiry-radar)


One inventory of everything that expires — TLS certs, intermediate CAs, secrets,
IAM keys, Vault leases, domains — ranked by blast radius, so the cert on the
payment path outranks the one on a staging dashboard.

Merge of the former `tls-expiry-radar` and `secret-rotation-calendar`: they are
one tool with one code path. Building two would just be two dormant repos.

**Ranking by blast radius is the entire value.** Any script can list expiry
dates; the value is ordering by consequence, inferred from traffic, ingress
class and namespace, and overridable.

## Usage

```
make build
./bin/expiry-radar -endpoints shop.example.com -domains example.com
./bin/expiry-radar -config expiry-radar.json -format ical -out renewals.ics
./bin/expiry-radar -config expiry-radar.json -format prometheus
./bin/expiry-radar -config expiry-radar.json -fail-within 14   # CI gate
```

Copy `expiry-radar.example.json` to `expiry-radar.json` to enable the
credentialed sources. Flags add to the config rather than replacing it, so a
one-off probe needs no config file at all.

Exit codes: `0` clean · `1` a `-fail-within` threshold was breached · `2` bad
usage or config · `3` partial results, at least one source failed. A source that
fails still returns what it managed to read, and the failure is printed to
stderr — a report that quietly lost a source reads exactly like a clean estate.

## How the ranking works

`priority = 0.55 × urgency + 0.45 × blast radius`, sorted descending.

Urgency ramps linearly from 0 at ninety days out to 1 on the expiry date, and
stays at 1 once expired. It is a **weighted sum, not a product**, on purpose: a
product ranks everything beyond the horizon at exactly zero and throws away the
ordering that makes the tool worth running.

Blast radius starts from the kind (a domain or an intermediate CA outranks a
leaf certificate, because it takes out everything below it) and is then moved by
whatever evidence exists: internet-facing, production vs non-production
namespace, wildcard or multi-SAN coverage, reported traffic, and whether an ACM
certificate is in use at all. Environment detection matches whole tokens, so
`device-registry` is not "dev" and `reproduction-service` is not "prod".

Two things override inference, in order: an `expiry-radar/blast-radius` label on
the resource, then an operator `overrides` glob in the config. Inference exists
because most estates have no reliable labels — not because it knows better.

Every row carries a `WHY` column. A ranking nobody can explain gets ignored.

## Sources (all read-only)

| Source | What it reads | Credentials |
| --- | --- | --- |
| `tls:endpoint` | leaf certificates from a live handshake | none |
| `tls:chain` | every intermediate CA presented, deduplicated across hosts | none |
| `domain:rdap` | registrar expiry over RDAP (the protocol that replaced WHOIS) | none |
| `k8s` | TLS secrets, with ingress class/hosts for context | service account, two `list` verbs |
| `vault` | the token's own TTL, and certificates in PKI mounts | `VAULT_TOKEN`, read + list |
| `aws` | ACM certificates, IAM access key age, Secrets Manager rotation | standard credential chain |

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

## Non-negotiable: read-only

expiry-radar never needs write access, and the shipped credentials say so:

- `docs/iam-readonly-policy.json` — five List/Describe actions, nothing else.
- `docs/rbac-readonly.yaml` — `list` on ingresses and secrets, namespace-scoped
  by default (note: `list` on secrets returns private keys, so scope it).
- `docs/vault-readonly-policy.hcl` — read + list only.

Deliberately **not** implemented: enumerating Vault dynamic leases. That endpoint
is a PUT needing `update` plus `sudo`, which would break the read-only promise
this tool is sold on. PKI mounts answer the same question with reads.

## Outputs

Ranked table (CLI) · JSON for CI · Prometheus gauges (`expiry_radar_seconds_left`,
`_blast_radius`, `_priority`, `_items`; only low-cardinality labels) · an iCal
feed of all-day renewal events with alarms scaled by blast radius — 30 days'
notice at ≥0.8, 14 at ≥0.6, 7 otherwise — and stable UIDs so refreshing the feed
updates events instead of duplicating them.

## Status

Working. `make test` covers ranking, all four renderers, and the TLS/RDAP/k8s
sources against local test servers (including an expired certificate and a
partially-failed scan).

**Not verified against a live AWS account**: the ACM/IAM/Secrets Manager adapter
compiles and vets clean but has never been run with real credentials. Treat the
first run as a smoke test.

Kill criterion: if blast-radius ranking can't be made meaningfully better than an
unsorted list, stop. See `../ROADMAP.md`.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Security issues go through
[GitHub Security Advisories](https://github.com/fabiocicerchia/expiry-radar/security/advisories/new),
never a public issue — see [SECURITY.md](SECURITY.md).

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
