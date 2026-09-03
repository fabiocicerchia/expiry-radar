-- What :checkhealth expiry-radar reports.
--
-- The whole output of health.lua is the sequence of vim.health calls it makes,
-- so that sequence is what is asserted: recorded verbatim, with the parts that
-- name this machine (the Neovim version, the project path, the stand-in binary)
-- dropped, because they are facts about the box and not about the diagnosis.

local here = vim.fn.fnamemodify(debug.getinfo(1, 'S').source:sub(2), ':p:h')
local support = dofile(here .. '/support.lua')

local health = require('expiry-radar.health')
local radar = require('expiry-radar')

--- Run :checkhealth against a fixture, and return what it reported.
---@param usage string what the stand-in binary prints for -h
---@param config table|string|nil the config file to write, or nil for none
---@param opts table|nil setup() options
local function check(usage, config, opts)
  local dir = support.project(config)
  local cmd = support.fake_cli(dir, { report = usage })
  vim.uv.chdir(dir)
  vim.cmd('enew')
  radar.setup(vim.tbl_extend('force', {
    cmd = { cmd },
    collect = { trigger = 'manual', on_startup = false },
  }, opts or {}))

  local original = vim.health
  local seen = {}
  local function record(level)
    return function(message, advice)
      seen[#seen + 1] = level .. ': ' .. tostring(message)
      for _, line in ipairs(advice or {}) do
        seen[#seen + 1] = '    ' .. line
      end
    end
  end
  vim.health = {
    start = record('start'),
    ok = record('ok'),
    info = record('info'),
    warn = record('warn'),
    error = record('error'),
  }
  local ran, err = pcall(health.check)
  vim.health = original
  assert(ran, err)

  -- Drop the rows that describe this machine rather than this configuration.
  return vim.tbl_filter(function(line)
    return not line:match('^ok: Neovim ')
      and not line:match('^info: project root: ')
      and not line:match('^ok: `.*` runs$')
      and not line:match('^ok: config: ')
  end, seen), cmd
end

describe('check', function()
  it('reports both free sources when the config declares them', function()
    assert.same({
      'start: expiry-radar',
      'ok: 2 endpoint(s) to probe over TLS',
      'ok: 1 domain(s) to check via RDAP',
    }, check('expiry-radar — usage', {
      endpoints = { { host = 'a' }, { host = 'b' } },
      domains = { 'example.com' },
    }))
  end)

  it('does not vouch for something on disk that is not expiry-radar', function()
    local seen, cmd = check('this is git, actually', { domains = { 'example.com' } })
    assert.same({
      'start: expiry-radar',
      ('warn: `%s` ran, but does not look like expiry-radar'):format(cmd),
      'ok: 1 domain(s) to check via RDAP',
    }, seen)
  end)

  it('calls a config that enables nothing an error, not an empty inventory', function()
    assert.same({
      'start: expiry-radar',
      'error: the config file enables no sources at all',
      '    Every source is opt-in; nothing runs implicitly.',
    }, check('expiry-radar — usage', { endpoints = {}, domains = {} }))
  end)

  it('says a config it cannot parse is unusable, rather than saying nothing', function()
    assert.same({
      'start: expiry-radar',
      'error: the config file could not be used: it is not valid JSON',
    }, check('expiry-radar — usage', '{ not json'))
  end)

  it('warns per source when the environment has nothing to reach it with', function()
    local saved = {}
    for _, key in ipairs({
      'KUBERNETES_SERVICE_HOST',
      'VAULT_ADDR',
      'VAULT_TOKEN',
      'AWS_ACCESS_KEY_ID',
      'AWS_PROFILE',
      'AWS_ROLE_ARN',
      'AWS_WEB_IDENTITY_TOKEN_FILE',
    }) do
      saved[key] = vim.env[key]
      vim.env[key] = nil
    end
    local seen = check('expiry-radar — usage', {
      k8s = { enabled = true },
      vault = { enabled = true },
      aws = { enabled = true, region = 'eu-west-1' },
    })
    for key, value in pairs(saved) do
      vim.env[key] = value
    end

    assert.same({
      'start: expiry-radar',
      'warn: kubernetes is enabled with no server, and this is not a cluster pod',
      '    Run `kubectl proxy` and set "server": "http://127.0.0.1:8001", or run in-cluster.',
      'warn: vault is enabled with no addr and no $VAULT_ADDR',
      'warn: aws is enabled but there are no AWS credentials in the environment',
      '    The credential chain is read from the process Neovim was launched from.',
    }, seen)
  end)

  it('explains an empty list when there is no config and nothing configured', function()
    assert.same({
      'start: expiry-radar',
      'warn: no config file at expiry-radar.json, and no endpoints or domains configured',
      '    Nothing is enabled implicitly: without one of these there are no sources to run.',
      '    Copy expiry-radar.example.json to expiry-radar.json, or run :ExpiryRadarConfig.',
    }, check('expiry-radar — usage', nil))
  end)

  it('names the collection options that are silently hiding rows', function()
    local seen = check('expiry-radar — usage', { domains = { 'example.com' } }, {
      collect = { within_days = 7, min_priority = 0.5, trigger = 'manual', on_startup = false },
    })
    assert.same({
      'info: collect.within_days is 7 — anything further out is not collected',
      'info: collect.min_priority is 0.5 — lower-ranked items are not collected',
    }, vim.list_slice(seen, #seen - 1, #seen))
  end)
end)
