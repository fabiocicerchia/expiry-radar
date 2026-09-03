-- Showing the inventory: a float, the quickfix list, the log.
--
-- Reading only. Nothing here starts a collection of its own -- it asks for one
-- through run.ensure_collected when there is nothing to show yet -- and nothing
-- here writes to the config file.

local core = require('expiry-radar.core')
local run = require('expiry-radar.run')
local state = require('expiry-radar.state')
local ui = require('expiry-radar.ui')

local M = {}

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

--- Only what the inventory actually has: offering a window with nothing in it
--- is a prompt that wastes the one keystroke somebody had a reason for.
local function filter_choices(items)
  local counts, kinds = {}, {}
  for _, item in ipairs(items) do
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
  return choices
end

local function filter_label(choice)
  local label = choice.kind == 'severity' and core.SEVERITY_LABEL[choice.key]
    or core.kind_label(choice.key)
  return string.format('%-9s %-20s %d', choice.kind, label, choice.count)
end

local function matching(items, choice)
  local field = choice.kind == 'severity' and 'severity' or 'kind'
  local rows = {}
  for _, item in ipairs(items) do
    if item[field] == choice.key then
      rows[#rows + 1] = item
    end
  end
  return rows
end

--- Pick a deadline window or a kind; the matching items go to the quickfix list.
function M.filter()
  run.ensure_collected(function()
    local choices = filter_choices(state.snapshot.items)
    if #choices == 0 then
      return state.notify('nothing to filter.')
    end
    vim.ui.select(choices, {
      prompt = 'Show which items',
      format_item = filter_label,
    }, function(choice)
      if not choice then
        return
      end
      ui.to_quickfix(
        matching(state.snapshot.items, choice),
        state.snapshot.config_path,
        'expiry-radar: ' .. choice.key
      )
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

return M
