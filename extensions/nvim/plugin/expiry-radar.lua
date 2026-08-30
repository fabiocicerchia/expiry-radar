-- User commands only.
--
-- Deliberately cheap: nothing here requires core.lua, so a session that never
-- runs a collection never loads the plugin.

if vim.g.loaded_expiry_radar then
  return
end
vim.g.loaded_expiry_radar = true

--- setup() is optional: a command used before it runs gets the defaults rather
--- than an error about a nil config.
local function ready()
  local radar = require('expiry-radar')
  if not radar.is_setup() then
    radar.setup({})
  end
  return radar
end

local command = vim.api.nvim_create_user_command

command('ExpiryRadar', function()
  ready().collect({ manual = true, reason = 'command' })
end, { desc = 'expiry-radar: refresh the inventory' })

command('ExpiryRadarReport', function()
  ready().report()
end, { desc = 'expiry-radar: the inventory, ranked by blast radius' })

command('ExpiryRadarList', function()
  ready().list()
end, { desc = 'expiry-radar: every item, in the quickfix list' })

command('ExpiryRadarFilter', function()
  ready().filter()
end, { desc = 'expiry-radar: one deadline window or kind, in the quickfix list' })

command('ExpiryRadarHover', function()
  ready().hover()
end, { desc = 'expiry-radar: what the inventory knows about the host under the cursor' })

command('ExpiryRadarProbe', function(opts)
  ready().probe(opts.args)
end, { nargs = '?', desc = 'expiry-radar: probe one host now, ignoring the config' })

command('ExpiryRadarExport', function(opts)
  local args = vim.split(vim.trim(opts.args), '%s+')
  ready().export(args[1] or '', args[2])
end, {
  nargs = '*',
  complete = function()
    return { 'html', 'ical', 'json', 'prometheus' }
  end,
  desc = 'expiry-radar: render a report to a file',
})

command('ExpiryRadarAdd', function()
  ready().add_item()
end, { desc = 'expiry-radar: record an endpoint, a domain, or something nothing can discover' })

command('ExpiryRadarConfig', function()
  ready().open_config()
end, { desc = 'expiry-radar: open (or create) the config file' })

command('ExpiryRadarCancel', function()
  ready().cancel()
end, { desc = 'expiry-radar: cancel the running collection' })

command('ExpiryRadarLog', function()
  ready().show_log()
end, { desc = 'expiry-radar: show the log' })
