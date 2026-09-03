-- Running expiry-radar.
--
-- A collection dials every configured host over TLS, queries RDAP for every
-- domain and calls the Kubernetes, Vault and AWS APIs, so everything here is
-- asynchronous -- vim.system with a callback, and everything that touches the
-- editor inside vim.schedule. When those runs are allowed to happen is
-- init.lua's business; what one does is this file's.

local core = require('expiry-radar.core')
local edit = require('expiry-radar.edit')
local state = require('expiry-radar.state')
local ui = require('expiry-radar.ui')

local M = {}

local function finish(ok, message)
  state.handle = nil
  if not ok and message then
    state.log('collection failed: %s', message)
    state.notify(message, vim.log.levels.ERROR)
  end
end

--- Where in the config file each declared item lives, read fresh per run: the
--- config is a file somebody edits, and a stale map would put a squiggle on the
--- wrong line, which is worse than none at all.
local function declared_in(config_path)
  if config_path == '' then
    return nil
  end
  local ok, text = pcall(function()
    return table.concat(vim.fn.readfile(config_path), '\n')
  end)
  if not ok then
    return nil
  end
  return edit.declared_in(text)
end

--- Tell the caller how it went, when it asked.
local function done(opts, ok)
  if opts.on_done then
    opts.on_done(ok)
  end
end

--- The decoded report, or nil and the reason there is not one.
local function report_from(out)
  -- Exit 0 clean, 1 a -fail-within threshold, 3 partial results: all three
  -- carry a report. Only 2 (bad usage or config) does not.
  local has_report = out.code == 0 or out.code == 1 or out.code == 3
  local stdout = out.stdout or ''
  if not has_report or vim.trim(stdout) == '' then
    local detail = vim.split(vim.trim(out.stderr or ''), '\n')
    return nil, ('expiry-radar failed (exit %s): %s'):format(
      tostring(out.code),
      detail[#detail] or 'no output'
    )
  end
  local decoded_ok, report = pcall(vim.json.decode, stdout)
  if not decoded_ok or type(report) ~= 'table' or type(report.items) ~= 'table' then
    return nil, 'expiry-radar wrote a report that could not be read'
  end
  return report
end

local warned_at = 0

--- A source that failed is not a log line. The whole product is a complete
--- inventory ranked by consequence, and a run that lost a source produces one
--- that looks exactly like a clean estate.
local function announce_warnings(warnings, manual)
  if #warnings == 0 then
    return
  end
  for _, warning in ipairs(warnings) do
    state.log('source failed: %s', warning)
  end
  local now = os.time()
  if not manual and now - warned_at < 60 then
    return
  end
  warned_at = now
  state.notify(
    string.format(
      '%d source(s) failed — this inventory is incomplete. :ExpiryRadarLog for the details.',
      #warnings
    ),
    vim.log.levels.WARN
  )
end

--- What one finished run leaves behind: a snapshot, or a reason there is none.
local function collected(out, opts, config_path)
  local warnings = core.parse_warnings(out.stderr)
  local report, why = report_from(out)
  if not report then
    finish(false, why)
    return done(opts, false)
  end
  state.snapshot = {
    items = core.normalize(report, state.cfg, declared_in(config_path)),
    warnings = warnings,
    generated_at = report.generatedAt,
    config_path = config_path,
    at = os.time(),
  }
  state.log('done: %d item(s), %d source(s) failed', #state.snapshot.items, #warnings)
  finish(true)
  announce_warnings(warnings, opts.manual)
  ui.publish(state.snapshot.items, state.cfg, config_path)
  done(opts, true)
end

--- Run one collection. Never blocks: vim.system with a callback, and everything
--- that touches the editor inside vim.schedule.
---@param opts table { manual?, reason?, on_done? }
function M.collect(opts)
  opts = opts or {}
  if not state.cfg.enabled then
    return
  end
  if state.handle then
    if not opts.manual then
      return -- an automatic collection never queues behind another
    end
    state.log('preempting the running collection')
    M.cancel({ quiet = true })
  end

  local root = state.root()
  if not core.has_sources(root, state.cfg) then
    -- The CLI would exit 2 saying exactly this. An automatic run stays quiet;
    -- a command says it with the thing that fixes it attached.
    state.log('no sources configured in %s — not collecting', root)
    if opts.manual then
      state.notify(
        'no sources configured — every source is opt-in. :ExpiryRadarConfig creates one.',
        vim.log.levels.WARN
      )
    end
    return done(opts, false)
  end

  local cmd, why = core.resolve_cmd(root, state.cfg)
  if not cmd then
    finish(false, ('%s — install it with `%s`, or set cmd'):format(why, core.INSTALL_COMMAND))
    return done(opts, false)
  end

  local config_path = core.resolve_config(root, state.cfg)
  local argv = core.argv(state.cfg, { format = 'json', config_path = config_path })
  local full = vim.list_extend(vim.list_slice(cmd, 1, #cmd), argv)
  state.log('collect (%s): %s', opts.reason or 'command', table.concat(full, ' '))

  local ok, started = pcall(vim.system, full, {
    text = true,
    cwd = root,
    -- A few seconds past the CLI's own budget, so its timeout wins and we get
    -- its partial results instead of killing it a moment before it reports them.
    timeout = state.cfg.collect.timeout_ms + 10000,
  }, function(out)
    vim.schedule(function()
      collected(out, opts, config_path)
    end)
  end)

  if not ok then
    state.log('could not run %s: %s', full[1], tostring(started))
    finish(false, ('could not run `%s` — see :checkhealth expiry-radar'):format(full[1]))
    return done(opts, false)
  end
  state.handle = started
end

function M.cancel(opts)
  opts = opts or {}
  if not state.handle then
    if not opts.quiet then
      state.notify('no collection is running.')
    end
    return
  end
  pcall(function()
    state.handle:kill('sigterm')
  end)
  finish(true)
  if not opts.quiet then
    state.notify('collection cancelled.')
  end
end

function M.ensure_collected(cb)
  if state.snapshot then
    return cb()
  end
  state.notify('collecting…')
  M.collect({
    manual = true,
    reason = 'command',
    on_done = function(ok)
      if ok then
        cb()
      end
    end,
  })
end

--- The inventory, in a float.

function M.probe(host)
  host = vim.trim(host or '')
  if host == '' then
    host = core.host_at_cursor(vim.api.nvim_get_current_line())
  end
  if host == '' then
    return vim.ui.input({ prompt = 'Probe host: ' }, function(answer)
      if answer and vim.trim(answer) ~= '' then
        M.probe(answer)
      end
    end)
  end

  local root = state.root()
  local cmd, why = core.resolve_cmd(root, state.cfg)
  if not cmd then
    return state.notify(why, vim.log.levels.ERROR)
  end
  -- Both sources, because "when does this expire" about a hostname means the
  -- certificate *and* the registration, and only one of them is usually the one
  -- about to bite.
  local argv = core.argv(state.cfg, {
    format = 'json',
    ignore_config = true,
    endpoints = { host },
    domains = { (host:gsub(':%d+$', '')) },
  })
  state.notify('probing ' .. host .. '…')
  vim.system(
    vim.list_extend(vim.list_slice(cmd, 1, #cmd), argv),
    { text = true, cwd = root, timeout = state.cfg.collect.timeout_ms + 10000 },
    function(out)
      vim.schedule(function()
        local warnings = core.parse_warnings(out.stderr)
        local decoded_ok, report = pcall(vim.json.decode, out.stdout or '')
        if not decoded_ok or type(report) ~= 'table' then
          return state.notify(('probe failed (exit %s)'):format(tostring(out.code)), vim.log.levels.ERROR)
        end
        local items = core.normalize(report, state.cfg, nil)
        ui.float(ui.hover_lines(host, items, { warnings = warnings }), {
          title = ' ' .. host .. ' ',
          filetype = 'expiry-radar',
        })
      end)
    end
  )
end

--- One rendered format, on disk. Binary, so the bytes on disk are the bytes
--- the CLI wrote: the iCal feed is CRLF per RFC 5545, and a line-mode write
--- would append a newline the document does not have.
local function exported(out, target)
  announce_warnings(core.parse_warnings(out.stderr), true)
  if out.code == 2 or vim.trim(out.stdout or '') == '' then
    return state.notify(('export failed (exit %s)'):format(tostring(out.code)), vim.log.levels.ERROR)
  end
  local ok, err = pcall(vim.fn.writefile, vim.split(out.stdout, '\n'), target, 'b')
  if not ok then
    return state.notify('could not write ' .. target .. ': ' .. tostring(err), vim.log.levels.ERROR)
  end
  state.notify('exported to ' .. target)
end

--- Render one format and write it somewhere. The CLI produces one format per
--- invocation, so this is its own collection, which is why it is on demand.

function M.export(format, path)
  local formats = { 'html', 'ical', 'json', 'prometheus' }
  if not format or format == '' then
    return vim.ui.select(formats, { prompt = 'Export as' }, function(chosen)
      if chosen then
        M.export(chosen, path)
      end
    end)
  end
  if not vim.tbl_contains(formats, format) then
    return state.notify(('unknown format %q (want one of: %s)'):format(format, table.concat(formats, ', ')), vim.log.levels.ERROR)
  end

  local root = state.root()
  local cmd, why = core.resolve_cmd(root, state.cfg)
  if not cmd then
    return state.notify(why, vim.log.levels.ERROR)
  end
  local extension = ({ html = 'html', ical = 'ics', json = 'json', prometheus = 'prom' })[format]
  local target = path
  if not target or target == '' then
    target = vim.fs.joinpath(root, ('expiry-radar-%s.%s'):format(os.date('%Y-%m-%d'), extension))
  end

  local argv = core.argv(state.cfg, { format = format, config_path = core.resolve_config(root, state.cfg) })
  state.notify('rendering ' .. format .. '…')
  vim.system(
    vim.list_extend(vim.list_slice(cmd, 1, #cmd), argv),
    { text = true, cwd = root, timeout = state.cfg.collect.timeout_ms + 10000 },
    function(out)
      vim.schedule(function()
        exported(out, target)
      end)
    end
  )
end

--- Record something.
---
--- Two of the six kinds of item are recorded rather than discovered -- a host
--- to probe and a domain to look up -- and the third option is for what nothing
--- can find at all: a registrar with no RDAP, a credential rotated by hand, a
--- code-signing certificate on somebody's laptop.

return M
