-- Everything that does not touch the editor: finding the binary, building the
-- argv, and turning one `-format json` report into rows.
--
-- Kept separate so the specs can exercise it headless, and so the policy that
-- matters -- what gets a squiggle, what order things come in, what a failed
-- source means -- is readable in one file rather than spread through callbacks.

local M = {}

-- --- the binary --------------------------------------------------------------

local function is_file(path)
  local stat = path ~= '' and vim.uv.fs_stat(path) or nil
  return stat ~= nil and stat.type == 'file'
end

--- Every plausible location, most explicit first.
---@param root string project root
---@param cfg table resolved configuration
---@return string[] candidates
function M.candidates(root, cfg)
  local exe = vim.fn.has('win32') == 1 and 'expiry-radar.exe' or 'expiry-radar'
  local out = {}
  if cfg.cmd and #cfg.cmd > 0 then
    return vim.list_slice(cfg.cmd, 1, #cfg.cmd)
  end
  -- `make build` writes here, so a checkout of the repository is its own best
  -- source of the binary, and the one most likely to be current.
  out[#out + 1] = vim.fs.joinpath(root, 'bin', exe)
  local on_path = vim.fn.exepath('expiry-radar')
  if on_path ~= '' then
    out[#out + 1] = on_path
  end
  -- `go install` puts it in GOBIN or GOPATH/bin, neither of which is
  -- necessarily on the PATH an editor inherits.
  for _, dir in ipairs({
    vim.env.GOBIN,
    vim.env.GOPATH and vim.fs.joinpath(vim.env.GOPATH, 'bin') or nil,
    vim.fs.joinpath(vim.fn.expand('~'), 'go', 'bin'),
  }) do
    if dir and dir ~= '' then
      out[#out + 1] = vim.fs.joinpath(dir, exe)
    end
  end
  return out
end

--- The command to run, or nil with the reason there isn't one.
function M.resolve_cmd(root, cfg)
  if cfg.cmd and #cfg.cmd > 0 then
    return vim.list_slice(cfg.cmd, 1, #cfg.cmd)
  end
  for _, candidate in ipairs(M.candidates(root, cfg)) do
    if is_file(candidate) then
      return { candidate }
    end
  end
  return nil, 'the expiry-radar binary was not found'
end

M.INSTALL_COMMAND = 'go install github.com/fabiocicerchia/expiry-radar/cmd/expiry-radar@latest'

--- The config file this project resolves to, or '' when there is none.
function M.resolve_config(root, cfg)
  local configured = cfg.config_path or ''
  local candidate
  if configured ~= '' then
    candidate = vim.fs.normalize(configured)
    if not vim.startswith(candidate, '/') and not candidate:match('^%a:') then
      candidate = vim.fs.joinpath(root, candidate)
    end
  else
    candidate = vim.fs.joinpath(root, 'expiry-radar.json')
  end
  return is_file(candidate) and candidate or ''
end

--- Whether this project has anything to collect at all.
---
--- Every source is opt-in: with no config file and nothing in setup(), a run
--- exits 2 with "no sources configured". That is the right answer to a
--- command, and completely wrong as an error five seconds after Neovim opens
--- in a project that has nothing to do with this tool.
function M.has_sources(root, cfg)
  return M.resolve_config(root, cfg) ~= '' or #cfg.endpoints > 0 or #cfg.domains > 0
end

-- --- argv --------------------------------------------------------------------

--- Argv for one run.
---@param cfg table resolved configuration
---@param opts table { format, config_path?, endpoints?, domains?, ignore_config? }
function M.argv(cfg, opts)
  local args = { '-format', opts.format or 'json' }
  local config_path = (not opts.ignore_config) and (opts.config_path or '') or ''
  if config_path ~= '' then
    vim.list_extend(args, { '-config', config_path })
  elseif opts.ignore_config then
    -- Not merely omitting the flag: -config defaults to expiry-radar.json,
    -- resolved against the working directory, which is the project root.
    -- Omitting it in a project that actually uses this tool would quietly
    -- collect the whole estate alongside the one host being probed -- slow, and
    -- every credentialed source hit for a question about a single hostname.
    -- An empty path stats as "does not exist", which the CLI already handles as
    -- "no config".
    vim.list_extend(args, { '-config', '' })
  end

  local endpoints = {}
  local domains = {}
  if not opts.ignore_config then
    vim.list_extend(endpoints, cfg.endpoints)
    vim.list_extend(domains, cfg.domains)
  end
  vim.list_extend(endpoints, opts.endpoints or {})
  vim.list_extend(domains, opts.domains or {})
  if #endpoints > 0 then
    vim.list_extend(args, { '-endpoints', table.concat(endpoints, ',') })
  end
  if #domains > 0 then
    vim.list_extend(args, { '-domains', table.concat(domains, ',') })
  end

  -- Filtering happens in the CLI rather than in the list: -within also caps
  -- what an export contains, and a list that hid rows the export still carried
  -- would be two different answers to one question.
  if not opts.ignore_config then
    if cfg.collect.within_days > 0 then
      vim.list_extend(args, { '-within', tostring(cfg.collect.within_days) })
    end
    if cfg.collect.min_priority > 0 then
      vim.list_extend(args, { '-min-priority', tostring(cfg.collect.min_priority) })
    end
  end
  vim.list_extend(args, { '-timeout', string.format('%ds', math.floor(cfg.collect.timeout_ms / 1000)) })
  if not opts.ignore_config then
    vim.list_extend(args, cfg.extra_args)
  end
  return args
end

-- --- the report --------------------------------------------------------------

--- Worst deadline first: the list's order and the filter's order.
M.SEVERITIES = { 'expired', 'urgent', 'soon', 'ok' }

M.SEVERITY_RANK = { expired = 1, urgent = 2, soon = 3, ok = 4 }

M.SEVERITY_LABEL = {
  expired = 'Expired',
  urgent = 'Within 14 days',
  soon = 'Within 30 days',
  ok = 'Further out',
}

M.KIND_LABEL = {
  tls_cert = 'TLS certificate',
  intermediate_ca = 'Intermediate CA',
  secret = 'Secret',
  iam_access_key = 'IAM access key',
  vault_lease = 'Vault lease',
  domain = 'Domain',
}

--- A kind this build has never heard of still gets a readable label.
function M.kind_label(kind)
  return M.KIND_LABEL[kind] or (tostring(kind):gsub('_', ' '))
end

--- Colour follows the deadline, not the priority. The defaults are the HTML
--- report's, so a row is the same colour in the list and in the report.
function M.severity(days_left, warn_within, info_within)
  warn_within = warn_within or 14
  info_within = info_within or 30
  if days_left < 0 then
    return 'expired'
  elseif days_left <= warn_within then
    return 'urgent'
  elseif days_left <= info_within then
    return 'soon'
  end
  return 'ok'
end

--- Whole days, and never a cheerful "0 days" for something already broken.
function M.human_days(days)
  if days < 0 then
    return string.format('expired %dd ago', math.floor(-days))
  elseif days < 1 then
    return 'today'
  end
  return string.format('%dd', math.floor(days))
end

--- The namespace is a prefix, not a repeat -- internal/output.displayName.
function M.display_name(item)
  local ns = item.namespace or ''
  if ns ~= '' and not vim.startswith(item.name, ns .. '/') then
    return ns .. '/' .. item.name
  end
  return item.name
end

--- The sources whose items were declared in the config file, and so have a
--- line to point at. Everything else was discovered: a certificate on an
--- Ingress was never written down here, and squiggling a config line for it
--- would be a lie.
M.DECLARED_BY = {
  ['tls:endpoint'] = true,
  ['domain:rdap'] = true,
  ['domain:whois'] = true,
  ['manual'] = true,
}

--- Which config array an item's source records into, or nil if it was
--- discovered. A discovered item has no entry to delete -- removing a line
--- would not remove a certificate from an Ingress.
function M.array_for_source(source)
  if source == 'tls:endpoint' then
    return 'endpoints'
  elseif source == 'domain:rdap' or source == 'domain:whois' then
    return 'domains'
  elseif source == 'manual' then
    return 'manual'
  end
  return nil
end

--- Rows the editor can place, from one decoded report.
---@param report table decoded `-format json` output
---@param cfg table resolved configuration
---@param declared table<string, table>|nil name -> { line, column } from the config
function M.normalize(report, cfg, declared)
  local out = {}
  for _, raw in ipairs(report.items or {}) do
    local item = vim.tbl_extend('force', {}, raw)
    item.display = M.display_name(raw)
    item.severity = M.severity(
      raw.daysLeft,
      cfg.diagnostics.warn_within_days,
      cfg.diagnostics.info_within_days
    )
    if declared and M.DECLARED_BY[raw.source] then
      item.origin = declared[raw.name]
    end
    out[#out + 1] = item
  end
  table.sort(out, M.compare_items)
  return out
end

--- Worst deadline first, then highest priority -- the report's own tiebreak.
function M.compare_items(a, b)
  if a.severity ~= b.severity then
    return M.SEVERITY_RANK[a.severity] < M.SEVERITY_RANK[b.severity]
  end
  if a.priority ~= b.priority then
    return a.priority > b.priority
  end
  if a.daysLeft ~= b.daysLeft then
    return a.daysLeft < b.daysLeft
  end
  return a.display < b.display
end

--- The sources that failed, from stderr.
---
--- The CLI prints one `expiry-radar: warning: <source>: <error>` line per
--- failed source and still reports everything the others managed to read -- so
--- a run can succeed, look clean, and be missing an entire cloud account.
--- These lines are the only evidence of that, so they are shown, never merely
--- logged.
function M.parse_warnings(stderr)
  local out = {}
  for line in tostring(stderr or ''):gmatch('[^\n]+') do
    local rest = vim.trim(line):match('^expiry%-radar:%s*warning:%s*(.+)$')
    if rest then
      out[#out + 1] = vim.trim(rest)
    end
  end
  return out
end

--- The facts behind a row, as the hover, the float and the log all want them.
function M.describe(item)
  local facts = { 'kind: ' .. M.kind_label(item.kind), 'source: ' .. item.source }
  if item.namespace and item.namespace ~= '' then
    facts[#facts + 1] = 'namespace: ' .. item.namespace
  end
  vim.list_extend(facts, {
    string.format('expires: %s (%s)', tostring(item.expires):sub(1, 10), M.human_days(item.daysLeft)),
    string.format('priority: %.2f', item.priority),
    string.format('blast radius: %.2f', item.blastRadius),
  })
  return facts
end

-- --- what is under the cursor -------------------------------------------------

--- The host or domain named on a line, or ''.
---
--- Pattern-based rather than per-format: a hostname turns up in a config file,
--- an Ingress manifest, a Terraform variable, a URL in a comment and a curl
--- command in a README, and one pattern covers all of them. A quoted or
--- schemed value is tried first, so `https://shop.example.com/checkout` answers
--- the host rather than the scheme.
function M.host_at_cursor(line)
  -- Tried in order, and lazily: a list with a nil in it ends at the nil as far
  -- as ipairs is concerned, so the first pattern that did not match would hide
  -- every pattern after it.
  local patterns = {
    'https?://([%w%.%-]+%.%a%a+)',
    '"([%w%.%-]+%.%a%a+)"',
    "'([%w%.%-]+%.%a%a+)'",
    '([%w%.%-]+%.%a%a+)',
  }
  for _, pattern in ipairs(patterns) do
    local candidate = line:match(pattern)
    -- At least one dot and a plausible TLD; anything shorter is a filename or
    -- a method call, not a host.
    if candidate and candidate:find('%.') and not candidate:match('^%d+%.%d+$') then
      -- Keep an explicit port: the CLI names a certificate after the host it
      -- was configured with, port and all.
      local port = line:match(vim.pesc(candidate) .. ':(%d+)')
      return port and (candidate .. ':' .. port) or candidate
    end
  end
  return ''
end

--- Items whose name, display name or covered hosts mention `needle`.
--- Case-insensitive: hostnames are, and two real hosts differing only by case
--- do not exist.
function M.items_matching(items, needle)
  local wanted = needle:lower()
  local bare = wanted:gsub(':%d+$', '')
  local out = {}
  for _, item in ipairs(items) do
    local hosts = (item.labels and item.labels.hosts or ''):lower()
    local name = tostring(item.name):lower()
    if
      name == wanted
      or name == bare
      or item.display:lower() == wanted
      or (',' .. hosts .. ','):find(',' .. bare .. ',', 1, true)
    then
      out[#out + 1] = item
    end
  end
  return out
end

return M
