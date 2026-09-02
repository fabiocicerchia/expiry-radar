-- Diagnostics, the floats, and the quickfix list.

local core = require('expiry-radar.core')

local M = {}

local NS = vim.api.nvim_create_namespace('expiry-radar')
M.namespace = NS

local SEVERITY = {
  expired = vim.diagnostic.severity.ERROR,
  urgent = vim.diagnostic.severity.WARN,
  soon = vim.diagnostic.severity.INFO,
  -- 'ok' is deliberately absent. A squiggle on a certificate with three months
  -- left is noise on a line somebody is trying to edit.
}

--- What expires, when, and why it is ranked where it is. A ranking nobody can
--- explain gets ignored, and a hover is where somebody actually reads it.
function M.message(item)
  local head
  if item.daysLeft < 0 then
    head = string.format('%s expired %d day(s) ago', item.display, math.floor(-item.daysLeft))
  else
    head = string.format('%s expires in %s', item.display, core.human_days(item.daysLeft))
  end
  return string.format('%s.\n\n%s\n\n%s', head, item.why, table.concat(core.describe(item), ' · '))
end

--- Publish everything at once. A partial publish is not an option: the
--- namespace is global, so writing only what was just collected would drop
--- everything else.
---
--- Only items declared in the config file get one: a certificate found on an
--- Ingress was never written down here, and there is no honest line for it.
function M.publish(items, cfg, config_path)
  vim.diagnostic.reset(NS)
  if not cfg.diagnostics.enabled or config_path == '' then
    return
  end
  -- Matched on the normalised name rather than through bufnr(), which takes a
  -- pattern and would happily answer with some other buffer whose name merely
  -- contains this path.
  local wanted = vim.fs.normalize(config_path)
  local buf = nil
  for _, candidate in ipairs(vim.api.nvim_list_bufs()) do
    if vim.api.nvim_buf_is_loaded(candidate) then
      local name = vim.api.nvim_buf_get_name(candidate)
      if name ~= '' and vim.fs.normalize(name) == wanted then
        buf = candidate
        break
      end
    end
  end
  -- A config that is not open has nowhere to put a squiggle; the list still has
  -- every item, and BufReadPost republishes when it is opened.
  if not buf then
    return
  end

  local out = {}
  for _, item in ipairs(items) do
    local severity = SEVERITY[item.severity]
    if item.origin and severity then
      out[#out + 1] = {
        lnum = math.max(0, item.origin.line - 1),
        col = math.max(0, item.origin.column - 1),
        end_lnum = math.max(0, item.origin.line - 1),
        -- The editor clamps this, so the whole entry is covered without
        -- reading the buffer to measure it.
        end_col = 9999,
        severity = severity,
        source = 'expiry-radar',
        code = item.kind,
        message = M.message(item),
      }
    end
  end
  vim.diagnostic.set(NS, buf, out)
end

function M.clear()
  vim.diagnostic.reset(NS)
end

-- --- floats ------------------------------------------------------------------

function M.float(lines, opts)
  opts = opts or {}
  local buf = vim.api.nvim_create_buf(false, true)
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
  vim.bo[buf].modifiable = false
  vim.bo[buf].filetype = opts.filetype or 'markdown'
  vim.bo[buf].bufhidden = 'wipe'

  local width = 0
  for _, line in ipairs(lines) do
    width = math.max(width, vim.fn.strdisplaywidth(line))
  end
  width = math.min(math.max(width + 2, 48), math.floor(vim.o.columns * 0.9))
  local height = math.min(math.max(#lines, 3), math.floor(vim.o.lines * 0.8))

  local win = vim.api.nvim_open_win(buf, true, {
    relative = 'editor',
    row = math.floor((vim.o.lines - height) / 2),
    col = math.floor((vim.o.columns - width) / 2),
    width = width,
    height = height,
    style = 'minimal',
    border = 'rounded',
    title = opts.title or ' expiry-radar ',
    title_pos = 'center',
  })
  vim.wo[win].wrap = false
  vim.wo[win].cursorline = true
  for _, key in ipairs({ 'q', '<Esc>' }) do
    vim.keymap.set('n', key, function()
      if vim.api.nvim_win_is_valid(win) then
        vim.api.nvim_win_close(win, true)
      end
    end, { buffer = buf, nowait = true, silent = true })
  end
  return buf, win
end

local MARK = { expired = '✗', urgent = '!', soon = '·', ok = '✓' }

--- The inventory, as the CLI's own table reads: ranked, with the deadline
--- first, grouped under the severity that decides what to do today.
function M.report_lines(snapshot)
  local lines = {
    string.format(
      '%d item(s) · collected %s',
      #snapshot.items,
      os.date('%H:%M:%S', snapshot.at)
    ),
    string.rep('─', 60),
  }

  -- Failed sources first, and in words. An inventory that quietly lost a source
  -- reads exactly like a clean estate, which is the failure this tool exists to
  -- prevent -- so it is never buried under the rows that did come back.
  if #snapshot.warnings > 0 then
    lines[#lines + 1] = ''
    lines[#lines + 1] = string.format(
      '⚠ %d source(s) failed — this inventory is incomplete:',
      #snapshot.warnings
    )
    for _, warning in ipairs(snapshot.warnings) do
      lines[#lines + 1] = '    ' .. warning
    end
  end

  local shown = nil
  for _, item in ipairs(snapshot.items) do
    if item.severity ~= shown then
      shown = item.severity
      lines[#lines + 1] = ''
      lines[#lines + 1] = core.SEVERITY_LABEL[shown]
    end
    lines[#lines + 1] = string.format(
      '  %s %-14s %-42s %.2f  %s',
      MARK[item.severity] or '?',
      core.human_days(item.daysLeft),
      item.display,
      item.priority,
      item.source
    )
  end

  if #snapshot.items == 0 then
    lines[#lines + 1] = ''
    lines[#lines + 1] = 'Nothing expiring — or no sources were enabled.'
  end
  return lines
end

--- Everything the inventory knows about one host or domain.
---
--- The "nothing found" case deliberately says whether anything actually looked.
--- A source that failed still lets the run succeed, so an empty answer can mean
--- "clean" or "never checked", and those are not the same answer.
function M.hover_lines(needle, matched, snapshot)
  local lines = { needle, string.rep('─', math.max(#needle + 12, 40)), '' }

  if #matched == 0 then
    lines[#lines + 1] = 'Nothing in the inventory matches this.'
    if snapshot and #snapshot.warnings > 0 then
      lines[#lines + 1] = ''
      lines[#lines + 1] = string.format(
        '⚠ %d source(s) failed, so this is not proof of anything:',
        #snapshot.warnings
      )
      for _, warning in ipairs(snapshot.warnings) do
        lines[#lines + 1] = '    ' .. warning
      end
    end
    return lines
  end

  lines[#lines + 1] = string.format('%d item(s)', #matched)
  lines[#lines + 1] = ''
  for _, item in ipairs(matched) do
    lines[#lines + 1] = string.format(
      '%s %s — %s',
      MARK[item.severity] or '?',
      item.display,
      core.human_days(item.daysLeft)
    )
    lines[#lines + 1] = '    ' .. item.why
    for _, fact in ipairs(core.describe(item)) do
      lines[#lines + 1] = '    ' .. fact
    end
    lines[#lines + 1] = ''
  end
  return lines
end

--- Items into the quickfix list, which is where a list of places to go belongs
--- in this editor. An item nothing declared has no place to go, so it carries
--- its facts in the text instead of a bogus file and line.
--- Where a row sends you, or nowhere for an item nothing declared.
local function place(item, config_path)
  local origin = item.origin
  if not origin then
    return { lnum = 0, col = 0, valid = 0 }
  end
  return {
    filename = config_path ~= '' and config_path or nil,
    lnum = origin.line,
    col = origin.column,
    valid = 1,
  }
end

function M.to_quickfix(items, config_path, title)
  local entries = {}
  for _, item in ipairs(items) do
    entries[#entries + 1] = vim.tbl_extend('error', place(item, config_path), {
      text = string.format(
        '%s  %s  [%s] %s',
        core.human_days(item.daysLeft),
        item.display,
        item.source,
        item.why
      ),
      type = (item.severity == 'expired' or item.severity == 'urgent') and 'E' or 'W',
    })
  end
  vim.fn.setqflist({}, ' ', { title = title, items = entries })
end

return M
