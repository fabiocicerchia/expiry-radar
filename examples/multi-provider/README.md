# Multi-provider Example

What it shows: one config covering an estate that spans several providers, and
**which environment variable each one needs**. Credentials never appear in the
config file — it names what to scan, and the process environment supplies what
is secret.

## Run

You need real credentials for the providers you leave enabled. Delete the blocks
you do not have; every source is independent.

```sh
export CLOUDFLARE_API_TOKEN=...   GITLAB_TOKEN=...
export OKTA_API_TOKEN=...         VAULT_ADDR=https://vault.example:8200
export VAULT_TOKEN=...            GANDI_API_KEY=...
export PORKBUN_API_KEY=...        PORKBUN_SECRET_KEY=...
export ANTHROPIC_ADMIN_KEY=...    DOCKERHUB_TOKEN=...
# aws uses the standard AWS credential chain — no variable of its own

cd examples/multi-provider
../../bin/expiry-radar -config expiry-radar.json
```

## What each block needs

| Config block | Environment | Notes |
| --- | --- | --- |
| `cloudflare` | `CLOUDFLARE_API_TOKEN` | Read-scoped, plus `Account API Tokens Read` for the account's own tokens. The only source that can fill `traffic` |
| `gitlab` | `GITLAB_TOKEN` | `read_api`. Token scope is the blast radius |
| `okta` | `OKTA_API_TOKEN` | API tokens *and* SAML app signing certificates |
| `vault` | `VAULT_ADDR` + `VAULT_TOKEN` | Read + list. `addr` may be set in the config instead |
| `registrars` | `GANDI_API_KEY`, `PORKBUN_API_KEY` + `PORKBUN_SECRET_KEY` | One variable pair per named adapter |
| `rotation` | `ANTHROPIC_ADMIN_KEY`, `DOCKERHUB_TOKEN` | See the warning below |
| `aws` | standard AWS credential chain | Region is in the config |
| `federation` | none | Public IdP metadata; no account anywhere |
| `endpoints`, `domains`, `manual` | none | |

The full list, including the providers not enabled here, is in
[`docs/sources.md`](../../docs/sources.md) and in `man 1 expiry-radar` under
`ENVIRONMENT`.

## Run one source at a time

Bringing eleven sources up at once means eleven ways to get a warning. `-only`
narrows the run to one:

```sh
../../bin/expiry-radar -config expiry-radar.json -only cloudflare
../../bin/expiry-radar -config expiry-radar.json -only manual      # no network at all
```

It narrows what is **collected**, not what is **validated**. The config is
loaded as a whole before anything runs, so `-only cloudflare` still fails if
`GITLAB_TOKEN` is unset while the `gitlab` block is enabled — a config that is
half-broken should say so rather than wait until the day you stop passing
`-only`. Delete the blocks you have no credentials for; that is what makes this
file yours rather than a template.

Naming a source this config did not enable is an error, not an empty report:

```
$ ../../bin/expiry-radar -config expiry-radar.json -only fastly
expiry-radar: unknown source "fastly" in -only; this config built: aws,
cloudflare, domain:rdap, federation, gitlab, manual, okta, registrar, rotation,
tls:endpoint, vault
```

## Two things this config is being honest about

**`rotation` deadlines are policy, not dates.** Anthropic, OpenAI and Docker Hub
keys mostly do not expire. `maxKeyAgeDays` is your rotation policy, and the
deadline reported is `created + maxKeyAgeDays`. There is deliberately no
default: a deadline nobody chose is not a policy. Those rows carry a
`policy.days` label so the report never implies the provider stated a date.

**`registrars` mostly duplicate `domains`.** RDAP already reports registry
expiry for any domain with no credentials at all. What a registrar API adds is
whether **auto-renew is actually on**, which de-ranks the domains nobody has to
touch — so the ones that do need paying rise on their own.

## Overrides

The `overrides` block at the end is where inference gets corrected: anything
under `payments/` is pinned to the maximum blast radius, and anything with
`sandbox` in its name is pinned near zero. Earlier entries win.
