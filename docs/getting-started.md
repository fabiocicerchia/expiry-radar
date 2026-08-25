# Getting Started

## Install

```sh
go install github.com/fabiocicerchia/expiry-radar/cmd/expiry-radar@latest
```

Or from a checkout:

```sh
make build      # -> ./bin/
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

Exit codes: `0` clean · `1` a `-fail-within` threshold was breached · `2` bad
usage or config · `3` partial results, at least one source failed. A source that
fails still returns what it managed to read, and the failure is printed to
stderr — a report that quietly lost a source reads exactly like a clean estate.

The [README](README.md) covers what expiry-radar does and why.
