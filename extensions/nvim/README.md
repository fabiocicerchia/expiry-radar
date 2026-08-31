# expiry-radar.nvim

One inventory of everything that expires — TLS certificates, the intermediate
CAs nobody tracks, secrets, IAM keys, Vault leases, domains — **ranked by blast
radius**, in Neovim.

This is a thin plugin over the [`expiry-radar`](../../) CLI: it runs the same
binary CI runs, and puts the answers where you already look — the quickfix
list, diagnostics on the config file, a float, and one string for the
statusline.

## Requirements

- Neovim 0.11+ (`vim.system`, `vim.fs.root`, `vim.validate`).
- The `expiry-radar` binary:

  ```sh
  go install github.com/fabiocicerchia/expiry-radar/cmd/expiry-radar@latest
  ```

  Or `make build` in a checkout, which writes `./bin/expiry-radar`.

The plugin finds it on its own: `./bin/expiry-radar` in the project, then
`expiry-radar` on `$PATH`, then `$GOBIN` / `$GOPATH/bin` / `~/go/bin`.

## Install

From a checkout, `make ext-install` at the repository root symlinks this plugin
onto your packpath (`~/.local/share/nvim/site/pack/expiry-radar/start/`) and
generates its helptags — no plugin manager involved, and `git pull` updates it.
`make ext-uninstall` removes it.

Otherwise, with a plugin manager — `setup()` is optional either way, since a
command used before it runs gets the defaults.

**lazy.nvim**

```lua
{
  'fabiocicerchia/expiry-radar',
  cmd = { 'ExpiryRadar', 'ExpiryRadarReport', 'ExpiryRadarList' },
  config = function()
    require('expiry-radar').setup({})
  end,
}
```

**packer**

```lua
use({ 'fabiocicerchia/expiry-radar', config = function() require('expiry-radar').setup({}) end })
```

Sources are opt-in — nothing runs implicitly. `:ExpiryRadarConfig` creates an
`expiry-radar.json` from the repository's own example, or set `endpoints` and
`domains` in `setup()` for a project with no config file.

Most of the inventory is *discovered*; `:ExpiryRadarAdd` *records* the rest — an
endpoint to probe, a domain to look up, or a `manual` item for what nothing can
discover at all (a registrar with no RDAP, a credential rotated by hand). It
writes the config for you, validates the date against the CLI's own rule, and
collects straight away.

## Commands

| Command | What it does |
| --- | --- |
| `:ExpiryRadar` | Collect now |
| `:ExpiryRadarAdd` | Record an endpoint, a domain, or something nothing can discover |
| `:ExpiryRadarRemove` | Stop tracking a recorded item |
| `:ExpiryRadarReport` | The inventory in a float, grouped by deadline |
| `:ExpiryRadarList` | Every item in the quickfix list |
| `:ExpiryRadarFilter` | One deadline window or kind → quickfix |
| `:ExpiryRadarHover` | What the inventory knows about the host under the cursor |
| `:ExpiryRadarProbe [host]` | Probe one host now, ignoring the config |
| `:ExpiryRadarExport [format] [path]` | Render `html` / `ical` / `json` / `prometheus` to a file |
| `:ExpiryRadarConfig` | Open (or create) the config file |
| `:ExpiryRadarCancel` | Cancel the running collection |
| `:ExpiryRadarLog` | What ran, and every source that failed |

`:checkhealth expiry-radar` answers the question a collection cannot: why it
produced nothing at all.

## Two decisions worth knowing about

**It never collects on a keystroke, and never on every save.** A collection
dials every configured host over TLS, queries RDAP for every domain and calls
the Kubernetes, Vault and AWS APIs. Registries rate-limit RDAP, and expiry
dates move on the scale of days. Only saving the *config file* triggers a
collection; everything else is the periodic refresh, or a command.

**It never quietly drops a source that failed.** The CLI reports what the
other sources managed to read and prints one warning per failure, so a run can
succeed, look clean, and be missing an entire cloud account. Those failures
are on the float, in the statusline as `(incomplete)`, and in a notification —
never only in the log.

## Configuration

```lua
require('expiry-radar').setup({
  enabled = true,
  cmd = {},              -- empty auto-detects
  config_path = '',      -- empty uses <root>/expiry-radar.json
  endpoints = {},        -- extra hosts, on top of the config file
  domains = {},          -- extra domains, on top of the config file
  extra_args = {},

  collect = {
    -- 'on_config_save' | 'on_config_save_and_interval' | 'interval' | 'manual'
    trigger = 'on_config_save_and_interval',
    debounce_ms = 1500,
    interval_minutes = 60,
    on_startup = true,
    timeout_ms = 120000,
    within_days = 0,     -- 0 is everything, which is the point of an inventory
    min_priority = 0,
  },

  diagnostics = {
    enabled = true,
    warn_within_days = 14,   -- WARN inside this window; expired is always ERROR
    info_within_days = 30,   -- INFO inside this one; nothing beyond it
  },

  status = { warn_within_days = 14 },
})
```

Diagnostics land on the config file only, on the line that declared the host
or the domain. A certificate found on an Ingress was never written down in the
repository, so there is no honest line to squiggle — those live in the list.

## Statusline

```lua
sections = { lualine_x = { require('expiry-radar').statusline } }
```

The soonest deadline, not the highest priority: one string in the corner is a
clock, and a clock showing the second-soonest deadline would be wrong in
exactly the case it matters.

## Development

```sh
make test     # the specs, headless, exactly as CI runs them
make lint     # check every file parses
```

`make ext-test` at the repository root runs these plus the smoke test below and
the whole VS Code side.

`tests/smoke.lua` drives the plugin against the real binary — the JSON shape,
the warning framing, the exit codes — and needs no network:

```sh
make -C ../.. build
nvim --headless --clean -u tests/smoke.lua
```

See `:help expiry-radar` for the full reference.
