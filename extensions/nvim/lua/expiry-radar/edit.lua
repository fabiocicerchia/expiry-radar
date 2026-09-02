-- The config file as text.
--
-- Reading an item's position and adding or removing an entry both edit the
-- file the operator wrote, so both are done on the text rather than by
-- decoding and re-encoding: a round trip through vim.json would reformat every
-- line, lose the comments-free layout somebody chose, and turn a one-line diff
-- into a whole-file one.

local M = {}

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
