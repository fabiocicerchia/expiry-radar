local core = require('expiry-radar.core')
local edit = require('expiry-radar.edit')
local config = require('expiry-radar.config')

local function cfg(opts)
  return config.resolve(opts)
end

local function item(over)
  return vim.tbl_extend('force', {
    priority = 0.5,
    blastRadius = 0.5,
    daysLeft = 40,
    expired = false,
    kind = 'tls_cert',
    name = 'shop.example.com',
    source = 'tls:endpoint',
    expires = '2026-10-09T12:00:00Z',
    why = 'public endpoint',
  }, over or {})
end

describe('argv', function()
  it('always passes the format and the collection budget', function()
    assert.same({ '-format', 'json', '-timeout', '120s' }, core.argv(cfg(), { format = 'json' }))
  end)

  it('passes a resolved config file', function()
    local args = core.argv(cfg(), { format = 'json', config_path = '/repo/expiry-radar.json' })
    assert.same({ '-format', 'json', '-config', '/repo/expiry-radar.json' }, vim.list_slice(args, 1, 4))
  end)

  it('adds configured endpoints and domains to the config, comma-joined', function()
    local args = core.argv(
      cfg({ endpoints = { 'a.example.com', 'b.example.com:8443' }, domains = { 'example.com' } }),
      { format = 'json', config_path = '/repo/expiry-radar.json' }
    )
    local at = vim.fn.index(args, '-endpoints')
    assert.equals('a.example.com,b.example.com:8443', args[at + 2])
    assert.equals('example.com', args[vim.fn.index(args, '-domains') + 2])
  end)

  it('drops the config, the options and the extra args for a one-off probe', function()
    local args = core.argv(
      cfg({ endpoints = { 'configured.example.com' }, extra_args = { '-fail-within', '7' } }),
      { format = 'json', ignore_config = true, endpoints = { 'probe.example.com' } }
    )
    assert.equals('probe.example.com', args[vim.fn.index(args, '-endpoints') + 2])
    assert.equals(-1, vim.fn.index(args, '-fail-within'))
  end)

  it('says "no config" out loud for a probe, rather than omitting the flag', function()
    -- -config defaults to expiry-radar.json relative to the working directory,
    -- so an omitted flag still reads the estate in any project that has one.
    local args = core.argv(cfg(), { format = 'json', ignore_config = true })
    assert.equals('', args[vim.fn.index(args, '-config') + 2])
  end)

  it('omits the flag entirely for a normal collection with no config file', function()
    assert.equals(-1, vim.fn.index(core.argv(cfg(), { format = 'json' }), '-config'))
  end)

  it('pushes the filters down to the CLI, and only when they are set', function()
    assert.equals(-1, vim.fn.index(core.argv(cfg(), { format = 'json' }), '-within'))
    local args = core.argv(cfg({ collect = { within_days = 45, min_priority = 0.3 } }), { format = 'json' })
    assert.equals('45', args[vim.fn.index(args, '-within') + 2])
    assert.equals('0.3', args[vim.fn.index(args, '-min-priority') + 2])
  end)

  it('puts extra args last, so a user can override what we chose', function()
    local args = core.argv(cfg({ extra_args = { '-timeout', '5s' } }), { format = 'json' })
    assert.same({ '-timeout', '5s' }, vim.list_slice(args, #args - 1, #args))
  end)
end)

describe('has_sources', function()
  local project

  before_each(function()
    project = vim.fn.tempname()
    vim.fn.mkdir(project, 'p')
  end)

  after_each(function()
    vim.fn.delete(project, 'rf')
  end)

  it('is false for a project that has nothing to do with this tool', function()
    -- Every source is opt-in, and this is the common case: it is why nothing is
    -- collected on startup in an unrelated repository.
    assert.is_false(core.has_sources(project, cfg()))
  end)

  it('is true once anything is configured', function()
    assert.is_true(core.has_sources(project, cfg({ endpoints = { 'a.example.com' } })))
    assert.is_true(core.has_sources(project, cfg({ domains = { 'example.com' } })))
    vim.fn.writefile({ '{}' }, vim.fs.joinpath(project, 'expiry-radar.json'))
    assert.is_true(core.has_sources(project, cfg()))
  end)
end)

describe('severity', function()
  it('follows the deadline, on the report thresholds', function()
    assert.equals('expired', core.severity(-1))
    assert.equals('urgent', core.severity(0))
    assert.equals('urgent', core.severity(14))
    assert.equals('soon', core.severity(14.5))
    assert.equals('soon', core.severity(30))
    assert.equals('ok', core.severity(31))
  end)

  it('honours configured windows', function()
    assert.equals('soon', core.severity(5, 3, 10))
    assert.equals('ok', core.severity(11, 3, 10))
  end)
end)

describe('human_days', function()
  it('never says a cheerful zero for something already broken', function()
    assert.equals('expired 3d ago', core.human_days(-3.5))
    assert.equals('today', core.human_days(0.4))
    assert.equals('1d', core.human_days(1))
    assert.equals('41d', core.human_days(41.9))
  end)
end)

describe('display_name', function()
  it('prefixes the namespace exactly once', function()
    assert.equals('prod/web-tls', core.display_name({ name = 'web-tls', namespace = 'prod' }))
    assert.equals('prod/web-tls', core.display_name({ name = 'prod/web-tls', namespace = 'prod' }))
    assert.equals('shop.example.com', core.display_name({ name = 'shop.example.com' }))
  end)
end)

describe('kind_label', function()
  it('stays readable for a kind this build has never heard of', function()
    assert.equals('Intermediate CA', core.kind_label('intermediate_ca'))
    assert.equals('ssh host key', core.kind_label('ssh_host_key'))
  end)
end)

describe('normalize', function()
  it('orders worst deadline first, then by priority', function()
    local items = core.normalize({
      items = {
        item({ name = 'calm', daysLeft = 80, priority = 0.9 }),
        item({ name = 'broken', daysLeft = -1, priority = 0.1 }),
        item({ name = 'urgent-low', daysLeft = 3, priority = 0.2 }),
        item({ name = 'urgent-high', daysLeft = 9, priority = 0.8 }),
      },
    }, cfg())
    assert.same(
      { 'broken', 'urgent-high', 'urgent-low', 'calm' },
      vim.tbl_map(function(i)
        return i.name
      end, items)
    )
  end)

  it('places only the items the config file declared', function()
    local declared = { ['shop.example.com'] = { line = 3, column = 15 } }
    local items = core.normalize({
      items = {
        item(),
        -- Same name, but discovered on an Ingress: nothing in the repository
        -- declared it, so there is no honest line to point at.
        item({ source = 'k8s:ingress' }),
      },
    }, cfg(), declared)
    local by_source = {}
    for _, i in ipairs(items) do
      by_source[i.source] = i
    end
    assert.equals(3, by_source['tls:endpoint'].origin.line)
    assert.is_nil(by_source['k8s:ingress'].origin)
  end)

  it('handles an empty report', function()
    assert.same({}, core.normalize({ items = {} }, cfg()))
    assert.same({}, core.normalize({}, cfg()))
  end)
end)

describe('parse_warnings', function()
  it('picks the failed sources out of stderr', function()
    local stderr = table.concat({
      'expiry-radar: warning: aws: AccessDenied: not authorized',
      'some unrelated chatter',
      '  expiry-radar: warning: vault: 403 permission denied  ',
      'expiry-radar: something that is not a warning',
    }, '\n')
    assert.same(
      { 'aws: AccessDenied: not authorized', 'vault: 403 permission denied' },
      core.parse_warnings(stderr)
    )
  end)

  it('finds nothing in a clean run', function()
    assert.same({}, core.parse_warnings(''))
    assert.same({}, core.parse_warnings(nil))
    assert.same({}, core.parse_warnings('all good\n'))
  end)
end)

describe('declared_in', function()
  local CONFIG = table.concat({
    '{',
    '  "endpoints": [',
    '    { "host": "shop.example.com", "labels": { "traffic": "2400" } },',
    '    { "host": "admin.internal.example.com:8443" }',
    '  ],',
    '  "domains": ["example.com", "example.net"],',
    '  "k8s": { "enabled": true, "namespaces": ["prod"] },',
    '  "overrides": [{ "match": "payments/*", "blastRadius": 1.0 }]',
    '}',
  }, '\n')

  it('places an endpoint host on the line that declares it', function()
    local found = edit.declared_in(CONFIG)
    assert.equals(3, found['shop.example.com'].line)
    local line = vim.split(CONFIG, '\n')[3]
    assert.equals('"shop.example.com"', line:sub(found['shop.example.com'].column, found['shop.example.com'].column + 17))
  end)

  it('keeps the port, because the item name does', function()
    local found = edit.declared_in(CONFIG)
    assert.is_table(found['admin.internal.example.com:8443'])
    assert.is_nil(found['admin.internal.example.com'])
  end)

  it('places domains individually', function()
    local found = edit.declared_in(CONFIG)
    assert.equals(6, found['example.com'].line)
    assert.equals(6, found['example.net'].line)
    assert.is_true(found['example.net'].column > found['example.com'].column)
  end)

  it('scans only the two declared arrays', function()
    local found = edit.declared_in(CONFIG)
    assert.is_nil(found['prod'])
    assert.is_nil(found['payments/*'])
  end)

  it('does not let a bracket inside a value close the array early', function()
    local found = edit.declared_in(table.concat({
      '{',
      '  "endpoints": [',
      '    { "host": "a.example.com", "labels": { "note": "]}" } },',
      '    { "host": "b.example.com" }',
      '  ]',
      '}',
    }, '\n'))
    assert.equals(3, found['a.example.com'].line)
    assert.equals(4, found['b.example.com'].line)
  end)

  it('claims nothing about a config being typed', function()
    assert.same({}, edit.declared_in('{ "endpoints": [ { "host": "a.example.com" }'))
    assert.same({}, edit.declared_in('not json at all'))
  end)

  it('lets the first declaration of a duplicate win', function()
    local found = edit.declared_in('{\n"endpoints": [\n{"host": "a.example.com"},\n{"host": "a.example.com"}\n]}')
    assert.equals(3, found['a.example.com'].line)
  end)
end)

describe('host_at_cursor', function()
  it('reads a host out of the shapes it actually appears in', function()
    assert.equals('shop.example.com', core.host_at_cursor('    { "host": "shop.example.com" },'))
    assert.equals('shop.example.com', core.host_at_cursor('curl https://shop.example.com/checkout'))
    assert.equals('shop.example.com', core.host_at_cursor('  - host: shop.example.com'))
    assert.equals('admin.example.com:8443', core.host_at_cursor('"host": "admin.example.com:8443"'))
  end)

  it('says nothing rather than guessing', function()
    assert.equals('', core.host_at_cursor('func main() {'))
    assert.equals('', core.host_at_cursor(''))
  end)
end)

describe('items_matching', function()
  local items = core.normalize({
    items = {
      item({ name = 'shop.example.com' }),
      item({
        name = 'Issuing CA',
        kind = 'intermediate_ca',
        source = 'tls:chain',
        labels = { hosts = 'shop.example.com,api.example.com' },
      }),
      item({ name = 'other.example.com' }),
    },
  }, cfg())

  it('matches the name and the hosts a certificate covers', function()
    local matched = core.items_matching(items, 'shop.example.com')
    assert.equals(2, #matched)
  end)

  it('matches a host with a port against the name without one', function()
    assert.equals(2, #core.items_matching(items, 'shop.example.com:443'))
  end)

  it('does not match a host that only shares a suffix', function()
    assert.equals(0, #core.items_matching(items, 'example.com'))
  end)
end)
