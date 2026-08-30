-- setup(), the public API, and the collection policy.
--
-- A collection dials every configured host over TLS, queries RDAP for every
-- domain and calls the Kubernetes, Vault and AWS APIs. Registries rate-limit
-- RDAP, and an editor left open all day would happily spend the afternoon
-- hammering them. So every trigger funnels through here and obeys the rules the
-- VS Code extension settled on: debounce a burst of config saves into one run,
-- exactly one process at a time, and a manual run preempts an automatic one.

local config = require('expiry-radar.config')
local core = require('expiry-radar.core')
local ui = require('expiry-radar.ui')

local M = {}

local cfg = nil
--- The last completed collection.
local snapshot = nil
local handle = nil
local debounce_timer = nil
local sweep_timer = nil
local log_lines = {}

local function log(fmt, ...)
  log_lines[#log_lines + 1] = string.format('[%s] ' .. fmt, os.date('%H:%M:%S'), ...)
  if #log_lines > 500 then
    table.remove(log_lines, 1)
  end
end

local function notify(msg, level)
  vim.notify('expiry-radar: ' .. msg, level or vim.log.levels.INFO)
end

function M.is_setup()
  return cfg ~= nil
end

function M.config()
  return cfg
end

function M.log_text()
  return log_lines
end

function M.root()
  return vim.fs.root(0, { 'expiry-radar.json', 'go.mod', '.git' }) or vim.uv.cwd()
end

function M.snapshot()
  return snapshot
end

function M.items()
  return snapshot and snapshot.items or {}
end

function M.is_collecting()
  return handle ~= nil
end

--- A lualine component, or anything else that wants one string.
---
--- The soonest deadline, not the highest priority. Priority is the right order
--- for a list you are working through; one string in the corner is a clock, and
--- a clock showing the second-soonest deadline would be wrong in exactly the
--- case it matters.
function M.statusline()
  if not cfg then
    return ''
  end
  if handle then
    return 'radar collecting'
  end
  if not snapshot then
    return ''
  end
  local soonest, expired = nil, 0
  for _, item in ipairs(snapshot.items) do
    if item.daysLeft < 0 then
      expired = expired + 1
    end
    if not soonest or item.daysLeft < soonest.daysLeft then
      soonest = item
    end
  end
  -- One word for one condition: a run that lost a source is "incomplete"
  -- whether or not anything came back, because an empty inventory from a run
  -- that could not look is the failure this tool exists to prevent.
  local incomplete = #snapshot.warnings > 0 and ' (incomplete)' or ''
  if not soonest then
    return string.format('%s radar %s%s', incomplete ~= '' and '!' or '·', '0', incomplete)
  end
  local mark = expired > 0 and '✗' or (soonest.daysLeft <= cfg.status.warn_within_days and '!' or '·')
  return string.format('%s radar %s%s', mark, core.human_days(soonest.daysLeft), incomplete)
end

-- --- running expiry-radar -----------------------------------------------------

local function finish(ok, message)
  handle = nil
  if not ok and message then
    log('collection failed: %s', message)
    notify(message, vim.log.levels.ERROR)
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
  return core.declared_in(text)
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
    log('source failed: %s', warning)
  end
  local now = os.time()
  if not manual and now - warned_at < 60 then
    return
  end
  warned_at = now
  notify(
    string.format(
      '%d source(s) failed — this inventory is incomplete. :ExpiryRadarLog for the details.',
      #warnings
    ),
    vim.log.levels.WARN
  )
end

--- Run one collection. Never blocks: vim.system with a callback, and everything
--- that touches the editor inside vim.schedule.
---@param opts table { manual?, reason?, on_done? }
function M.collect(opts)
  opts = opts or {}
  if not cfg.enabled then
    return
  end
  if handle then
    if not opts.manual then
      return -- an automatic collection never queues behind another
    end
    log('preempting the running collection')
    M.cancel({ quiet = true })
  end

  local root = M.root()
  if not core.has_sources(root, cfg) then
    -- The CLI would exit 2 saying exactly this. An automatic run stays quiet;
    -- a command says it with the thing that fixes it attached.
    log('no sources configured in %s — not collecting', root)
    if opts.manual then
      notify(
        'no sources configured — every source is opt-in. :ExpiryRadarConfig creates one.',
        vim.log.levels.WARN
      )
    end
    if opts.on_done then
      opts.on_done(false)
    end
    return
  end

  local cmd, why = core.resolve_cmd(root, cfg)
  if not cmd then
    finish(false, ('%s — install it with `%s`, or set cmd'):format(why, core.INSTALL_COMMAND))
    if opts.on_done then
      opts.on_done(false)
    end
    return
  end

  local config_path = core.resolve_config(root, cfg)
  local argv = core.argv(cfg, { format = 'json', config_path = config_path })
  local full = vim.list_extend(vim.list_slice(cmd, 1, #cmd), argv)
  log('collect (%s): %s', opts.reason or 'command', table.concat(full, ' '))

  local ok, started = pcall(vim.system, full, {
    text = true,
    cwd = root,
    -- A few seconds past the CLI's own budget, so its timeout wins and we get
    -- its partial results instead of killing it a moment before it reports them.
    timeout = cfg.collect.timeout_ms + 10000,
  }, function(out)
    vim.schedule(function()
      local warnings = core.parse_warnings(out.stderr)
      -- Exit 0 clean, 1 a -fail-within threshold, 3 partial results: all three
      -- carry a report. Only 2 (bad usage or config) does not.
      local has_report = out.code == 0 or out.code == 1 or out.code == 3
      local stdout = out.stdout or ''
      if not has_report or vim.trim(stdout) == '' then
        local detail = vim.split(vim.trim(out.stderr or ''), '\n')
        finish(false, ('expiry-radar failed (exit %s): %s'):format(
          tostring(out.code),
          detail[#detail] or 'no output'
        ))
        if opts.on_done then
          opts.on_done(false)
        end
        return
      end

      local decoded_ok, report = pcall(vim.json.decode, stdout)
      if not decoded_ok or type(report) ~= 'table' or type(report.items) ~= 'table' then
        finish(false, 'expiry-radar wrote a report that could not be read')
        if opts.on_done then
          opts.on_done(false)
        end
        return
      end

      snapshot = {
        items = core.normalize(report, cfg, declared_in(config_path)),
        warnings = warnings,
        generated_at = report.generatedAt,
        config_path = config_path,
        at = os.time(),
      }
      log('done: %d item(s), %d source(s) failed', #snapshot.items, #warnings)
      finish(true)
      announce_warnings(warnings, opts.manual)
      ui.publish(snapshot.items, cfg, config_path)
      if opts.on_done then
        opts.on_done(true)
      end
    end)
  end)

  if not ok then
    log('could not run %s: %s', full[1], tostring(started))
    finish(false, ('could not run `%s` — see :checkhealth expiry-radar'):format(full[1]))
    if opts.on_done then
      opts.on_done(false)
    end
    return
  end
  handle = started
end

function M.cancel(opts)
  opts = opts or {}
  if not handle then
    if not opts.quiet then
      notify('no collection is running.')
    end
    return
  end
  pcall(function()
    handle:kill('sigterm')
  end)
  finish(true)
  if not opts.quiet then
    notify('collection cancelled.')
  end
end

-- --- triggers -----------------------------------------------------------------

local function stop_timer(timer)
  if timer then
    timer:stop()
    timer:close()
  end
  return nil
end

local function schedule_collect(opts)
  debounce_timer = stop_timer(debounce_timer)
  debounce_timer = vim.uv.new_timer()
  debounce_timer:start(cfg.collect.debounce_ms, 0, function()
    debounce_timer = stop_timer(debounce_timer)
    vim.schedule(function()
      M.collect(opts)
    end)
  end)
end

local function arm_sweep()
  sweep_timer = stop_timer(sweep_timer)
  local trigger = cfg.collect.trigger
  if trigger ~= 'interval' and trigger ~= 'on_config_save_and_interval' then
    return
  end
  local period = cfg.collect.interval_minutes * 60000
  sweep_timer = vim.uv.new_timer()
  sweep_timer:start(period, period, function()
    vim.schedule(function()
      M.collect({ reason = 'periodic refresh' })
    end)
  end)
end

local function attach_autocmds()
  local group = vim.api.nvim_create_augroup('expiry-radar', { clear = true })

  vim.api.nvim_create_autocmd('BufWritePost', {
    group = group,
    callback = function(event)
      local trigger = cfg.collect.trigger
      if not cfg.enabled or (trigger ~= 'on_config_save' and trigger ~= 'on_config_save_and_interval') then
        return
      end
      -- Only the config file. Every other save has nothing to do with what the
      -- estate has expiring, and re-dialling every host because somebody saved
      -- a README would be indefensible.
      local config_path = core.resolve_config(M.root(), cfg)
      if config_path == '' or vim.fs.normalize(event.match) ~= vim.fs.normalize(config_path) then
        return
      end
      schedule_collect({ reason = 'config saved' })
    end,
  })

  -- The config opened after a collection still gets its squiggles.
  vim.api.nvim_create_autocmd('BufReadPost', {
    group = group,
    callback = function()
      if snapshot then
        vim.schedule(function()
          ui.publish(snapshot.items, cfg, snapshot.config_path)
        end)
      end
    end,
  })
end

-- --- commands ------------------------------------------------------------------

local function ensure_collected(cb)
  if snapshot then
    return cb()
  end
  notify('collecting…')
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
function M.report()
  ensure_collected(function()
    ui.float(ui.report_lines(snapshot), { title = ' expiry-radar ', filetype = 'expiry-radar' })
  end)
end

--- Everything, in the quickfix list.
function M.list()
  ensure_collected(function()
    if #snapshot.items == 0 then
      return notify('nothing expiring — or no sources were enabled.')
    end
    ui.to_quickfix(snapshot.items, snapshot.config_path, 'expiry-radar')
    vim.cmd('copen')
  end)
end

--- Pick a deadline window or a kind; the matching items go to the quickfix list.
function M.filter()
  ensure_collected(function()
    local counts, kinds = {}, {}
    for _, item in ipairs(snapshot.items) do
      counts[item.severity] = (counts[item.severity] or 0) + 1
      kinds[item.kind] = (kinds[item.kind] or 0) + 1
    end

    local choices = {}
    for _, severity in ipairs(core.SEVERITIES) do
      if (counts[severity] or 0) > 0 then
        choices[#choices + 1] = { kind = 'severity', key = severity, count = counts[severity] }
      end
    end
    for kind, count in pairs(kinds) do
      choices[#choices + 1] = { kind = 'kind', key = kind, count = count }
    end
    if #choices == 0 then
      return notify('nothing to filter.')
    end

    vim.ui.select(choices, {
      prompt = 'Show which items',
      format_item = function(choice)
        local label = choice.kind == 'severity' and core.SEVERITY_LABEL[choice.key]
          or core.kind_label(choice.key)
        return string.format('%-9s %-20s %d', choice.kind, label, choice.count)
      end,
    }, function(choice)
      if not choice then
        return
      end
      local rows = {}
      for _, item in ipairs(snapshot.items) do
        if item[choice.kind == 'severity' and 'severity' or 'kind'] == choice.key then
          rows[#rows + 1] = item
        end
      end
      ui.to_quickfix(rows, snapshot.config_path, 'expiry-radar: ' .. choice.key)
      vim.cmd('copen')
    end)
  end)
end

--- What the inventory already knows about the host under the cursor.
function M.hover()
  local needle = core.host_at_cursor(vim.api.nvim_get_current_line())
  if needle == '' then
    return notify('no host or domain on this line.')
  end
  ensure_collected(function()
    ui.float(ui.hover_lines(needle, core.items_matching(snapshot.items, needle), snapshot), {
      title = ' ' .. needle .. ' ',
      filetype = 'expiry-radar',
    })
  end)
end

--- One host, right now, without touching the config. The answer somebody
--- actually wants when they are looking at a hostname and wondering how long
--- its certificate has left.
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

  local root = M.root()
  local cmd, why = core.resolve_cmd(root, cfg)
  if not cmd then
    return notify(why, vim.log.levels.ERROR)
  end
  -- Both sources, because "when does this expire" about a hostname means the
  -- certificate *and* the registration, and only one of them is usually the one
  -- about to bite.
  local argv = core.argv(cfg, {
    format = 'json',
    ignore_config = true,
    endpoints = { host },
    domains = { (host:gsub(':%d+$', '')) },
  })
  notify('probing ' .. host .. '…')
  vim.system(
    vim.list_extend(vim.list_slice(cmd, 1, #cmd), argv),
    { text = true, cwd = root, timeout = cfg.collect.timeout_ms + 10000 },
    function(out)
      vim.schedule(function()
        local warnings = core.parse_warnings(out.stderr)
        local decoded_ok, report = pcall(vim.json.decode, out.stdout or '')
        if not decoded_ok or type(report) ~= 'table' then
          return notify(('probe failed (exit %s)'):format(tostring(out.code)), vim.log.levels.ERROR)
        end
        local items = core.normalize(report, cfg, nil)
        ui.float(ui.hover_lines(host, items, { warnings = warnings }), {
          title = ' ' .. host .. ' ',
          filetype = 'expiry-radar',
        })
      end)
    end
  )
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
    return notify(('unknown format %q (want one of: %s)'):format(format, table.concat(formats, ', ')), vim.log.levels.ERROR)
  end

  local root = M.root()
  local cmd, why = core.resolve_cmd(root, cfg)
  if not cmd then
    return notify(why, vim.log.levels.ERROR)
  end
  local extension = ({ html = 'html', ical = 'ics', json = 'json', prometheus = 'prom' })[format]
  local target = path
  if not target or target == '' then
    target = vim.fs.joinpath(root, ('expiry-radar-%s.%s'):format(os.date('%Y-%m-%d'), extension))
  end

  local argv = core.argv(cfg, { format = format, config_path = core.resolve_config(root, cfg) })
  notify('rendering ' .. format .. '…')
  vim.system(
    vim.list_extend(vim.list_slice(cmd, 1, #cmd), argv),
    { text = true, cwd = root, timeout = cfg.collect.timeout_ms + 10000 },
    function(out)
      vim.schedule(function()
        announce_warnings(core.parse_warnings(out.stderr), true)
        if out.code == 2 or vim.trim(out.stdout or '') == '' then
          return notify(('export failed (exit %s)'):format(tostring(out.code)), vim.log.levels.ERROR)
        end
        -- Binary, so the bytes on disk are the bytes the CLI wrote: the iCal
        -- feed is CRLF per RFC 5545, and a line-mode write would append a
        -- newline the document does not have.
        local ok, err = pcall(vim.fn.writefile, vim.split(out.stdout, '\n'), target, 'b')
        if not ok then
          return notify('could not write ' .. target .. ': ' .. tostring(err), vim.log.levels.ERROR)
        end
        notify('exported to ' .. target)
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
function M.add_item()
  local choices = {
    { entry = 'endpoint', label = 'Endpoint', hint = 'a host to probe over TLS, chain included' },
    { entry = 'domain', label = 'Domain', hint = 'a registration to check via RDAP' },
    { entry = 'manual', label = 'Something nothing can discover', hint = 'a date you know' },
  }
  vim.ui.select(choices, {
    prompt = 'Record what?',
    format_item = function(choice)
      return string.format('%-32s %s', choice.label, choice.hint)
    end,
  }, function(choice)
    if not choice then
      return
    end
    if choice.entry == 'manual' then
      return M.prompt_manual(function(entry)
        M.write_entry(choice.entry, entry)
      end)
    end
    local seeded = core.host_at_cursor(vim.api.nvim_get_current_line())
    vim.ui.input({
      prompt = choice.entry == 'endpoint' and 'Host to probe: ' or 'Domain: ',
      default = seeded ~= '' and seeded or nil,
    }, function(value)
      if value and vim.trim(value) ~= '' then
        M.write_entry(choice.entry, value)
      end
    end)
  end)
end

--- Name, then kind, then date. The kind is not cosmetic: it picks the base
--- blast radius, which is what decides where this lands in the ranking.
function M.prompt_manual(done)
  local seeded = core.host_at_cursor(vim.api.nvim_get_current_line())
  vim.ui.input({ prompt = 'What is it? ', default = seeded ~= '' and seeded or nil }, function(name)
    if not name or vim.trim(name) == '' then
      return
    end
    vim.ui.select(core.MANUAL_KINDS, {
      prompt = 'What kind? (this sets its base blast radius)',
      format_item = function(k)
        return string.format('%-18s %s', k.label, k.hint)
      end,
    }, function(kind)
      if not kind then
        return
      end
      vim.ui.input({ prompt = 'Expires (YYYY-MM-DD): ' }, function(expires)
        if not expires then
          return
        end
        local bad = core.invalid_expires(expires)
        if bad then
          return notify(bad, vim.log.levels.ERROR)
        end
        done({ name = name, kind = kind.kind, expires = expires })
      end)
    end)
  end)
end

--- Write one entry into the config, show it, and collect so the row appears.
function M.write_entry(entry_kind, value)
  local root = M.root()
  local target = core.resolve_config(root, cfg)
  if target == '' then
    target = vim.fs.joinpath(root, cfg.config_path ~= '' and cfg.config_path or 'expiry-radar.json')
  end
  local existing = ''
  if vim.uv.fs_stat(target) then
    existing = table.concat(vim.fn.readfile(target), '\n')
  end

  local text, line = core.add_to_array(
    existing,
    core.ARRAY_FOR[entry_kind],
    core.render_entry(entry_kind, value)
  )
  local ok, err = pcall(vim.fn.writefile, vim.split(text, '\n'), target, 'b')
  if not ok then
    return notify('could not write ' .. target .. ': ' .. tostring(err), vim.log.levels.ERROR)
  end

  -- Shown, not just written: the entry is now the operator's to check, and a
  -- config edited invisibly is one nobody trusts.
  vim.cmd.edit(vim.fn.fnameescape(target))
  pcall(vim.api.nvim_win_set_cursor, 0, { line, 0 })
  -- Straight into a collection, so the row appears in the list rather than
  -- waiting for the next refresh to prove the edit worked.
  M.collect({ manual = true, reason = 'item added' })
end

function M.open_config()
  local root = M.root()
  local existing = core.resolve_config(root, cfg)
  if existing ~= '' then
    return vim.cmd.edit(vim.fn.fnameescape(existing))
  end
  local target = vim.fs.joinpath(root, cfg.config_path ~= '' and cfg.config_path or 'expiry-radar.json')
  local example = vim.fs.joinpath(root, 'expiry-radar.example.json')
  -- Seeded from the repository's own example when there is one, so a new file
  -- shows every source rather than the two that need no credentials.
  local seed = vim.uv.fs_stat(example) and vim.fn.readfile(example)
    or { '{', '  "endpoints": [{ "host": "shop.example.com" }],', '  "domains": ["example.com"]', '}' }
  vim.fn.writefile(seed, target)
  notify('created ' .. target)
  vim.cmd.edit(vim.fn.fnameescape(target))
end

function M.show_log()
  ui.float(#log_lines > 0 and log_lines or { 'nothing logged yet' }, {
    title = ' expiry-radar log ',
    filetype = 'log',
  })
end

-- --- setup ---------------------------------------------------------------------

function M.setup(opts)
  cfg = config.resolve(opts)
  if not cfg.enabled then
    ui.clear()
    return
  end
  attach_autocmds()
  arm_sweep()
  if cfg.collect.on_startup and cfg.collect.trigger ~= 'manual' then
    -- Let the session settle before dialling anything.
    vim.defer_fn(function()
      M.collect({ reason = 'startup' })
    end, 5000)
  end
end

return M
