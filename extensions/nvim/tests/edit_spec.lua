local core = require('expiry-radar.core')

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
    local text, line = core.add_to_array(before, 'endpoints', core.render_entry('endpoint', 'api.example.com'))
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
    local text = core.add_to_array(
      '{ "domains": ["example.com", "example.net"] }',
      'domains',
      core.render_entry('domain', 'example.org')
    )
    assert.equals('{ "domains": ["example.com", "example.net", "example.org"] }', text)
    assert.same({ 'example.com', 'example.net', 'example.org' }, reparse(text).domains)
  end)

  it('takes the first entry into an empty array without a stray comma', function()
    local text = core.add_to_array('{ "domains": [] }', 'domains', core.render_entry('domain', 'a.example'))
    assert.equals('{ "domains": ["a.example"] }', text)
    local multi = core.add_to_array('{\n  "domains": [\n  ]\n}\n', 'domains', '"a.example"')
    assert.same({ 'a.example' }, reparse(multi).domains)
  end)

  it('adds a missing key rather than replacing the object', function()
    local before = '{\n  "endpoints": [\n    { "host": "shop.example.com" }\n  ]\n}\n'
    local text, line = core.add_to_array(
      before,
      'manual',
      core.render_entry('manual', { name = 'acme-corp.co.uk', kind = 'domain', expires = '2027-03-01' })
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
      local text = core.add_to_array(before, 'domains', '"a.example"')
      assert.same({ 'a.example' }, reparse(text).domains, 'from ' .. vim.inspect(before))
    end
  end)

  it('preserves four-space and tab indentation', function()
    local spaces = core.add_to_array(
      '{\n    "domains": [\n        "a.example"\n    ]\n}\n',
      'domains',
      '"b.example"'
    )
    assert.is_truthy(spaces:find('\n        "b.example"', 1, true))
    local tabs = core.add_to_array('{\n\t"domains": [\n\t\t"a.example"\n\t]\n}\n', 'domains', '"b.example"')
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
    local text = core.add_to_array(before, 'endpoints', core.render_entry('endpoint', 'b.example'))
    local parsed = reparse(text)
    assert.equals(2, #parsed.endpoints)
    assert.same({ 'x.example' }, parsed.domains)
  end)

  it('reports the line the entry actually landed on', function()
    local text, line = core.add_to_array('{\n  "domains": [\n    "a.example"\n  ]\n}\n', 'domains', '"b.example"')
    assert.is_truthy(vim.split(text, '\n')[line]:find('b.example', 1, true))
  end)
end)

describe('render_entry', function()
  it('carries what ranking needs, and omits what it does not', function()
    local full = vim.json.decode(core.render_entry('manual', {
      name = 'code-signing',
      kind = 'tls_cert',
      expires = '2026-11-15',
      namespace = 'release',
    }))
    assert.same({ name = 'code-signing', kind = 'tls_cert', expires = '2026-11-15', namespace = 'release' }, full)
    -- An empty namespace is left out rather than written as "", which would
    -- show up as a "/name" prefix in every report.
    local bare = vim.json.decode(core.render_entry('manual', {
      name = 'a',
      kind = 'secret',
      expires = '2026-11-15',
      namespace = '  ',
    }))
    assert.is_nil(bare.namespace)
  end)

  it('trims, so a pasted hostname does not become a name with a space', function()
    assert.equals('"example.com"', core.render_entry('domain', '  example.com \n'))
    assert.equals('a.example', vim.json.decode(core.render_entry('endpoint', ' a.example ')).host)
  end)
end)

describe('invalid_expires', function()
  it('accepts what the CLI accepts', function()
    assert.is_nil(core.invalid_expires('2027-03-01'))
    assert.is_nil(core.invalid_expires('2027-03-01T15:04:05Z'))
    assert.is_nil(core.invalid_expires('2027-03-01T15:04:05+02:00'))
  end)

  it('rejects what the CLI would reject at load', function()
    assert.is_truthy(core.invalid_expires(''))
    assert.is_truthy(core.invalid_expires('next march'))
    assert.is_truthy(core.invalid_expires('03/01/2027'))
    -- Rolling over to 3 March rather than failing would record a deadline
    -- nobody chose.
    assert.is_truthy(core.invalid_expires('2027-02-31'))
  end)
end)
