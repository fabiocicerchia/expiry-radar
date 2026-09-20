# Basic Example

What it shows: the two sources that need **no credentials at all** — a live TLS
handshake against each endpoint, and registrar expiry over RDAP for each domain.
Nothing to configure, nothing to grant, no account anywhere.

## Run

```sh
make build                                     # from the repo root
cd examples/basic
../../bin/expiry-radar -config expiry-radar.json
```

Output is a table ranked by priority, highest first — illustrative, since the
real dates move every time the certificates rotate:

```
PRIORITY  BLAST  EXPIRES IN  KIND             NAME                    SOURCE        WHY
0.73      0.70   21d         tls_cert         example.com             tls:endpoint  base tls_cert, internet-facing
0.68      0.70   29d         tls_cert         wikipedia.org           tls:endpoint  base tls_cert, internet-facing
0.41      0.85   214d        domain           example.com             domain:rdap   base domain
0.12      1.00   3431d       intermediate_ca  DigiCert Global Root G2 tls:chain     base intermediate_ca
```

Four things worth noticing:

- **`tls:chain` rows nobody asked for.** Probing an endpoint also reports the
  intermediate CAs it presented, deduplicated across every host. Those are the
  ones that take out an estate rather than a host, which is why they carry a
  higher blast radius on a much longer clock.
- **`WHY` is the ranking, shown.** Priority is never a number you have to trust;
  the column says which signals produced it.
- **The domain and its certificate are separate rows.** A renewed certificate on
  a lapsed registration still goes dark.
- **`PRIORITY` is not `BLAST`.** The root CA above has the highest blast radius
  in the table and the lowest priority, because it expires in nine years.

## Next

```sh
../../bin/expiry-radar -config expiry-radar.json -format html -out report.html
../../bin/expiry-radar -config expiry-radar.json -fail-within 14   # CI gate: exit 1
../../bin/expiry-radar -config expiry-radar.json -only domain:rdap # one source
```

Exit codes: `0` clean · `1` `-fail-within` breached · `2` bad usage or config ·
`3` partial results. Exit 3 means something could not be read — an unreachable
host, or a registry that answered without a date. The failure is printed to
stderr and the rest of the report is still written, because a report that
quietly lost a source reads exactly like a clean estate. A TLD with no RDAP
service is not one of these: it falls back to WHOIS, and those rows come back
with `domain:whois` in the `SOURCE` column.

Then see [`../multi-provider/`](../multi-provider/) for a config with real
providers wired up.
