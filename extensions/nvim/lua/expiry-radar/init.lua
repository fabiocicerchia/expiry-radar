-- setup(), the public API, and when a collection is allowed to happen.
--
-- A collection dials every configured host over TLS, queries RDAP for every
-- domain and calls the Kubernetes, Vault and AWS APIs. Registries rate-limit
-- RDAP, and an editor left open all day would happily spend the afternoon
-- hammering them. So every trigger funnels through here and obeys the rules the
-- VS Code extension settled on: debounce a burst of config saves into one run,
-- exactly one process at a time, and a manual run preempts an automatic one.
-- What a run actually does is run.lua; what it writes back is record.lua.

local config = require('expiry-radar.config')
local core = require('expiry-radar.core')
local record = require('expiry-radar.record')
local run = require('expiry-radar.run')
local state = require('expiry-radar.state')
local ui = require('expiry-radar.ui')

local M = {}

local debounce_timer = nil
local sweep_timer = nil

function M.is_setup()
  return state.cfg ~= nil
end

function M.config()
  return state.cfg
end

function M.log_text()
  return state.log_text()
end

function M.root()
  return state.root()
end

function M.snapshot()
  return state.snapshot
end

function M.items()
  return state.snapshot and state.snapshot.items or {}
end

function M.is_collecting()
  return state.handle ~= nil
end

--- A lualine component, or anything else that wants one string.
---
--- The soonest deadline, not the highest priority. Priority is the right order
--- for a list you are working through; one string in the corner is a clock, and
--- a clock showing the second-soonest deadline would be wrong in exactly the
--- case it matters.
function M.statusline()
  if not state.cfg then
    return ''
  end
  if state.handle then
    return 'radar collecting'
  end
  if not state.snapshot then
    return ''
  end
  local soonest, expired = nil, 0
  for _, item in ipairs(state.snapshot.items) do
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
  local incomplete = #state.snapshot.warnings > 0 and ' (incomplete)' or ''
  if not soonest then
    return string.format('%s radar %s%s', incomplete ~= '' and '!' or '·', '0', incomplete)
  end
  local mark = expired > 0 and '✗' or (soonest.daysLeft <= state.cfg.status.warn_within_days and '!' or '·')
  return string.format('%s radar %s%s', mark, core.human_days(soonest.daysLeft), incomplete)
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
  debounce_timer:start(state.cfg.collect.debounce_ms, 0, function()
    debounce_timer = stop_timer(debounce_timer)
    vim.schedule(function()
      run.collect(opts)
    end)
  end)
end

local function arm_sweep()
  sweep_timer = stop_timer(sweep_timer)
  local trigger = state.cfg.collect.trigger
  if trigger ~= 'interval' and trigger ~= 'on_config_save_and_interval' then
    return
  end
  local period = state.cfg.collect.interval_minutes * 60000
  sweep_timer = vim.uv.new_timer()
  sweep_timer:start(period, period, function()
    vim.schedule(function()
      run.collect({ reason = 'periodic refresh' })
    end)
  end)
end

local function attach_autocmds()
  local group = vim.api.nvim_create_augroup('expiry-radar', { clear = true })

  vim.api.nvim_create_autocmd('BufWritePost', {
    group = group,
    callback = function(event)
      local trigger = state.cfg.collect.trigger
      if not state.cfg.enabled or (trigger ~= 'on_config_save' and trigger ~= 'on_config_save_and_interval') then
        return
      end
      -- Only the config file. Every other save has nothing to do with what the
      -- estate has expiring, and re-dialling every host because somebody saved
      -- a README would be indefensible.
      local config_path = core.resolve_config(state.root(), state.cfg)
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
      if state.snapshot then
        vim.schedule(function()
          ui.publish(state.snapshot.items, state.cfg, state.snapshot.config_path)
        end)
      end
    end,
  })
end

-- --- commands ------------------------------------------------------------------

--- The inventory, in a float.
function M.report()
  run.ensure_collected(function()
    ui.float(ui.report_lines(state.snapshot), { title = ' expiry-radar ', filetype = 'expiry-radar' })
  end)
end

--- Everything, in the quickfix list.
function M.list()
  run.ensure_collected(function()
    if #state.snapshot.items == 0 then
      return state.notify('nothing expiring — or no sources were enabled.')
    end
    ui.to_quickfix(state.snapshot.items, state.snapshot.config_path, 'expiry-radar')
    vim.cmd('copen')
  end)
end

--- Pick a deadline window or a kind; the matching items go to the quickfix list.
function M.filter()
  run.ensure_collected(function()
    local counts, kinds = {}, {}
    for _, item in ipairs(state.snapshot.items) do
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
      return state.notify('nothing to filter.')
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
      for _, item in ipairs(state.snapshot.items) do
        if item[choice.kind == 'severity' and 'severity' or 'kind'] == choice.key then
          rows[#rows + 1] = item
        end
      end
      ui.to_quickfix(rows, state.snapshot.config_path, 'expiry-radar: ' .. choice.key)
      vim.cmd('copen')
    end)
  end)
end

--- What the inventory already knows about the host under the cursor.
function M.hover()
  local needle = core.host_at_cursor(vim.api.nvim_get_current_line())
  if needle == '' then
    return state.notify('no host or domain on this line.')
  end
  run.ensure_collected(function()
    ui.float(ui.hover_lines(needle, core.items_matching(state.snapshot.items, needle), state.snapshot), {
      title = ' ' .. needle .. ' ',
      filetype = 'expiry-radar',
    })
  end)
end

function M.show_log()
  local lines = state.log_text()
  ui.float(#lines > 0 and lines or { 'nothing logged yet' }, {
    title = ' expiry-radar log ',
    filetype = 'log',
  })
end

-- --- setup ---------------------------------------------------------------------

function M.setup(opts)
  state.cfg = config.resolve(opts)
  if not state.cfg.enabled then
    ui.clear()
    return
  end
  attach_autocmds()
  arm_sweep()
  if state.cfg.collect.on_startup and state.cfg.collect.trigger ~= 'manual' then
    -- Let the session settle before dialling anything.
    vim.defer_fn(function()
      run.collect({ reason = 'startup' })
    end, 5000)
  end
end

-- --- the public API ------------------------------------------------------------
--
-- Kept here, and only here: :ExpiryRadar* and :checkhealth call these names,
-- and where the body lives is not their business.

M.collect = run.collect
M.cancel = run.cancel
M.probe = run.probe
M.export = run.export
M.add_item = record.add_item
M.prompt_manual = record.prompt_manual
M.write_entry = record.write_entry
M.remove_item = record.remove_item
M.open_config = record.open_config

return M
