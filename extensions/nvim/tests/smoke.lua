-- Drive the plugin against the real binary.
--
-- The specs prove the plugin is self-consistent and nothing about whether it
-- still agrees with the tool it drives. This runs `expiry-radar` for real: the
-- JSON shape, the `expiry-radar: warning:` framing that is the only evidence a
-- source failed, and the exit codes that decide whether there is a report to
-- read at all.
--
-- Offline by construction -- the one endpoint is a closed port on loopback --
-- so this needs a binary, not the internet.
--
--   nvim --headless --clean -u tests/smoke.lua
--
-- Set EXPIRY_RADAR_CMD to run something other than ./bin/expiry-radar.

local here = vim.fn.fnamemodify(vim.fn.resolve(debug.getinfo(1, 'S').source:sub(2)), ':p:h:h')
vim.opt.runtimepath:prepend(here)
vim.opt.swapfile = false

local failures = 0

local function check(name, ok, detail)
  if ok then
    io.stdout:write('ok    ' .. name .. '\n')
  else
    failures = failures + 1
    io.stdout:write('FAIL  ' .. name .. (detail and ('\n      ' .. tostring(detail)) or '') .. '\n')
  end
end

local function die(message)
  io.stderr:write('smoke: ' .. message .. '\n')
  vim.cmd('cq')
end

-- The repository this plugin ships in, so the binary `make build` writes is
-- found the same way a user's checkout would find it.
local repo = vim.fn.fnamemodify(here, ':h:h')
local binary = vim.env.EXPIRY_RADAR_CMD or vim.fs.joinpath(repo, 'bin', 'expiry-radar')
if vim.fn.executable(binary) ~= 1 then
  die(('no expiry-radar at %s — run `make build` at the repository root'):format(binary))
end

-- A project of its own, so nobody's real expiry-radar.json can influence this.
-- Port 2 is refused as instantly as port 1, so a probe that wrongly reads this
-- config shows up as an extra named failure rather than as a hang.
local CONFIGURED_HOST = '127.0.0.1:2'
local PROBED_HOST = '127.0.0.1:1'

local project = vim.fn.tempname()
vim.fn.mkdir(project, 'p')
local config_path = vim.fs.joinpath(project, 'expiry-radar.json')
vim.fn.writefile({
  '{',
  '  "endpoints": [',
  ('    { "host": "%s" }'):format(CONFIGURED_HOST),
  '  ]',
  '}',
}, config_path)
vim.cmd.cd(project)

local radar = require('expiry-radar')
radar.setup({
  cmd = { binary },
  -- Nothing automatic: this drives every run itself.
  collect = { trigger = 'manual', on_startup = false, timeout_ms = 20000 },
})

check('the project root is the temporary project', radar.root() == project, radar.root())
check('the config file resolves', require('expiry-radar.core').resolve_config(project, radar.config()) == config_path)

-- --- a collection that loses its only source ---------------------------------

local done = false
radar.collect({
  manual = true,
  reason = 'smoke',
  on_done = function(ok)
    done = ok
  end,
})
if not vim.wait(30000, function()
  return not radar.is_collecting() and done
end, 100) then
  die('the collection never finished')
end

local snapshot = radar.snapshot()
check('a collection produced a snapshot', snapshot ~= nil)
-- Port 1 on loopback refuses instantly: the TLS source fails, the CLI exits 3
-- with partial results, and the failure is on stderr rather than swallowed.
check('the failed source survived to the snapshot', #snapshot.warnings == 1, vim.inspect(snapshot.warnings))
check('the failure names the source', snapshot.warnings[1]:match('^tls:') ~= nil, snapshot.warnings[1])
check('nothing was invented to fill the gap', #snapshot.items == 0)

local report = table.concat(require('expiry-radar.ui').report_lines(snapshot), '\n')
check('the report says the inventory is incomplete', report:find('incomplete', 1, true) ~= nil, report)

-- A run that lost every source must never read as a clean estate, in the one
-- place that is always on screen.
check('the statusline admits it', radar.statusline():find('incomplete', 1, true) ~= nil, radar.statusline())

-- --- a probe must not read the config sitting next to it ---------------------

-- -config defaults to expiry-radar.json relative to the working directory, so
-- merely omitting the flag reads the estate anyway. A probe that collected
-- every configured source to answer a question about one hostname would be
-- slow, would hit every credential, and would bury the answer.
local core = require('expiry-radar.core')
local probe_argv = core.argv(radar.config(), {
  format = 'json',
  ignore_config = true,
  endpoints = { PROBED_HOST },
})
local probe = vim.system(
  vim.list_extend({ binary }, probe_argv),
  { text = true, cwd = project }
):wait(30000)
local probe_failures = table.concat(core.parse_warnings(probe.stderr), '\n')
check('the probe reached the host it was asked about', probe_failures:find(PROBED_HOST, 1, true) ~= nil, probe_failures)
check(
  'the probe ignored the config file next to it',
  probe_failures:find(CONFIGURED_HOST, 1, true) == nil,
  'the config leaked into the probe: ' .. probe_failures
)

-- --- an entry the plugin writes must be one the CLI can read -----------------

-- The two halves agree on a schema neither owns alone: the plugin renders JSON
-- and the CLI parses it. Unit tests on either side prove only that each is
-- self-consistent.
local recorded = vim.fn.tempname()
vim.fn.mkdir(recorded, 'p')
local recorded_config = vim.fs.joinpath(recorded, 'expiry-radar.json')
local text = ''
for _, entry in ipairs({
  { kind = 'domain', value = 'acme-corp.co.uk' },
  { kind = 'endpoint', value = PROBED_HOST },
  { kind = 'manual', value = { name = 'code-signing', kind = 'tls_cert', expires = '2027-03-01' } },
}) do
  text = core.add_to_array(text, core.ARRAY_FOR[entry.kind], core.render_entry(entry.kind, entry.value))
end
vim.fn.writefile(vim.split(text, '\n'), recorded_config, 'b')

local recorded_run = vim.system(
  { binary, '-format', 'json', '-config', recorded_config, '-within', '0' },
  { text = true, cwd = recorded }
):wait(30000)
-- Exit 2 is "bad usage or config": the one outcome that means the plugin wrote
-- something the CLI cannot load.
check('the CLI loads a config the plugin wrote', recorded_run.code ~= 2, recorded_run.stderr)
local decoded_ok, recorded_report = pcall(vim.json.decode, recorded_run.stdout or '')
check('the recorded config produced a report', decoded_ok and type(recorded_report) == 'table')
if decoded_ok then
  local names = {}
  for _, item in ipairs(recorded_report.items or {}) do
    names[item.name] = item.source
  end
  -- The manual item is the one nothing could have discovered, and the only one
  -- of the three that needs no network, so it is the one that proves recording
  -- works at all.
  check('the manually recorded item came back', names['code-signing'] == 'manual', vim.inspect(names))

  -- The other two arrays are proved by their sources having been *attempted*.
  -- Asserting that a TLS dial or an RDAP query succeeds is not something an
  -- offline test can do, and pretending otherwise would make this suite fail on
  -- a train rather than when something is broken.
  local attempted = (recorded_run.stderr or '') .. table.concat(vim.tbl_keys(names), ' ')
  check('the recorded endpoint was attempted', attempted:find(PROBED_HOST, 1, true) ~= nil, attempted)
  check('the recorded domain was attempted', attempted:find('acme%-corp%.co%.uk') ~= nil, attempted)
end
-- Recording is only half of managing: an entry has to come out again, and the
-- config has to still load afterwards.
local declared = core.declared_in(text)
local origin = declared['code-signing']
check('the recorded manual entry can be found again', origin ~= nil, vim.inspect(vim.tbl_keys(declared)))
if origin then
  local pruned = core.remove_entry(text, 'manual', origin.line, origin.column)
  check('the recorded entry could be removed', pruned ~= nil)
  if pruned then
    vim.fn.writefile(vim.split(pruned, '\n'), recorded_config, 'b')
    local after = vim.system(
      { binary, '-format', 'json', '-config', recorded_config },
      { text = true, cwd = recorded }
    ):wait(30000)
    check('the CLI still loads the config after a removal', after.code ~= 2, after.stderr)
    local ok_after, report_after = pcall(vim.json.decode, after.stdout or '')
    if ok_after then
      local still_there = false
      for _, item in ipairs(report_after.items or {}) do
        still_there = still_there or item.name == 'code-signing'
      end
      check('the removed item is gone from the report', not still_there)
    end
  end
end

vim.fn.delete(recorded, 'rf')

-- --- a run with no sources at all --------------------------------------------

vim.fn.delete(config_path)
local second = nil
radar.collect({
  manual = true,
  reason = 'smoke: no sources',
  on_done = function(ok)
    second = ok
  end,
})
if not vim.wait(30000, function()
  return second ~= nil
end, 100) then
  die('the second collection never finished')
end
-- An empty list here would say "nothing expires", which is the one thing this
-- tool must never say when it did not look. The plugin refuses before spawning
-- anything, which is the same answer the CLI's exit 2 gives, without the noise.
check('a run with no sources refuses, rather than reporting an empty inventory', second == false)
check('the previous snapshot was not replaced by the refusal', #radar.snapshot().warnings == 1)

local log = table.concat(radar.log_text(), '\n')
check('the log says why the second run did not happen', log:find('no sources configured', 1, true) ~= nil, log)
check('only the first run reached the binary', select(2, log:gsub('collect %(', '')) == 1, log)

vim.fn.delete(project, 'rf')
if failures > 0 then
  io.stderr:write(('smoke: %d check(s) failed\n'):format(failures))
  vim.cmd('cq')
end
io.stdout:write('smoke: all checks passed\n')
vim.cmd('qa!')
