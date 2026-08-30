# expiry-radar

[![CI](https://github.com/fabiocicerchia/expiry-radar/actions/workflows/code-quality.yml/badge.svg)](https://github.com/fabiocicerchia/expiry-radar/actions/workflows/code-quality.yml)
[![Security](https://github.com/fabiocicerchia/expiry-radar/actions/workflows/security.yml/badge.svg)](https://github.com/fabiocicerchia/expiry-radar/actions/workflows/security.yml)
[![License](https://img.shields.io/badge/license-Apache_2.0-blue.svg)](LICENSE)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/fabiocicerchia/expiry-radar/badge)](https://securityscorecards.dev/viewer/?uri=github.com/fabiocicerchia/expiry-radar)
[![CI carbon](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/fabiocicerchia/expiry-radar/gh-pages/badge.json)](.github/workflows/carbon-badge.yml)

One inventory of everything that expires — TLS certs, intermediate CAs, secrets,
IAM keys, Vault leases, domains — ranked by blast radius, so the cert on the
payment path outranks the one on a staging dashboard.

Merge of the former `tls-expiry-radar` and `secret-rotation-calendar`: they are
one tool with one code path. Building two would just be two dormant repos.

**Ranking by blast radius is the entire value.** Any script can list expiry
dates; the value is ordering by consequence, inferred from traffic, ingress
class and namespace, and overridable.

## Install

```sh
go install github.com/fabiocicerchia/expiry-radar/cmd/expiry-radar@latest
```

Or from a checkout:

```sh
make build                    # -> ./bin/
make install                  # -> $GOBIN, or $GOPATH/bin
make install PREFIX=/usr/local  # -> /usr/local/bin
```

## Usage

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

Most of the inventory is **discovered** — grant read access and the sources
enumerate. `endpoints` and `domains` you **record**, and `manual` holds what
nothing can discover: a registrar with no RDAP, a credential rotated by hand, a
code-signing certificate on somebody's laptop. Manual items are ranked by the
same rules as everything else — see [`docs/sources.md`](docs/sources.md).

Exit codes: `0` clean · `1` a `-fail-within` threshold was breached · `2` bad
usage or config · `3` partial results, at least one source failed. A source that
fails still returns what it managed to read, and the failure is printed to
stderr — a report that quietly lost a source reads exactly like a clean estate.

## Editors

The same binary, in the editor: a ranked panel, diagnostics on the lines of
`expiry-radar.json` that declared what is about to break, and the full HTML
report in a tab.

```sh
code --install-extension fabiocicerchia.expiry-radar   # from the Marketplace
make ext-install                                       # or from this checkout
```

`make ext-install` packages and installs the VS Code extension and symlinks the
Neovim plugin onto your packpath, so `git pull` updates both. `make ext-uninstall`
removes them.

Neovim 0.11+ users get [`extensions/nvim`](extensions/nvim) — `:ExpiryRadarReport`,
`:ExpiryRadarList`, `:ExpiryRadarProbe`. Both live in [`extensions/`](extensions/)
and are documented in [`docs/editors.md`](docs/editors.md).

## Documentation

Full docs live in [`docs/`](docs/). Runnable examples live in [`examples/`](examples/).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Security issues go through
[GitHub Security Advisories](https://github.com/fabiocicerchia/expiry-radar/security/advisories/new),
never a public issue — see [SECURITY.md](SECURITY.md).

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
