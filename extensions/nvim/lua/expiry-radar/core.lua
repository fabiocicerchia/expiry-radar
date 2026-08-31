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

-- --- placing items in the config file ----------------------------------------

local function position_of(text, offset)
  local line, start = 1, 0
  for i = 1, offset - 1 do
    if text:sub(i, i) == '\n' then
      line = line + 1
      start = i
    end
  end
  return line, offset - start
end

--- The `[...]` that follows `"key"`, as 1-based offsets. String-aware, so a
--- bracket inside a value cannot close the array early.
--- File-local: both declared_in, which reads these arrays, and add_to_array,
--- which appends to them, come after it.
local function array_span(text, key)
  local at = text:find('"' .. key .. '"%s*:%s*%[')
  if not at then
    return nil
  end
  local open = text:find('%[', at)
  local depth, in_string = 0, false
  local i = open
  while i <= #text do
    local ch = text:sub(i, i)
    if in_string then
      if ch == '\\' then
        i = i + 1
      elseif ch == '"' then
        in_string = false
      end
    elseif ch == '"' then
      in_string = true
    elseif ch == '[' or ch == '{' then
      depth = depth + 1
    elseif ch == ']' or ch == '}' then
      depth = depth - 1
      if depth == 0 then
        return open, i
      end
    end
    i = i + 1
  end
  return nil -- Unterminated: the file is mid-edit, so claim nothing.
end

--- Item name -> where it is declared, for one config file.
---
--- Keyed by the item's `name` exactly as the CLI reports it: the TLS source
--- names a certificate after the `host` it was configured with (port and all),
--- and the RDAP source names a domain after the string in `domains`. That
--- equality is the whole mapping -- no normalisation, because a host that does
--- not round-trip is a host we would be guessing about.
---
--- Read as text rather than through a JSON decoder, because a position is
--- exactly what decoding throws away.
function M.declared_in(text)
  local found = {}
  local function record(value, offset)
    -- First declaration wins: a duplicated host is one item, and the earlier
    -- line is the one somebody will look at.
    if value ~= '' and not found[value] then
      local line, column = position_of(text, offset)
      found[value] = { line = line, column = column }
    end
  end

  local from, to = array_span(text, 'endpoints')
  if from then
    local section = text:sub(from, to)
    local at = 1
    while true do
      local s, e, value = section:find('"host"%s*:%s*"(.-)"', at)
      if not s then
        break
      end
      -- Point at the value, not at the key: that is what the squiggle is about.
      local quote = section:sub(s, e):find('"[^"]*"$')
      record(M.unescape(value), from + s - 1 + quote - 1)
      at = e + 1
    end
  end

  -- Manual entries are keyed by `name`, which is exactly what the CLI reports
  -- as the item's name -- the same equality the other two rely on.
  from, to = array_span(text, 'manual')
  if from then
    local section = text:sub(from, to)
    local at = 1
    while true do
      local start, stop, value = section:find('"name"%s*:%s*"(.-)"', at)
      if not start then
        break
      end
      local quote = section:sub(start, stop):find('"[^"]*"$')
      record(M.unescape(value), from + start - 1 + quote - 1)
      at = stop + 1
    end
  end

  from, to = array_span(text, 'domains')
  if from then
    local section = text:sub(from, to)
    local at = 1
    while true do
      local s, e, value = section:find('"(.-)"', at)
      if not s then
        break
      end
      record(M.unescape(value), from + s - 1)
      at = e + 1
    end
  end
  return found
end

--- JSON string escapes, which a host or domain can legitimately carry none of.
function M.unescape(raw)
  if not raw:find('\\') then
    return raw
  end
  local ok, decoded = pcall(vim.json.decode, '"' .. raw .. '"')
  return (ok and type(decoded) == 'string') and decoded or raw
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

-- --- recording an item -------------------------------------------------------

--- The config key each kind of recordable entry lives under. Everything else
--- is discovered, not recorded.
M.ARRAY_FOR = { endpoint = 'endpoints', domain = 'domains', manual = 'manual' }

--- The CLI's source.Kinds, in the order internal/rank weights them. The kind is
--- not cosmetic: it picks the base blast radius, which decides where a recorded
--- item lands in the ranking.
M.MANUAL_KINDS = {
  { kind = 'domain', label = 'Domain', hint = 'a registration -- the whole estate, including mail' },
  { kind = 'intermediate_ca', label = 'Intermediate CA', hint = 'every leaf it signed, at once' },
  { kind = 'tls_cert', label = 'TLS certificate', hint = 'a code-signing or client cert, say' },
  { kind = 'iam_access_key', label = 'IAM access key', hint = 'a key rotated by hand' },
  { kind = 'secret', label = 'Secret', hint = 'an API token, a password' },
  { kind = 'vault_lease', label = 'Vault lease', hint = 'a lease nothing enumerates' },
}

local function is_real_date(y, m, d)
  local t = os.date('!*t', os.time({ year = y, month = m, day = d, hour = 12 }))
  return t.year == y and t.month == m and t.day == d
end

--- The same rule source.ManualItem.ExpiresAt applies, so the editor rejects
--- what the CLI would reject rather than writing a config that fails to load.
---@return string|nil reason the value is unusable, or nil when it is fine
function M.invalid_expires(value)
  value = vim.trim(value or '')
  if value == '' then
    return 'a date is required'
  end
  local y, m, d = value:match('^(%d%d%d%d)%-(%d%d)%-(%d%d)$')
  if not y then
    -- RFC 3339 proper, rather than "anything with digits in it": a value the
    -- editor accepts and the CLI refuses is the worst of both.
    y, m, d = value:match('^(%d%d%d%d)%-(%d%d)%-(%d%d)[Tt]%d%d:%d%d:%d%d[%.%d]*([Zz]?[%+%-]?%d*:?%d*)$')
    if not y then
      return 'use YYYY-MM-DD, or a full RFC 3339 timestamp'
    end
    y, m, d = value:match('^(%d%d%d%d)%-(%d%d)%-(%d%d)')
  end
  if not is_real_date(tonumber(y), tonumber(m), tonumber(d)) then
    return value .. ' is not a real date'
  end
  return nil
end

--- One entry, rendered as the CLI's config expects it.
function M.render_entry(kind, value)
  if kind == 'domain' then
    return vim.json.encode(vim.trim(value))
  elseif kind == 'endpoint' then
    return vim.json.encode({ host = vim.trim(value) })
  end
  local out = {
    name = vim.trim(value.name),
    kind = value.kind,
    expires = vim.trim(value.expires),
  }
  -- An empty namespace is left out rather than written as "", which would show
  -- up as a "/name" prefix in every report.
  if value.namespace and vim.trim(value.namespace) ~= '' then
    out.namespace = vim.trim(value.namespace)
  end
  -- Key order, so the file reads the way the example does rather than however
  -- the encoder felt about hashing today.
  local parts = {}
  for _, key in ipairs({ 'name', 'kind', 'expires', 'namespace' }) do
    if out[key] then
      parts[#parts + 1] = string.format('%s:%s', vim.json.encode(key), vim.json.encode(out[key]))
    end
  end
  return '{' .. table.concat(parts, ',') .. '}'
end

--- The indentation of the line `offset` sits on.
local function indent_at(text, offset)
  local line_start = (text:sub(1, offset - 1):find('\n[^\n]*$') or 0) + 1
  return text:sub(line_start, offset - 1):match('^[ \t]*') or ''
end

local function line_of(text, offset)
  local _, count = text:sub(1, offset - 1):gsub('\n', '')
  return count + 1
end

--- Append `entry` to the array under `key`, creating the array -- and the
--- object around it -- when they are not there yet.
---
--- Text surgery rather than decode-then-encode: a round-trip through the JSON
--- encoder reformats the whole document and drops the ordering somebody chose.
--- An operator who added one host should get a one-line diff.
---@return string text, integer line the entry landed on
function M.add_to_array(text, key, entry)
  local open, close = array_span(text, key)
  if open then
    local body = text:sub(open + 1, close - 1)
    local empty = vim.trim(body) == ''
    -- A one-line array stays a one-line array: re-flowing ["a", "b"] across
    -- three lines to add a third element is not the diff anybody asked for.
    local inline = not body:find('\n')

    if inline then
      local insert_at = close
      local addition = empty and entry or (', ' .. entry)
      local next_text = text:sub(1, insert_at - 1) .. addition .. text:sub(insert_at)
      return next_text, line_of(next_text, insert_at)
    end

    local close_indent = indent_at(text, close)
    if empty then
      local indent = indent_at(text, open) .. '  '
      local next_text = text:sub(1, open)
        .. '\n'
        .. indent
        .. entry
        .. '\n'
        .. close_indent
        .. text:sub(close)
      return next_text, line_of(next_text, open + 2)
    end

    local trimmed = body:gsub('%s+$', '')
    local last_content = open + #trimmed
    local last_line = vim.trim(body):match('[^\n]*$')
    local indent = indent_at(text, last_content - #last_line + 1)
    local next_text = text:sub(1, last_content) .. ',\n' .. indent .. entry .. text:sub(last_content + 1)
    -- The newline just inserted sits at last_content + 2; line_of counts the
    -- newlines strictly before its offset, so the entry's own line needs one
    -- past it. Offsets here are 1-based, unlike the TypeScript twin.
    return next_text, line_of(next_text, last_content + 3)
  end

  -- No such array yet.
  local trimmed = vim.trim(text)
  if trimmed == '' or trimmed:sub(1, 1) ~= '{' then
    return ('{\n  %s: [%s]\n}\n'):format(vim.json.encode(key), entry), 2
  end
  local brace = text:match('.*()}')
  local before = text:sub(1, brace - 1):gsub('%s+$', '')
  local needs_comma = before:sub(-1) ~= ',' and vim.trim(before) ~= '{'
  local indent = indent_at(text, brace) .. '  '
  -- Same 1-based reasoning: the comma, when there is one, pushes the newline
  -- along by a character.
  local newline_at = #before + (needs_comma and 2 or 1)
  local next_text = before
    .. (needs_comma and ',' or '')
    .. '\n'
    .. indent
    .. vim.json.encode(key)
    .. ': ['
    .. entry
    .. ']\n'
    .. text:sub(brace)
  return next_text, line_of(next_text, newline_at + 1)
end

--- The offset of a 1-based line and column.
local function offset_of(text, line, column)
  local lines = vim.split(text, '\n')
  if line < 1 or line > #lines then
    return -1
  end
  local offset = 0
  for i = 1, line - 1 do
    offset = offset + #lines[i] + 1
  end
  return offset + column
end

--- The start and end offset of each element of an array, at its own depth only.
local function element_spans(text, open, close)
  local spans = {}
  local depth, in_string, start = 0, false, nil
  for i = open + 1, close - 1 do
    local ch = text:sub(i, i)
    if in_string then
      if ch == '\\' then
        i = i + 1
      elseif ch == '"' then
        in_string = false
      end
    else
      if depth == 0 and not start and not ch:match('%s') and ch ~= ',' then
        start = i
      end
      if ch == '"' then
        in_string = true
      elseif ch == '[' or ch == '{' then
        depth = depth + 1
      elseif ch == ']' or ch == '}' then
        depth = depth - 1
      elseif ch == ',' and depth == 0 and start then
        spans[#spans + 1] = { start, i - 1 }
        start = nil
      end
    end
  end
  if start then
    spans[#spans + 1] = { start, close - 1 }
  end
  for _, span in ipairs(spans) do
    -- Trailing whitespace belongs to the layout, not the element.
    span[2] = span[1] + #(text:sub(span[1], span[2]):gsub('%s+$', '')) - 1
  end
  return spans
end

--- Remove the entry recorded at `line`/`column` from the array under `key`.
---
--- Bounded to that array by construction: the element is picked from the
--- array's own elements rather than by balancing brackets out from a line.
--- Addressing by line alone looked simpler and would have deleted the entire
--- config on a one-line file, where line 1 begins with the document's own
--- opening brace.
---
--- Returns nil when the position names no element, which is the right answer
--- when the file has been edited since the collection that reported it.
function M.remove_entry(text, key, line, column)
  local open, close = array_span(text, key)
  if not open then
    return nil
  end
  local offset = offset_of(text, line, column)
  if offset < 0 or offset < open or offset > close then
    return nil
  end

  local from, to
  for _, span in ipairs(element_spans(text, open, close)) do
    if offset >= span[1] and offset <= span[2] then
      from, to = span[1], span[2]
      break
    end
  end
  if not from then
    return nil
  end

  -- Swallow the separator: the comma after it, or the one before it when this
  -- was the last element. Leaving either behind produces invalid JSON.
  local after_start, after_stop = text:find('^%s*,', to + 1)
  if after_start then
    to = after_stop
    local _, line_stop = text:find('^[ \t]*\n?', to + 1)
    if line_stop then
      to = line_stop
    end
  else
    local before_start = text:sub(1, from - 1):find(',%s*$')
    if before_start then
      from = before_start
    end
  end
  local indent_start = text:sub(1, from - 1):find('[ \t]*$')
  if indent_start then
    from = indent_start
  end

  return text:sub(1, from - 1) .. text:sub(to + 1)
end

return M
