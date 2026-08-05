# Output formats

## Outputs

Ranked table (CLI) · JSON for CI · Prometheus gauges (`expiry_radar_seconds_left`,
`_blast_radius`, `_priority`, `_items`; only low-cardinality labels) · an iCal
feed of all-day renewal events with alarms scaled by blast radius — 30 days'
notice at ≥0.8, 14 at ≥0.6, 7 otherwise — and stable UIDs so refreshing the feed
updates events instead of duplicating them.

The HTML report (`-format html`) is the one built to be read rather than parsed:
stat tiles across the top, rows grouped under the domain they belong to, and
live filtering by name, kind or deadline. It is a single file with no external
CSS, fonts or scripts, so it survives being mailed as an attachment or dropped
on a static host. Rows are coloured by deadline rather than priority, always
alongside the day count in text.

Domains group without a public-suffix list: the groups are the domains the report
already collected, so a certificate is filed under the registration it depends on.
Anything matching none of them falls back to its namespace, then to a shared
bucket.
