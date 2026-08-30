-- Defaults, and the validation that turns a typo into a message at setup()
-- rather than a nil index inside a callback a minute into a collection.

local M = {}

M.defaults = {
  enabled = true,

  --- How to run expiry-radar. A list, so a wrapper can go in front of it.
  --- Empty auto-detects: ./bin/expiry-radar in the project (what `make build`
  --- writes), then expiry-radar on PATH, then $GOBIN / $GOPATH/bin / ~/go/bin.
  cmd = {},

  --- Config file passed as -config. Empty uses expiry-radar.json at the
  --- project root, when there is one. Relative paths resolve against the root.
  config_path = '',

  --- Hosts to probe over TLS (-endpoints), in addition to the config file.
  endpoints = {},

  --- Domains to check via RDAP (-domains), in addition to the config file.
  domains = {},

  --- Appended to every invocation, verbatim.
  extra_args = {},

  collect = {
    --- 'on_config_save' | 'on_config_save_and_interval' | 'interval' | 'manual'
    --- Never on a keystroke, and never on every save: a collection dials real
    --- hosts, queries RDAP and calls cloud APIs, and expiry dates move on the
    --- scale of days.
    trigger = 'on_config_save_and_interval',
    --- Quiet period after the triggering event. Later events restart it.
    debounce_ms = 1500,
    --- Period of the background refresh, for the interval triggers.
    interval_minutes = 60,
    --- One collection when the plugin loads, so the list is populated.
    on_startup = true,
    --- Overall budget, passed to the CLI as -timeout and enforced here.
    timeout_ms = 120000,
    --- Only collect items expiring within this many days (-within). 0 is
    --- everything, which is the point of an inventory.
    within_days = 0,
    --- Only collect items at or above this priority (-min-priority), 0..1.
    min_priority = 0,
  },

  diagnostics = {
    enabled = true,
    --- Items expiring within this many days are warnings. Anything already
    --- expired is always an error.
    warn_within_days = 14,
    --- Items expiring within this many days are informational. Beyond it,
    --- nothing is published: a squiggle follows the deadline, not the ranking.
    info_within_days = 30,
  },

  --- The statusline component turns amber inside this window, red once
  --- anything has expired.
  status = {
    warn_within_days = 14,
  },
}

local function merge(defaults, opts)
  local out = {}
  for key, value in pairs(defaults) do
    if type(value) == 'table' and not vim.islist(value) then
      out[key] = merge(value, (opts or {})[key] or {})
    elseif opts and opts[key] ~= nil then
      out[key] = opts[key]
    else
      out[key] = value
    end
  end
  for key, value in pairs(opts or {}) do
    if out[key] == nil then
      out[key] = value
    end
  end
  return out
end

local TRIGGERS = {
  on_config_save = true,
  on_config_save_and_interval = true,
  interval = true,
  manual = true,
}

local function non_negative(v)
  return type(v) == 'number' and v >= 0
end

function M.validate(cfg)
  vim.validate('enabled', cfg.enabled, 'boolean')
  vim.validate('cmd', cfg.cmd, vim.islist, 'a list of strings')
  vim.validate('config_path', cfg.config_path, 'string')
  vim.validate('endpoints', cfg.endpoints, vim.islist, 'a list of hosts')
  vim.validate('domains', cfg.domains, vim.islist, 'a list of domains')
  vim.validate('extra_args', cfg.extra_args, vim.islist, 'a list of arguments')
  vim.validate('collect.trigger', cfg.collect.trigger, function(v)
    return TRIGGERS[v] == true
  end, 'one of: ' .. table.concat(vim.tbl_keys(TRIGGERS), ', '))
  vim.validate('collect.debounce_ms', cfg.collect.debounce_ms, function(v)
    return type(v) == 'number' and v >= 250
  end, 'a number >= 250')
  vim.validate('collect.interval_minutes', cfg.collect.interval_minutes, function(v)
    return type(v) == 'number' and v >= 1
  end, 'a number >= 1')
  vim.validate('collect.on_startup', cfg.collect.on_startup, 'boolean')
  vim.validate('collect.timeout_ms', cfg.collect.timeout_ms, function(v)
    return type(v) == 'number' and v >= 10000
  end, 'a number >= 10000')
  vim.validate('collect.within_days', cfg.collect.within_days, non_negative, 'a number >= 0')
  vim.validate('collect.min_priority', cfg.collect.min_priority, function(v)
    return type(v) == 'number' and v >= 0 and v <= 1
  end, 'a number between 0 and 1')
  vim.validate('diagnostics.enabled', cfg.diagnostics.enabled, 'boolean')
  vim.validate('diagnostics.warn_within_days', cfg.diagnostics.warn_within_days, non_negative, 'a number >= 0')
  vim.validate('diagnostics.info_within_days', cfg.diagnostics.info_within_days, non_negative, 'a number >= 0')
  vim.validate('status.warn_within_days', cfg.status.warn_within_days, non_negative, 'a number >= 0')

  -- A hint window that ends before the warning window would silently drop the
  -- warnings it is meant to sit outside of.
  if cfg.diagnostics.info_within_days < cfg.diagnostics.warn_within_days then
    cfg.diagnostics.info_within_days = cfg.diagnostics.warn_within_days
  end
  return cfg
end

function M.resolve(opts)
  return M.validate(merge(M.defaults, opts or {}))
end

return M
