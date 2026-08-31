# Editor integration

The inventory is most useful where you are already looking at the thing that is
about to expire. Both editor integrations live in
[`extensions/`](https://github.com/fabiocicerchia/expiry-radar/tree/main/extensions)
and drive the same binary CI runs — nothing is reimplemented, so the report in
the editor and the report on the build server are the same document.

| | VS Code | Neovim |
| --- | --- | --- |
| Ranked list | panel (`expiry-radar` view) | quickfix list, float |
| Diagnostics on the config file | yes | yes |
| Status | status bar item | `statusline()` component |
| Full HTML report | webview tab | export, then open |
| Export (HTML / iCal / JSON / Prometheus) | yes | yes |
| Probe one host | yes | yes |
| Record an item | **Add Item…** | `:ExpiryRadarAdd` |
| Stop tracking one | right-click a row | `:ExpiryRadarRemove` |
| Environment check | Doctor command | `:checkhealth expiry-radar` |

## From a checkout

One verb installs both, and is the path to use while developing them:

```sh
make ext-install     # package + install the VS Code extension, link the Neovim plugin
make ext-uninstall   # remove both again
```

The Neovim plugin is symlinked onto your packpath rather than copied, so a
`git pull` updates what is installed. The VS Code half is skipped with a note
if the `code` command is not on your `PATH`.

## VS Code

```sh
code --install-extension fabiocicerchia.expiry-radar
```

Then the CLI, if it is not already there:

```sh
go install github.com/fabiocicerchia/expiry-radar/cmd/expiry-radar@latest
```

The extension finds the binary at `./bin/expiry-radar` in the open folder (what
`make build` writes), then on `PATH`, then in `$GOBIN` / `$GOPATH/bin` /
`~/go/bin`. Full settings reference:
[`extensions/vscode/README.md`](https://github.com/fabiocicerchia/expiry-radar/tree/main/extensions/vscode).

## Neovim

Neovim 0.11 or newer. With lazy.nvim:

```lua
{
  'fabiocicerchia/expiry-radar',
  cmd = { 'ExpiryRadar', 'ExpiryRadarReport', 'ExpiryRadarList' },
  config = function()
    require('expiry-radar').setup({})
  end,
}
```

`:ExpiryRadarReport` for the inventory, `:ExpiryRadarList` for the quickfix
list, `:ExpiryRadarProbe` for one host. `:help expiry-radar` is the full
reference; the source is in
[`extensions/nvim/`](https://github.com/fabiocicerchia/expiry-radar/tree/main/extensions/nvim).

## Recording items

Most of the inventory is discovered — you grant read access and the sources
enumerate. Three things are recorded instead, and both editors write them into
`expiry-radar.json` for you rather than leaving you in a JSON editor: an
endpoint to probe, a domain to look up, and a `manual` item for what nothing
can discover at all (see [Sources](sources.md#recorded-or-discovered)).

VS Code: **expiry-radar: Add Item…**, or the `+` in the panel title bar.
Neovim: `:ExpiryRadarAdd`.

Both prompt for the kind first, seed the value from your selection or the host
under the cursor, and validate the date against the same rule the CLI applies —
so the editor cannot write a config the CLI then refuses to load. The file is
opened at the new line afterwards, because a config edited invisibly is one
nobody trusts, and a collection runs immediately so the row appears rather than
waiting for the next refresh to prove the edit worked.

The entry is appended as text, not re-serialised: adding one host gives you a
one-line diff, and your indentation, key order and one-line arrays survive.

### Where to find it

The VS Code panel lives in the **bottom Panel**, next to Terminal and Problems —
not the sidebar. Its title bar carries `+` (add), refresh, grouping, filter and
report; every command is also under **expiry-radar:** in the Command Palette.
Until the first collection lands, the panel itself offers the three things worth
doing from empty.

### Removing one

A row that the config *recorded* can be removed: right-click it in VS Code, or
run `:ExpiryRadarRemove` in Neovim and pick from the list. A row that was
*discovered* offers nothing, because deleting a config line would not delete a
certificate from an Ingress — and implying otherwise would suggest this tool
writes to your estate, which it never does.

Removal is bounded to the array the entry lives in, and refuses outright if the
config has changed since the collection that reported the position — better to
ask for a refresh than to delete the wrong entry.

To *change* an entry, click the row: it opens the config at the line that
recorded it. There is deliberately no edit dialog; you are already in a text
editor, and a form would be a worse one.

## What both of them refuse to do

Two behaviours are shared on purpose, because getting either wrong would make
the integration worse than no integration.

### They never collect on a keystroke, and never on every save

A collection dials every configured host over TLS, queries RDAP for every
domain, and calls the Kubernetes, Vault and AWS APIs. Registries rate-limit
RDAP, and cloud APIs are billed. An editor open all day that re-collected on
every save would spend the afternoon hammering somebody's registrar to
re-discover a date that moves once a year.

So collections are debounced, single-flight, and triggered by:

- saving **the config file** — the only save that changes what is in scope;
- a periodic refresh, hourly by default, skipped while the window is in the
  background and skipped again if a run already landed recently;
- a command.

### They never quietly drop a source that failed

`expiry-radar` returns whatever the other sources managed to read when one
fails, prints `expiry-radar: warning: <source>: <error>` per failure, and exits
3. That is the right CLI behaviour — one broken credential must not hide the
certificate expiring tomorrow — but it means a run can succeed, look clean, and
be missing an entire cloud account.

An inventory that quietly lost a source reads exactly like a clean estate. So
in both editors a failed source is surfaced where it cannot be missed: at the
top of the panel or float, on the status indicator, and in a notification —
never only in the log.

## Where the diagnostics land, and where they don't

Items declared in `expiry-radar.json` get a diagnostic on the line that
declared them: the `host` in `endpoints`, the string in `domains`. Severity
follows the deadline, never the ranking — expired is an error, inside the
warning window is a warning, inside the information window is information, and
beyond that nothing is published. A certificate on the payment path ninety days
out belongs at the top of the panel and is still nothing to interrupt an edit
over.

Everything else — a certificate on an Ingress, a key in IAM, a lease in Vault —
gets none. Nothing in the repository declared it, so there is no honest line to
attach it to, and inventing one would point somebody at a file that has nothing
to do with the thing about to break. Those items live in the panel, the
quickfix list and the report, which is where they belong.
