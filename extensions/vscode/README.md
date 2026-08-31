# expiry-radar for VS Code

One inventory of everything that expires — TLS certificates, the intermediate
CAs nobody tracks, secrets, IAM keys, Vault leases, domains — **ranked by blast
radius**, in the editor.

Any script can list expiry dates. The value is ordering them by consequence, so
the certificate on the payment path outranks the one on a staging dashboard.
This extension runs the same [`expiry-radar`](https://github.com/fabiocicerchia/expiry-radar)
binary CI runs and shows the same report, in a panel, on the status bar, and as
squiggles on the lines of `expiry-radar.json` that declared what is about to
break.

## Install

```sh
code --install-extension fabiocicerchia.expiry-radar
```

From a checkout, `make ext-install` at the repository root packages this
extension and installs the .vsix in one step.

Then get the CLI:

```sh
go install github.com/fabiocicerchia/expiry-radar/cmd/expiry-radar@latest
```

Or `make build` in a checkout, which writes `./bin/expiry-radar`. The extension
finds it on its own: `./bin/expiry-radar` in the open folder, then
`expiry-radar` on `PATH`, then `$GOBIN` / `$GOPATH/bin` / `~/go/bin`. Point
`expiryRadar.path` at it when it lives somewhere else.

Sources are opt-in — nothing runs implicitly. **expiry-radar: Open Config
File** creates an `expiry-radar.json` from the repository's own example, or set
`expiryRadar.endpoints` and `expiryRadar.domains` for a folder with no config.

## What you get

- **A ranked panel.** One list in blast-radius order by default, or grouped by
  kind when the question is "what certificates do we have". Filter by deadline
  and by kind. Clicking a row opens the line that declared it.
- **Squiggles where you edit.** Every host in `endpoints` and every string in
  `domains` gets a diagnostic when it is close to expiring, with the reason its
  blast radius came out where it did — a ranking nobody can explain gets
  ignored.
- **The status bar.** The soonest deadline, amber inside your warning window,
  red once anything has expired.
- **The full report.** The CLI's self-contained HTML report, in an editor tab,
  filters and all — the same document CI publishes.
- **Export.** HTML, the iCal renewal feed (with alarm lead times set by blast
  radius), JSON for CI, or a Prometheus scrape body.
- **Probe a host.** One hostname, right now, ignoring the config: its
  certificate, its chain and its registration.
- **Record what nothing can discover.** Most of the inventory is discovered, but
  a registrar with no RDAP, a credential rotated by hand or a code-signing
  certificate on somebody's laptop has to be written down. **Add Item…** writes
  it into the config for you, validates the date against the CLI's own rule, and
  collects straight away.

**The panel is in the bottom Panel**, next to Terminal and Problems — not the
sidebar. `+` in its title bar adds an item; right-clicking a recorded row
removes it; clicking one opens the config at the line that recorded it. Every
command is also under **expiry-radar:** in the Command Palette.

## Commands

| Command | What it does |
| --- | --- |
| expiry-radar: Refresh Inventory | Collect now |
| expiry-radar: Add Item… | Record an endpoint, a domain, or something nothing can discover |
| expiry-radar: Stop Tracking This Item | Remove a recorded entry (right-click a row) |
| expiry-radar: Open Report | The HTML report in a tab |
| expiry-radar: Export Report… | Render HTML / iCal / JSON / Prometheus to a file |
| expiry-radar: Probe a Host… | One host, ignoring the config |
| expiry-radar: Filter Items | By deadline and by kind |
| expiry-radar: Open Config File | Open, or create from the example |
| expiry-radar: Check Environment (Doctor) | Why a run came back empty |
| expiry-radar: Cancel Running Collection | |
| expiry-radar: Show Log | What ran, and every source that failed |

## Two decisions worth knowing about

**It never collects on a keystroke, and never on every save.** A collection
dials every configured host over TLS, queries RDAP for every domain and calls
the Kubernetes, Vault and AWS APIs. Registries rate-limit RDAP, and expiry
dates move on the scale of days. Only saving the *config file* triggers one;
otherwise it is the periodic refresh (hourly by default, skipped while the
window is in the background) or a command.

**It never quietly drops a source that failed.** The CLI reports whatever the
other sources managed to read and prints one warning per failure, so a run can
succeed, look clean, and be missing an entire cloud account. Those failures are
at the top of the panel, on the status bar tooltip, and in a notification —
never only in the log.

## Settings

| Setting | Default | |
| --- | --- | --- |
| `expiryRadar.path` | `""` | The binary. Empty auto-detects. |
| `expiryRadar.configPath` | `""` | `-config`. Empty uses `expiry-radar.json`. |
| `expiryRadar.endpoints` | `[]` | Extra hosts, on top of the config file. |
| `expiryRadar.domains` | `[]` | Extra domains, on top of the config file. |
| `expiryRadar.extraArgs` | `[]` | Appended to every invocation. |
| `expiryRadar.scan.trigger` | `onConfigSaveAndInterval` | `onConfigSave`, `interval`, `manual`. |
| `expiryRadar.scan.onStartup` | `true` | One collection when the window opens. |
| `expiryRadar.scan.intervalMinutes` | `60` | Period of the background refresh. |
| `expiryRadar.scan.timeoutSeconds` | `120` | Collection budget. |
| `expiryRadar.diagnostics.enabled` | `true` | |
| `expiryRadar.diagnostics.warnWithinDays` | `14` | Warning inside this window; expired is always an error. |
| `expiryRadar.diagnostics.infoWithinDays` | `30` | Information inside this one; nothing beyond it. |
| `expiryRadar.view.withinDays` | `0` | `-within`. `0` is everything. |
| `expiryRadar.view.minPriority` | `0` | `-min-priority`, 0..1. |
| `expiryRadar.status.warnWithinDays` | `14` | When the status bar turns amber. |

`view.withinDays` and `view.minPriority` are passed to the CLI rather than
applied to the panel afterwards, so an export contains exactly what the panel
shows rather than a wider set.

## Development

`make ext-test` at the repository root runs everything below, plus the Neovim
side. Directly:

```sh
npm install
npm run typecheck
npm test        # unit tests, plus contract tests against a real ./bin/expiry-radar
npm run build   # bundle into dist/
npm run package # build a .vsix
```

The contract tests run the real binary against a closed port on loopback, so
they need `make build` at the repository root but no network. Without a binary
they skip rather than fail.

## License

Apache-2.0. See [LICENSE](LICENSE).
