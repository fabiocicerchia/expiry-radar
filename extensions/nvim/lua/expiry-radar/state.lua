-- The one collection a session has, and the log of how it went.
--
-- Every part of the plugin reads the same three things -- the resolved
-- configuration, the last completed collection, and the process running right
-- now -- so they live in one place rather than being threaded through every
-- call or duplicated per module.

local M = {
  --- Resolved configuration. nil until setup() has run.
  cfg = nil,
  --- The last completed collection.
  snapshot = nil,
  --- The vim.system handle of the collection in flight, if any.
  handle = nil,
}

local log_lines = {}

function M.log(fmt, ...)
  log_lines[#log_lines + 1] = string.format('[%s] ' .. fmt, os.date('%H:%M:%S'), ...)
  if #log_lines > 500 then
    table.remove(log_lines, 1)
  end
end

function M.log_text()
  return log_lines
end

function M.notify(msg, level)
  vim.notify('expiry-radar: ' .. msg, level or vim.log.levels.INFO)
end

function M.root()
  return vim.fs.root(0, { 'expiry-radar.json', 'go.mod', '.git' }) or vim.uv.cwd()
end

return M
