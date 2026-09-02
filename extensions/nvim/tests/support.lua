-- A project on disk with a stand-in expiry-radar in it.
--
-- The specs that exercise init.lua and health.lua have to run the CLI, so they
-- run a two-line shell script instead: the report is fixed, the failed source
-- is fixed, and nothing leaves the machine. `cmd` is passed to setup(), so the
-- binary search never runs and the result does not depend on what happens to be
-- installed on the box the specs run on.

local M = {}

--- One row of a `-format json` report, with every field the plugin reads.
function M.item(over)
  return vim.tbl_extend('force', {
    name = 'shop.example.com',
    kind = 'tls_cert',
    source = 'tls:endpoint',
    expires = '2026-01-01T00:00:00Z',
    daysLeft = -3,
    expired = true,
    priority = 0.9,
    blastRadius = 0.8,
    why = 'public endpoint',
  }, over or {})
end

--- A temporary project directory, removed by the caller.
function M.project(config)
  local dir = vim.fn.tempname()
  vim.fn.mkdir(dir, 'p')
  if config ~= nil then
    vim.fn.writefile(
      vim.split(type(config) == 'string' and config or vim.json.encode(config), '\n'),
      vim.fs.joinpath(dir, 'expiry-radar.json')
    )
  end
  return dir
end

--- A stand-in CLI: `-format ical` renders a calendar, anything else the report.
---@param dir string project directory
---@param opts table { report?, ical?, stderr?, code? }
function M.fake_cli(dir, opts)
  local path = vim.fs.joinpath(dir, 'fake-radar')
  local lines = { '#!/bin/sh' }
  if opts.stderr then
    lines[#lines + 1] = ("printf '%%s\\n' %s >&2"):format(vim.fn.shellescape(opts.stderr))
  end
  if opts.ical then
    lines[#lines + 1] = 'case "$*" in'
    lines[#lines + 1] = ("  *'-format ical'*) printf '%s'; exit 0 ;;"):format(opts.ical)
    lines[#lines + 1] = 'esac'
  end
  vim.list_extend(lines, { "cat <<'RADAR_EOF'", opts.report or '', 'RADAR_EOF' })
  lines[#lines + 1] = 'exit ' .. tostring(opts.code or 0)
  vim.fn.writefile(lines, path)
  vim.uv.fs_chmod(path, 493)
  return path
end

--- Wait for an asynchronous collection, and say so rather than time out silently.
function M.wait(predicate, what)
  assert(vim.wait(20000, predicate, 20), 'timed out waiting for ' .. what)
end

--- Record what the plugin told the user, instead of printing it.
function M.capture_notify()
  local original = vim.notify
  local seen = {}
  vim.notify = function(message, level)
    seen[#seen + 1] = { message = message, level = level }
  end
  return seen, function()
    vim.notify = original
  end
end

return M
