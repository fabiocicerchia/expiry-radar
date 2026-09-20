# Getting Started

## Install

```sh
go install github.com/fabiocicerchia/expiry-radar/cmd/expiry-radar@latest
```

Or from a checkout:

```sh
make build      # -> ./bin/
make install    # or put it on your PATH: $GOBIN, $GOPATH/bin, or PREFIX=/usr/local
```

## First run

```
make build
./bin/expiry-radar -endpoints shop.example.com -domains example.com
./bin/expiry-radar -config expiry-radar.json -format ical -out renewals.ics
./bin/expiry-radar -config expiry-radar.json -format prometheus
./bin/expiry-radar -config expiry-radar.json -format html -out report.html
./bin/expiry-radar -config expiry-radar.json -fail-within 14   # CI gate
```

Copy `expiry-radar.example.json` to `expiry-radar.json` to enable the
credentialed sources. Flags add to the config rather than replacing it, so a
one-off probe needs no config file at all.

Runnable examples live in [`examples/`](https://github.com/fabiocicerchia/expiry-radar/tree/main/examples):
`basic/` needs no credentials at all, and `multi-provider/` shows a config with
several providers wired up and the environment variable each one needs.

## What it can see

Twenty-three sources, all read-only, all producing the same `Item`. Start with
the ones that need **no credential at all** — they work on the first run:

| | Sources |
| --- | --- |
| No credential | `tls:endpoint`, `domain:rdap`, `federation` (SAML/OIDC metadata), `manual` |
| Clouds | `aws`, `gcp`, `azure`, `scaleway`, `digitalocean`, `hetzner`, `k8s`, `vault` |
| Edge | `cloudflare`, `fastly` |
| Registrars | `namecheap`, `registrar` (DNSimple, Gandi, Porkbun, GoDaddy) |
| Identity | `okta`, and Entra ID via `azure` |
| Forges & registries | `gitlab`, `github`, `harbor`, `jfrog` |
| Code signing | `apple` |
| Keys with no expiry | `rotation` (Anthropic, OpenAI, Docker Hub) |

[`sources.md`](sources.md) has the full table: what each one reads, the exact
permission it needs, and where a provider's answer is weaker than it looks.

Run one at a time with `-only`, which is the quickest way to see what a single
source reports without waiting for the rest:

```sh
./bin/expiry-radar -config expiry-radar.json -only cloudflare
```

`-only` narrows what is **collected**, not what is **validated**. The config is
loaded as a whole first, so every block you enabled still needs its environment
variable set, whether or not `-only` names it. To run without a provider's
credentials, take its block out of the config.

Naming a source the config did not enable is an error rather than an empty
report — otherwise a typo would look exactly like an estate with nothing to
fix.

Exit codes: `0` clean · `1` a `-fail-within` threshold was breached · `2` bad
usage or config · `3` partial results, at least one source failed. A source that
fails still returns what it managed to read, and the failure is printed to
stderr — a report that quietly lost a source reads exactly like a clean estate.

The [README](README.md) covers what expiry-radar does and why.
