-- What a collection leaves behind.
--
-- init.lua is the collection policy: it runs the CLI, decides what a non-zero
-- exit means, and is the only thing that ever sets the snapshot the statusline,
-- the list and the filter all read. None of that is reachable without running
-- something, so these specs run a stand-in CLI out of a temporary project --
-- fixed report, fixed failed source, nothing off the machine -- and assert on
-- what the public API then returns.

local here = vim.fn.fnamemodify(debug.getinfo(1, 'S').source:sub(2), ':p:h')
local support = dofile(here .. '/support.lua')

local radar = require('expiry-radar')

local REFUSED = 'tls: 127.0.0.1:1: connection refused'

local EXPIRED = support.item()
-- Discovered, not recorded: nothing wrote it into the config, so it must come
-- back without an origin however the config happens to be laid out.
local DISCOVERED = support.item({
  name = 'api-tls',
  kind = 'secret',
  source = 'k8s:secret',
  expires = '2027-03-01T00:00:00Z',
  daysLeft = 200,
  expired = false,
  priority = 0.4,
  blastRadius = 0.5,
  why = 'namespace payments',
})

local function report(items)
  return vim.json.encode({
    generatedAt = '2026-01-04T00:00:00Z',
    count = #items,
    expired = 1,
    items = items,
  })
end

--- A project, a stand-in CLI, and setup() pointed at it.
local function setup(opts)
  opts = opts or {}
  local dir = support.project(
    opts.config or { endpoints = { { host = 'shop.example.com' } }, domains = { 'example.com' } }
  )
  local cmd = support.fake_cli(dir, {
    report = opts.report or report({ EXPIRED, DISCOVERED }),
    stderr = opts.stderr == nil and ('expiry-radar: warning: ' .. REFUSED) or opts.stderr,
    ical = opts.ical,
    code = opts.code or 3,
  })
  vim.uv.chdir(dir)
  -- An unnamed buffer, so vim.fs.root() answers from the working directory
  -- rather than from whatever a previous spec left open.
  vim.cmd('enew')
  radar.setup({ cmd = { cmd }, collect = { trigger = 'manual', on_startup = false } })
  return dir
end

--- One collection, run to completion.
local function collect()
  local outcome
  radar.collect({
    manual = true,
    reason = 'spec',
    on_done = function(ok)
      outcome = ok
    end,
  })
  support.wait(function()
    return outcome ~= nil
  end, 'the collection to finish')
  return outcome
end

describe('collect', function()
  it('turns a partial report into rows, worst deadline first', function()
    setup()
    assert.is_true(collect())
    local items = radar.items()
    assert.equals(2, #items)
    assert.equals('shop.example.com', items[1].name)
    assert.equals('expired', items[1].severity)
    assert.equals('api-tls', items[2].name)
    assert.equals('ok', items[2].severity)
  end)

  it('places a declared item on the config line that asked for it', function()
    setup()
    collect()
    -- Line 1 of a single-line JSON config: the point is that an origin exists
    -- for what the config recorded and not for anything else.
    assert.is_table(radar.items()[1].origin)
    assert.is_nil(radar.items()[2].origin)
  end)

  it('keeps the sources that failed, so an incomplete run cannot look clean', function()
    setup()
    collect()
    assert.same({ REFUSED }, radar.snapshot().warnings)
    local logged = table.concat(radar.log_text(), '\n')
    assert.is_truthy(logged:find('source failed: ' .. REFUSED, 1, true))
  end)

  it('exit 2 is a failure, and leaves the last good inventory alone', function()
    setup()
    collect()
    local before = radar.snapshot()

    local seen, restore = support.capture_notify()
    setup({ report = '', code = 2, stderr = 'expiry-radar: no sources configured' })
    assert.is_false(collect())
    restore()

    assert.equals(before, radar.snapshot())
    assert.equals(vim.log.levels.ERROR, seen[#seen].level)
    assert.is_truthy(seen[#seen].message:find('exit 2', 1, true))
  end)

  it('a report that cannot be decoded is a failure, not an empty inventory', function()
    local seen, restore = support.capture_notify()
    setup({ report = 'this is not json', code = 0 })
    assert.is_false(collect())
    restore()
    assert.is_truthy(seen[#seen].message:find('could not be read', 1, true))
  end)
end)

describe('statusline', function()
  it('shows the soonest deadline, and says when the run lost a source', function()
    setup()
    collect()
    assert.equals('✗ radar expired 3d ago (incomplete)', radar.statusline())
  end)

  it('marks a deadline inside the warning window without calling it expired', function()
    setup({ report = report({ support.item({ daysLeft = 5, expired = false }) }), stderr = false })
    collect()
    assert.equals('! radar 5d', radar.statusline())
  end)

  it('stays neutral for a deadline outside the warning window', function()
    setup({ report = report({ support.item({ daysLeft = 40, expired = false }) }), stderr = false })
    collect()
    assert.equals('· radar 40d', radar.statusline())
  end)

  it('says nothing came back rather than showing an empty count as fine', function()
    setup({ report = report({}) })
    collect()
    assert.equals('! radar 0 (incomplete)', radar.statusline())
  end)
end)

describe('export', function()
  it('writes exactly the bytes the CLI produced, and none of its own', function()
    local ICAL = 'BEGIN:VCALENDAR\nEND:VCALENDAR\n'
    local dir = setup({ ical = 'BEGIN:VCALENDAR\\nEND:VCALENDAR\\n' })
    local target = vim.fs.joinpath(dir, 'out.ics')
    local seen, restore = support.capture_notify()
    radar.export('ical', target)
    support.wait(function()
      return vim.uv.fs_stat(target) ~= nil
    end, 'the export to be written')
    restore()
    assert.same({ 'BEGIN:VCALENDAR', 'END:VCALENDAR', '' }, vim.fn.readfile(target, 'b'))
    -- A line-mode write would append a newline the document does not have.
    assert.equals(#ICAL, vim.fn.getfsize(target))
    assert.is_truthy(seen[#seen].message:find(target, 1, true))
  end)

  it('refuses a format the CLI does not render, without running anything', function()
    setup()
    local seen, restore = support.capture_notify()
    radar.export('pdf')
    restore()
    assert.equals(vim.log.levels.ERROR, seen[1].level)
    assert.is_truthy(seen[1].message:find('unknown format', 1, true))
  end)
end)

describe('filter', function()
  it('offers only the severities and kinds the inventory actually has', function()
    setup()
    collect()
    local offered
    local original = vim.ui.select
    vim.ui.select = function(choices, opts, on_choice)
      offered = vim.tbl_map(function(c)
        return string.format('%s:%s:%d', c.kind, c.key, c.count)
      end, choices)
      on_choice(nil, nil)
      -- Formatting is part of what is offered: a label nobody can read is a
      -- prompt nobody can answer.
      offered.first_label = opts.format_item(choices[1])
    end
    radar.filter()
    vim.ui.select = original

    assert.same({ 'severity:expired:1', 'severity:ok:1' }, { offered[1], offered[2] })
    local kinds = { offered[3], offered[4] }
    table.sort(kinds)
    assert.same({ 'kind:secret:1', 'kind:tls_cert:1' }, kinds)
    -- The human label, not the key: a prompt showing `expired` and `tls_cert`
    -- is the JSON, not a choice.
    assert.is_truthy(offered.first_label:find('Expired', 1, true))
  end)

  it('sends only the chosen severity to the quickfix list', function()
    setup()
    collect()
    local original = vim.ui.select
    vim.ui.select = function(choices, _opts, on_choice)
      on_choice(choices[1], 1)
    end
    radar.filter()
    vim.ui.select = original

    local qf = vim.fn.getqflist()
    assert.equals(1, #qf)
    assert.is_truthy(vim.fn.getqflist({ title = 0 }).title:find('expired', 1, true))
  end)
end)
