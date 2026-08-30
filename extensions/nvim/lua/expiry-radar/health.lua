-- :checkhealth expiry-radar
--
-- Deliberately not a source-by-source inventory: a collection already reports
-- every source it ran, and a second copy of that list here would drift. What a
-- collection cannot tell you is why it produced nothing at all -- no binary, no
-- config, a source enabled with no credentials in the environment to reach it
-- -- so that is what this checks.

local core = require('expiry-radar.core')

local M = {}

local function read_json(path)
  local ok, lines = pcall(vim.fn.readfile, path)
  if not ok then
    return nil, 'could not read it'
  end
  local decoded_ok, decoded = pcall(vim.json.decode, table.concat(lines, '\n'))
  if not decoded_ok or type(decoded) ~= 'table' then
    return nil, 'it is not valid JSON'
  end
  return decoded
end

function M.check()
  vim.health.start('expiry-radar')

  local radar = require('expiry-radar')
  local cfg = radar.config() or require('expiry-radar.config').resolve({})

  if vim.fn.has('nvim-0.11') ~= 1 then
    vim.health.error('Neovim 0.11 or newer is required (vim.system, vim.fs.root, vim.validate).')
  else
    vim.health.ok('Neovim ' .. tostring(vim.version()))
  end

  local root = radar.root()
  vim.health.info('project root: ' .. tostring(root))

  local cmd, why = core.resolve_cmd(root, cfg)
  if not cmd then
    vim.health.error(why, {
      'Install it: ' .. core.INSTALL_COMMAND,
      'Or build it in a checkout: make build',
      "Or point the plugin at it: require('expiry-radar').setup({ cmd = { '/path/to/expiry-radar' } })",
    })
  else
    local shown = table.concat(cmd, ' ')
    -- There is no --version; the usage text is the cheapest proof that the
    -- thing on disk is the CLI we are about to trust with the list.
    local ok, out = pcall(function()
      return vim.system(vim.list_extend(vim.list_slice(cmd, 1, #cmd), { '-h' }), { text = true }):wait(20000)
    end)
    local help = ok and ((out.stdout or '') .. (out.stderr or '')) or ''
    if not ok then
      vim.health.error(('`%s` did not run: %s'):format(shown, tostring(out)))
    elseif not help:match('expiry%-radar') then
      vim.health.warn(('`%s` ran, but does not look like expiry-radar'):format(shown))
    else
      vim.health.ok(('`%s` runs'):format(shown))
    end
  end

  local config_path = core.resolve_config(root, cfg)
  if config_path == '' then
    local expected = cfg.config_path ~= '' and cfg.config_path or 'expiry-radar.json'
    if #cfg.endpoints > 0 or #cfg.domains > 0 then
      vim.health.info(('no config file at %s — running on setup() options alone'):format(expected))
    else
      vim.health.warn(('no config file at %s, and no endpoints or domains configured'):format(expected), {
        'Nothing is enabled implicitly: without one of these there are no sources to run.',
        'Copy expiry-radar.example.json to expiry-radar.json, or run :ExpiryRadarConfig.',
      })
    end
  else
    vim.health.ok('config: ' .. config_path)
    M.check_config(config_path)
  end

  if cfg.collect.within_days > 0 then
    vim.health.info(
      ('collect.within_days is %d — anything further out is not collected'):format(cfg.collect.within_days)
    )
  end
  if cfg.collect.min_priority > 0 then
    vim.health.info(
      ('collect.min_priority is %s — lower-ranked items are not collected'):format(tostring(cfg.collect.min_priority))
    )
  end

  local snapshot = radar.snapshot()
  if snapshot and #snapshot.warnings > 0 then
    vim.health.warn(('the last collection lost %d source(s)'):format(#snapshot.warnings), snapshot.warnings)
  end
end

--- Credentials never come from the config file -- they come from the
--- environment -- so an enabled source with an empty environment is the most
--- common way to get a clean-looking report missing an entire account.
function M.check_config(config_path)
  local parsed, err = read_json(config_path)
  if not parsed then
    return vim.health.error('the config file could not be used: ' .. err)
  end

  local endpoints = #(parsed.endpoints or {})
  local domains = #(parsed.domains or {})
  if endpoints > 0 then
    vim.health.ok(('%d endpoint(s) to probe over TLS'):format(endpoints))
  end
  if domains > 0 then
    vim.health.ok(('%d domain(s) to check via RDAP'):format(domains))
  end

  local k8s = parsed.k8s or {}
  if k8s.enabled then
    if k8s.server and k8s.server ~= '' then
      vim.health.ok('kubernetes: ' .. k8s.server)
    elseif vim.env.KUBERNETES_SERVICE_HOST then
      vim.health.ok('kubernetes: in-cluster')
    else
      vim.health.warn('kubernetes is enabled with no server, and this is not a cluster pod', {
        'Run `kubectl proxy` and set "server": "http://127.0.0.1:8001", or run in-cluster.',
      })
    end
  end

  local vault = parsed.vault or {}
  if vault.enabled then
    local addr = (vault.addr ~= '' and vault.addr) or vim.env.VAULT_ADDR
    if not addr or addr == '' then
      vim.health.warn('vault is enabled with no addr and no $VAULT_ADDR')
    elseif not vim.env.VAULT_TOKEN then
      vim.health.warn(('vault is enabled (%s) but $VAULT_TOKEN is not set'):format(addr))
    else
      vim.health.ok('vault: ' .. addr)
    end
  end

  local aws = parsed.aws or {}
  if aws.enabled then
    local credentialed = vim.env.AWS_ACCESS_KEY_ID
      or vim.env.AWS_PROFILE
      or vim.env.AWS_ROLE_ARN
      or vim.env.AWS_WEB_IDENTITY_TOKEN_FILE
      or (aws.profile and aws.profile ~= '')
    if credentialed then
      vim.health.ok('aws: ' .. (aws.region ~= '' and aws.region or '$AWS_REGION'))
    else
      vim.health.warn('aws is enabled but there are no AWS credentials in the environment', {
        'The credential chain is read from the process Neovim was launched from.',
      })
    end
  end

  if endpoints == 0 and domains == 0 and not k8s.enabled and not vault.enabled and not aws.enabled then
    vim.health.error('the config file enables no sources at all', {
      'Every source is opt-in; nothing runs implicitly.',
    })
  end
end

return M
