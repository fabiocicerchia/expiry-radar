local config = require('expiry-radar.config')
local core = require('expiry-radar.core')
local ui = require('expiry-radar.ui')

local function cfg(opts)
  return config.resolve(opts)
end

local function item(over)
  return vim.tbl_extend('force', {
    priority = 0.7,
    blastRadius = 0.8,
    daysLeft = 5,
    expired = false,
    kind = 'tls_cert',
    name = 'shop.example.com',
    source = 'tls:endpoint',
    expires = '2026-09-04T12:00:00Z',
    why = 'public endpoint, 2400 req/s',
  }, over or {})
end

local function snapshot(items, warnings)
  return {
    items = core.normalize({ items = items }, cfg(), { ['shop.example.com'] = { line = 3, column = 15 } }),
    warnings = warnings or {},
    config_path = '/repo/expiry-radar.json',
    at = os.time(),
  }
end

describe('message', function()
  it('says what expires, when, and why it is ranked where it is', function()
    local text = ui.message(core.normalize({ items = { item() } }, cfg())[1])
    assert.is_true(vim.startswith(text, 'shop.example.com expires in 5d.'))
    assert.is_truthy(text:find('public endpoint, 2400 req/s', 1, true))
    assert.is_truthy(text:find('blast radius: 0.80', 1, true))
  end)

  it('says how long an expired item has been broken', function()
    local text = ui.message(core.normalize({ items = { item({ daysLeft = -3.2 }) } }, cfg())[1])
    assert.is_true(vim.startswith(text, 'shop.example.com expired 3 day(s) ago.'))
  end)
end)

describe('publish', function()
  local buf

  before_each(function()
    buf = vim.api.nvim_create_buf(true, false)
    vim.api.nvim_buf_set_name(buf, '/repo/expiry-radar.json')
    vim.api.nvim_buf_set_lines(buf, 0, -1, false, { '{', '"endpoints": [', '{"host": "shop.example.com"}', ']}' })
  end)

  after_each(function()
    ui.clear()
    vim.api.nvim_buf_delete(buf, { force = true })
  end)

  local function published()
    return vim.diagnostic.get(buf, { namespace = ui.namespace })
  end

  it('maps the deadline onto a severity, and publishes nothing beyond the window', function()
    ui.publish(
      snapshot({
        item({ daysLeft = -1 }),
        item({ daysLeft = 5 }),
        item({ daysLeft = 20 }),
        item({ daysLeft = 90 }),
      }).items,
      cfg(),
      '/repo/expiry-radar.json'
    )
    assert.same({
      vim.diagnostic.severity.ERROR,
      vim.diagnostic.severity.WARN,
      vim.diagnostic.severity.INFO,
    }, vim.tbl_map(function(d)
      return d.severity
    end, published()))
  end)

  it('gives no squiggle to an item nothing in the repository declared', function()
    ui.publish(snapshot({ item({ source = 'k8s:ingress' }) }).items, cfg(), '/repo/expiry-radar.json')
    assert.equals(0, #published())
  end)

  it('can be turned off without emptying the list', function()
    local disabled = cfg({ diagnostics = { enabled = false } })
    ui.publish(snapshot({ item() }).items, disabled, '/repo/expiry-radar.json')
    assert.equals(0, #published())
  end)

  it('replaces the previous publish wholesale', function()
    ui.publish(snapshot({ item() }).items, cfg(), '/repo/expiry-radar.json')
    assert.equals(1, #published())
    ui.publish({}, cfg(), '/repo/expiry-radar.json')
    assert.equals(0, #published())
  end)

  it('places the squiggle on the declared line and column', function()
    ui.publish(snapshot({ item() }).items, cfg(), '/repo/expiry-radar.json')
    local diagnostic = published()[1]
    assert.equals(2, diagnostic.lnum) -- 1-based in the config, 0-based here.
    assert.equals(14, diagnostic.col)
    assert.equals('expiry-radar', diagnostic.source)
  end)
end)

describe('report_lines', function()
  it('puts the failed sources above the rows that did come back', function()
    local lines = ui.report_lines(snapshot({ item() }, { 'aws: AccessDenied' }))
    local warning_at, row_at
    for i, line in ipairs(lines) do
      if not warning_at and line:find('source(s) failed', 1, true) then
        warning_at = i
      end
      if not row_at and line:find('shop.example.com', 1, true) then
        row_at = i
      end
    end
    -- An inventory that quietly lost a source reads exactly like a clean
    -- estate, so the loss is never below the fold.
    assert.is_true(warning_at < row_at)
    assert.is_truthy(vim.tbl_filter(function(l)
      return l:find('aws: AccessDenied', 1, true) ~= nil
    end, lines)[1])
  end)

  it('says so when there is nothing, rather than showing an empty float', function()
    local lines = ui.report_lines(snapshot({}))
    assert.is_truthy(table.concat(lines, '\n'):find('Nothing expiring', 1, true))
  end)

  it('groups rows under the deadline that decides what to do today', function()
    local lines = ui.report_lines(snapshot({ item({ daysLeft = -1 }), item({ daysLeft = 90 }) }))
    local text = table.concat(lines, '\n')
    assert.is_truthy(text:find('Expired', 1, true))
    assert.is_truthy(text:find('Further out', 1, true))
  end)
end)

describe('to_quickfix', function()
  it('sends declared items to their line, and keeps the rest as text', function()
    local items = snapshot({ item(), item({ source = 'k8s:ingress', name = 'prod/web-tls' }) }).items
    ui.to_quickfix(items, '/repo/expiry-radar.json', 'expiry-radar')
    local list = vim.fn.getqflist()
    assert.equals(2, #list)
    local placed = vim.tbl_filter(function(entry)
      return entry.valid == 1
    end, list)
    assert.equals(1, #placed)
    assert.equals(3, placed[1].lnum)
    -- The undeclared one still carries its facts: it is in the inventory, it
    -- just has nowhere in this repository to jump to.
    local unplaced = vim.tbl_filter(function(entry)
      return entry.valid == 0
    end, list)
    assert.is_truthy(unplaced[1].text:find('prod/web-tls', 1, true))
  end)

  it('marks what is expired or nearly so as an error', function()
    ui.to_quickfix(snapshot({ item({ daysLeft = -1 }), item({ daysLeft = 90 }) }).items, '', 'expiry-radar')
    assert.same({ 'E', 'W' }, vim.tbl_map(function(entry)
      return entry.type
    end, vim.fn.getqflist()))
  end)
end)

describe('hover_lines', function()
  it('says whether anything actually looked, when nothing matched', function()
    local lines = ui.hover_lines('shop.example.com', {}, { warnings = { 'aws: AccessDenied' } })
    local text = table.concat(lines, '\n')
    -- "Clean" and "never checked" are not the same answer.
    assert.is_truthy(text:find('not proof of anything', 1, true))
    assert.is_truthy(text:find('aws: AccessDenied', 1, true))
  end)

  it('lists what matched, with the reason for its rank', function()
    local items = core.normalize({ items = { item() } }, cfg())
    local text = table.concat(ui.hover_lines('shop.example.com', items, { warnings = {} }), '\n')
    assert.is_truthy(text:find('public endpoint, 2400 req/s', 1, true))
    assert.is_truthy(text:find('blast radius: 0.80', 1, true))
  end)
end)
