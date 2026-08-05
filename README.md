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

## Install

```sh
go install github.com/fabiocicerchia/expiry-radar/cmd/expiry-radar@latest
```

Or from a checkout:

```sh
make build      # -> ./bin/
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

Exit codes: `0` clean · `1` a `-fail-within` threshold was breached · `2` bad
usage or config · `3` partial results, at least one source failed. A source that
fails still returns what it managed to read, and the failure is printed to
stderr — a report that quietly lost a source reads exactly like a clean estate.

## Documentation

Full docs live in [`docs/`](docs/). Runnable examples live in [`examples/`](examples/).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Security issues go through
[GitHub Security Advisories](https://github.com/fabiocicerchia/expiry-radar/security/advisories/new),
never a public issue — see [SECURITY.md](SECURITY.md).

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
