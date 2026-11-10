@domain:editor-integrations @area:editor-settings @from:extensions/nvim
Feature: Neovim plugin settings
  Gives the operator safe defaults and rejects settings that would misbehave before any collection runs.

  Background:
    Given the operator uses the expiry-radar plugin in Neovim

  @id:editor-integrations.editor-settings.001 @rule:editor.settings.invalid-options-rejected @verified @source:extensions/nvim/lua/expiry-radar/config.lua#M.validate @test:extensions/nvim/tests/config_spec.lua#rejects a trigger that does not exist @test:extensions/nvim/tests/config_spec.lua#rejects a debounce short enough to hammer a registry @test:extensions/nvim/tests/config_spec.lua#rejects a priority floor outside 0..1 @test:extensions/nvim/tests/config_spec.lua#rejects a cmd that is not a list
  Scenario Outline: Settings that would misbehave are rejected at setup
    Given the operator's setup sets <option> to <value>
    When Neovim loads the plugin setup
    Then setup fails with a message that <option> must be <expected>

    Examples:
      | option                   | value          | expected                 |
      | collect.trigger          | on_keystroke   | one of the four triggers |
      | collect.debounce_ms      | 10             | a number >= 250          |
      | collect.min_priority     | 2              | a number between 0 and 1 |
      | cmd                      | "expiry-radar" | a list of strings        |
      | collect.interval_minutes | 0              | a number >= 1            |
      | collect.timeout_ms       | 5000           | a number >= 10000        |

  @id:editor-integrations.editor-settings.002 @rule:editor.settings.partial-settings-keep-defaults @verified @source:extensions/nvim/lua/expiry-radar/config.lua#merge @test:extensions/nvim/tests/config_spec.lua#merges nested tables rather than replacing them @test:extensions/nvim/tests/config_spec.lua#returns the defaults when given nothing
  Scenario: Setting one option keeps the defaults of the others
    Given the operator's setup sets only the collection trigger to manual
    When Neovim loads the plugin setup
    Then the trigger is manual
    And the refresh interval stays 60 minutes and the collection timeout stays 120 seconds

  @id:editor-integrations.editor-settings.003 @rule:editor.settings.info-window-never-narrower-than-warning @verified @source:extensions/nvim/lua/expiry-radar/config.lua#M.validate @test:extensions/nvim/tests/config_spec.lua#widens an info window that would swallow the warning window @questionable
  Scenario: An information window narrower than the warning window is widened to match
    Given the operator's setup sets the warning window to 30 days and the information window to 7 days
    When Neovim loads the plugin setup
    Then the information window is 30 days

  @id:editor-integrations.editor-settings.004 @rule:editor.settings.disabled-plugin-collects-nothing @inferred @source:extensions/nvim/lua/expiry-radar/init.lua#M.setup @questionable
  Scenario: A disabled plugin never collects and clears its squiggles
    Given the operator's setup disables the plugin
    When the operator opens the inventory report
    Then no collection runs and no squiggles are shown
    And the operator sees only "expiry-radar: collecting…" and no report appears

  @id:editor-integrations.editor-settings.005 @rule:editor.settings.commands-work-without-setup @inferred @source:extensions/nvim/plugin/expiry-radar.lua#ready
  Scenario: Commands work with the defaults before any setup
    Given the operator never called the plugin setup
    When the operator runs any expiry-radar command
    Then the plugin uses its default settings rather than failing
