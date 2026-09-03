local core = require('expiry-radar.core')
local edit = require('expiry-radar.edit')

--- Every result has to still be a config the CLI can load.
local function reparse(text)
  local ok, decoded = pcall(vim.json.decode, text)
  assert.is_true(ok, 'result is not valid JSON: ' .. text)
  return decoded
end

describe('add_to_array', function()
  it('appends to a multi-line array, indented like its siblings', function()
    local before = table.concat({
      '{',
      '  "endpoints": [',
      '    { "host": "shop.example.com" }',
      '  ]',
      '}',
      '',
    }, '\n')
    local text, line = edit.add_to_array(before, 'endpoints', edit.render_entry('endpoint', 'api.example.com'))
    assert.equals(
      table.concat({
        '{',
        '  "endpoints": [',
        '    { "host": "shop.example.com" },',
        '    {"host":"api.example.com"}',
        '  ]',
        '}',
        '',
      }, '\n'),
      text
    )
    assert.equals(4, line)
    assert.equals(2, #reparse(text).endpoints)
  end)

  it('keeps a one-line array on one line', function()
    -- Re-flowing ["a", "b"] across three lines to add a third is not the diff
    -- anybody asked for.
    local text = edit.add_to_array(
      '{ "domains": ["example.com", "example.net"] }',
      'domains',
      edit.render_entry('domain', 'example.org')
    )
    assert.equals('{ "domains": ["example.com", "example.net", "example.org"] }', text)
    assert.same({ 'example.com', 'example.net', 'example.org' }, reparse(text).domains)
  end)

  it('takes the first entry into an empty array without a stray comma', function()
    local text = edit.add_to_array('{ "domains": [] }', 'domains', edit.render_entry('domain', 'a.example'))
    assert.equals('{ "domains": ["a.example"] }', text)
    local multi = edit.add_to_array('{\n  "domains": [\n  ]\n}\n', 'domains', '"a.example"')
    assert.same({ 'a.example' }, reparse(multi).domains)
  end)

  it('adds a missing key rather than replacing the object', function()
    local before = '{\n  "endpoints": [\n    { "host": "shop.example.com" }\n  ]\n}\n'
    local text, line = edit.add_to_array(
      before,
      'manual',
      edit.render_entry('manual', { name = 'acme-corp.co.uk', kind = 'domain', expires = '2027-03-01' })
    )
    assert.is_truthy(vim.split(text, '\n')[line]:find('acme-corp.co.uk', 1, true))
    local parsed = reparse(text)
    -- The existing key survives: this is the case where a naive rewrite loses
    -- everything the operator already had.
    assert.equals(1, #parsed.endpoints)
    assert.equals('acme-corp.co.uk', parsed.manual[1].name)
    assert.equals('2027-03-01', parsed.manual[1].expires)
  end)

  it('turns an empty or absent config into a config', function()
    for _, before in ipairs({ '', '   \n', '{}', '{\n}\n' }) do
      local text = edit.add_to_array(before, 'domains', '"a.example"')
      assert.same({ 'a.example' }, reparse(text).domains, 'from ' .. vim.inspect(before))
    end
  end)

  it('preserves four-space and tab indentation', function()
    local spaces = edit.add_to_array(
      '{\n    "domains": [\n        "a.example"\n    ]\n}\n',
      'domains',
      '"b.example"'
    )
    assert.is_truthy(spaces:find('\n        "b.example"', 1, true))
    local tabs = edit.add_to_array('{\n\t"domains": [\n\t\t"a.example"\n\t]\n}\n', 'domains', '"b.example"')
    assert.is_truthy(tabs:find('\n\t\t"b.example"', 1, true))
  end)

  it('is not confused by a bracket inside a value', function()
    local before = table.concat({
      '{',
      '  "endpoints": [',
      '    { "host": "a.example", "labels": { "note": "]}" } }',
      '  ],',
      '  "domains": ["x.example"]',
      '}',
      '',
    }, '\n')
    local text = edit.add_to_array(before, 'endpoints', edit.render_entry('endpoint', 'b.example'))
    local parsed = reparse(text)
    assert.equals(2, #parsed.endpoints)
    assert.same({ 'x.example' }, parsed.domains)
  end)

  it('reports the line the entry actually landed on', function()
    local text, line = edit.add_to_array('{\n  "domains": [\n    "a.example"\n  ]\n}\n', 'domains', '"b.example"')
    assert.is_truthy(vim.split(text, '\n')[line]:find('b.example', 1, true))
  end)
end)

describe('render_entry', function()
  it('carries what ranking needs, and omits what it does not', function()
    local full = vim.json.decode(edit.render_entry('manual', {
      name = 'code-signing',
      kind = 'tls_cert',
      expires = '2026-11-15',
      namespace = 'release',
    }))
    assert.same({ name = 'code-signing', kind = 'tls_cert', expires = '2026-11-15', namespace = 'release' }, full)
    -- An empty namespace is left out rather than written as "", which would
    -- show up as a "/name" prefix in every report.
    local bare = vim.json.decode(edit.render_entry('manual', {
      name = 'a',
      kind = 'secret',
      expires = '2026-11-15',
      namespace = '  ',
    }))
    assert.is_nil(bare.namespace)
  end)

  it('trims, so a pasted hostname does not become a name with a space', function()
    assert.equals('"example.com"', edit.render_entry('domain', '  example.com \n'))
    assert.equals('a.example', vim.json.decode(edit.render_entry('endpoint', ' a.example ')).host)
  end)
end)

describe('invalid_expires', function()
  it('accepts what the CLI accepts', function()
    assert.is_nil(edit.invalid_expires('2027-03-01'))
    assert.is_nil(edit.invalid_expires('2027-03-01T15:04:05Z'))
    assert.is_nil(edit.invalid_expires('2027-03-01T15:04:05+02:00'))
  end)

  it('rejects what the CLI would reject at load', function()
    assert.is_truthy(edit.invalid_expires(''))
    assert.is_truthy(edit.invalid_expires('next march'))
    assert.is_truthy(edit.invalid_expires('03/01/2027'))
    -- Rolling over to 3 March rather than failing would record a deadline
    -- nobody chose.
    assert.is_truthy(edit.invalid_expires('2027-02-31'))
  end)
end)

describe('remove_entry', function()
  local function reparse(text)
    local ok, decoded = pcall(vim.json.decode, text)
    assert.is_true(ok, 'result is not valid JSON: ' .. text)
    return decoded
  end

  it('removes the middle of a multi-line array and leaves valid JSON', function()
    local before = table.concat({
      '{',
      '  "endpoints": [',
      '    { "host": "a.example" },',
      '    { "host": "b.example" },',
      '    { "host": "c.example" }',
      '  ]',
      '}',
      '',
    }, '\n')
    local text = edit.remove_entry(before, 'endpoints', 4, 15)
    assert.same({ { host = 'a.example' }, { host = 'c.example' } }, reparse(text).endpoints)
    assert.is_nil(text:find('\n%s*\n'))
  end)

  it('takes the comma before the last element, not after', function()
    local before = '{\n  "endpoints": [\n    { "host": "a.example" },\n    { "host": "b.example" }\n  ]\n}\n'
    assert.same({ { host = 'a.example' } }, reparse(edit.remove_entry(before, 'endpoints', 4, 15)).endpoints)
  end)

  it('leaves an empty array rather than a broken one', function()
    local text = edit.remove_entry('{\n  "domains": [\n    "a.example"\n  ]\n}\n', 'domains', 3, 5)
    assert.same({}, reparse(text).domains)
  end)

  it('removes a multi-line entry whole', function()
    local before = table.concat({
      '{',
      '  "manual": [',
      '    {',
      '      "name": "code-signing",',
      '      "kind": "tls_cert",',
      '      "expires": "2026-11-15"',
      '    },',
      '    { "name": "other", "kind": "secret", "expires": "2027-01-01" }',
      '  ]',
      '}',
      '',
    }, '\n')
    local parsed = reparse(edit.remove_entry(before, 'manual', 4, 15))
    assert.equals(1, #parsed.manual)
    assert.equals('other', parsed.manual[1].name)
  end)

  it('takes one entry from a one-line config, not the whole document', function()
    -- Addressing by line alone would take the document's own opening brace here
    -- and delete everything. The column is what separates the two entries.
    local before = '{ "domains": ["a.example", "b.example"] }'
    assert.same({ 'b.example' }, reparse(edit.remove_entry(before, 'domains', 1, 15)).domains)
    assert.same({ 'a.example' }, reparse(edit.remove_entry(before, 'domains', 1, 28)).domains)
  end)

  it('refuses a position that names no entry', function()
    local before = '{\n  "domains": [\n    "a.example"\n  ]\n}\n'
    assert.is_nil(edit.remove_entry(before, 'domains', 999, 1))
    assert.is_nil(edit.remove_entry(before, 'domains', 1, 1))
    assert.is_nil(edit.remove_entry(before, 'endpoints', 3, 5))
  end)

  it('never reaches outside the array it was given', function()
    local before = table.concat({
      '{',
      '  "endpoints": [{ "host": "a.example" }],',
      '  "domains": ["keep.example"],',
      '  "manual": [{ "name": "keep", "kind": "secret", "expires": "2027-01-01" }]',
      '}',
      '',
    }, '\n')
    local parsed = reparse(edit.remove_entry(before, 'endpoints', 2, 27))
    assert.same({}, parsed.endpoints)
    assert.same({ 'keep.example' }, parsed.domains)
    assert.equals(1, #parsed.manual)
  end)
end)

describe('array_for_source', function()
  it('maps a recorded source to its array, and discovery to none', function()
    assert.equals('endpoints', core.array_for_source('tls:endpoint'))
    assert.equals('domains', core.array_for_source('domain:rdap'))
    assert.equals('manual', core.array_for_source('manual'))
    for _, discovered in ipairs({ 'k8s:secret', 'aws:acm', 'vault:pki_int', 'tls:chain' }) do
      assert.is_nil(core.array_for_source(discovered), discovered)
    end
  end)
end)

describe('declared_in', function()
  it('places a manual entry on the line that records it', function()
    local config = table.concat({
      '{',
      '  "manual": [',
      '    { "name": "acme-corp.co.uk", "kind": "domain", "expires": "2027-03-01" }',
      '  ]',
      '}',
    }, '\n')
    local found = edit.declared_in(config)
    assert.equals(3, found['acme-corp.co.uk'].line)
  end)
end)
