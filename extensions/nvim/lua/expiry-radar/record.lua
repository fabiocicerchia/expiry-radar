-- Recording and un-recording an item, and the config file itself.
--
-- Most of what expires is discovered; a host to probe, a domain to look up and
-- a date nobody can find at all are recorded by the operator. The list is where
-- you are standing when you notice something is missing, so these write the
-- config for you rather than leaving you in a JSON editor.

local core = require('expiry-radar.core')
local edit = require('expiry-radar.edit')
local run = require('expiry-radar.run')
local state = require('expiry-radar.state')

local M = {}

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
    vim.ui.select(edit.MANUAL_KINDS, {
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
        local bad = edit.invalid_expires(expires)
        if bad then
          return state.notify(bad, vim.log.levels.ERROR)
        end
        done({ name = name, kind = kind.kind, expires = expires })
      end)
    end)
  end)
end

--- Write one entry into the config, show it, and collect so the row appears.
function M.write_entry(entry_kind, value)
  local root = state.root()
  local target = core.resolve_config(root, state.cfg)
  if target == '' then
    target = vim.fs.joinpath(root, state.cfg.config_path ~= '' and state.cfg.config_path or 'expiry-radar.json')
  end
  local existing = ''
  if vim.uv.fs_stat(target) then
    existing = table.concat(vim.fn.readfile(target), '\n')
  end

  local text, line = edit.add_to_array(
    existing,
    edit.ARRAY_FOR[entry_kind],
    edit.render_entry(entry_kind, value)
  )
  local ok, err = pcall(vim.fn.writefile, vim.split(text, '\n'), target, 'b')
  if not ok then
    return state.notify('could not write ' .. target .. ': ' .. tostring(err), vim.log.levels.ERROR)
  end

  -- Shown, not just written: the entry is now the operator's to check, and a
  -- config edited invisibly is one nobody trusts.
  vim.cmd.edit(vim.fn.fnameescape(target))
  pcall(vim.api.nvim_win_set_cursor, 0, { line, 0 })
  -- Straight into a collection, so the row appears in the list rather than
  -- waiting for the next refresh to prove the edit worked.
  run.collect({ manual = true, reason = 'item added' })
end

--- Stop tracking a recorded item.
---
--- Only recorded items are offered. A discovered one has no entry to delete --
--- removing a line would not remove a certificate from an Ingress -- and
--- offering it would imply this tool writes to your estate, which it never does.
function M.remove_item()
  run.ensure_collected(function()
    local recorded = {}
    for _, item in ipairs(state.snapshot.items) do
      if item.origin and core.array_for_source(item.source) then
        recorded[#recorded + 1] = item
      end
    end
    if #recorded == 0 then
      return state.notify('nothing recorded to remove — every item here was discovered.')
    end

    vim.ui.select(recorded, {
      prompt = 'Stop tracking which item?',
      format_item = function(item)
        return string.format('%-42s %-10s %s', item.display, core.human_days(item.daysLeft), item.source)
      end,
    }, function(item)
      if not item then
        return
      end
      local path = state.snapshot.config_path
      local ok, text = pcall(function()
        return table.concat(vim.fn.readfile(path), '\n')
      end)
      if not ok then
        return state.notify('could not read ' .. path, vim.log.levels.ERROR)
      end
      -- The position came from the last collection; the file may have been
      -- edited since. Removing whatever now sits there would delete the wrong
      -- entry, so check it still names this item before touching anything.
      local on_line = vim.split(text, '\n')[item.origin.line] or ''
      if not on_line:find(item.name, 1, true) then
        return state.notify(
          vim.fs.basename(path) .. ' has changed since the last collection — refresh and try again.',
          vim.log.levels.WARN
        )
      end
      local next_text =
        edit.remove_entry(text, core.array_for_source(item.source), item.origin.line, item.origin.column)
      if not next_text then
        return state.notify('could not find the entry for ' .. item.display, vim.log.levels.WARN)
      end
      local wrote = pcall(vim.fn.writefile, vim.split(next_text, '\n'), path, 'b')
      if not wrote then
        return state.notify('could not write ' .. path, vim.log.levels.ERROR)
      end
      state.notify('stopped tracking ' .. item.display)
      run.collect({ manual = true, reason = 'item removed' })
    end)
  end)
end

function M.open_config()
  local root = state.root()
  local existing = core.resolve_config(root, state.cfg)
  if existing ~= '' then
    return vim.cmd.edit(vim.fn.fnameescape(existing))
  end
  local target = vim.fs.joinpath(root, state.cfg.config_path ~= '' and state.cfg.config_path or 'expiry-radar.json')
  local example = vim.fs.joinpath(root, 'expiry-radar.example.json')
  -- Seeded from the repository's own example when there is one, so a new file
  -- shows every source rather than the two that need no credentials.
  local seed = vim.uv.fs_stat(example) and vim.fn.readfile(example)
    or { '{', '  "endpoints": [{ "host": "shop.example.com" }],', '  "domains": ["example.com"]', '}' }
  vim.fn.writefile(seed, target)
  state.notify('created ' .. target)
  vim.cmd.edit(vim.fn.fnameescape(target))
end

return M
