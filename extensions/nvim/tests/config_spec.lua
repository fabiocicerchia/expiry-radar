local config = require('expiry-radar.config')

describe('config', function()
  it('returns the defaults when given nothing', function()
    local cfg = config.resolve()
    assert.is_true(cfg.enabled)
    assert.same({}, cfg.cmd)
    assert.equals('on_config_save_and_interval', cfg.collect.trigger)
    assert.equals(60, cfg.collect.interval_minutes)
  end)

  it('merges nested tables rather than replacing them', function()
    local cfg = config.resolve({ collect = { trigger = 'manual' } })
    assert.equals('manual', cfg.collect.trigger)
    -- Everything else in `collect` survives, which is the whole point: a user
    -- who sets one field must not silently lose the other nine.
    assert.equals(60, cfg.collect.interval_minutes)
    assert.equals(120000, cfg.collect.timeout_ms)
  end)

  it('replaces lists wholesale, because a merged list is nobody`s intent', function()
    local cfg = config.resolve({ domains = { 'example.net' } })
    assert.same({ 'example.net' }, cfg.domains)
  end)

  it('rejects a trigger that does not exist', function()
    assert.has_error(function()
      config.resolve({ collect = { trigger = 'on_keystroke' } })
    end)
  end)

  it('rejects a debounce short enough to hammer a registry', function()
    assert.has_error(function()
      config.resolve({ collect = { debounce_ms = 10 } })
    end)
  end)

  it('rejects a priority floor outside 0..1', function()
    assert.has_error(function()
      config.resolve({ collect = { min_priority = 2 } })
    end)
  end)

  it('rejects a cmd that is not a list', function()
    assert.has_error(function()
      config.resolve({ cmd = 'expiry-radar' })
    end)
  end)

  it('widens an info window that would swallow the warning window', function()
    -- Otherwise the warnings this is meant to sit outside of are dropped: an
    -- item at 20 days would be neither a warning nor an info.
    local cfg = config.resolve({ diagnostics = { warn_within_days = 30, info_within_days = 7 } })
    assert.equals(30, cfg.diagnostics.info_within_days)
  end)
end)
