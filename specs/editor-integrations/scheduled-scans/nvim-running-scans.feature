@domain:editor-integrations @area:scheduled-scans @from:extensions/nvim
Feature: Running collections from Neovim
  Lets an operator refresh, probe and export the inventory from the editor without hammering hosts and registries or losing the last good result.

  Background:
    Given Given the operator uses the expiry-radar plugin in Neovim

  @id:editor-integrations.scheduled-scans.001 @rule:editor.scans.automatic-triggers @inferred @source:extensions/nvim/lua/expiry-radar/init.lua#attach_autocmds
  Scenario Outline: Collections start on their own only on the configured triggers
    Given Given the collection trigger is <trigger>
    And And the project has a config file expiry-radar.json
    When When <event>
    Then Then <outcome>

    Examples:
      | trigger                     | event                                | outcome                                               |
      | on_config_save_and_interval | the operator saves expiry-radar.json | one collection starts after a 1.5 second quiet period |
      | on_config_save_and_interval | the operator saves README.md         | no collection starts                                  |
      | interval                    | the operator saves expiry-radar.json | no collection starts                                  |
      | interval                    | 60 minutes pass                      | a collection starts                                   |
      | on_config_save              | 60 minutes pass                      | no collection starts                                  |
      | on_config_save              | the operator opens the project       | a collection starts 5 seconds later                   |
      | manual                      | the operator opens the project       | no collection starts                                  |

  @id:editor-integrations.scheduled-scans.002 @rule:editor.scans.no-sources-quiet-unless-asked @inferred @source:extensions/nvim/lua/expiry-radar/run.lua#M.collect
  Scenario Outline: A project with nothing configured is not collected, and only a manual refresh says why
    Given Given the project has no expiry-radar.json and no endpoints or domains in the plugin setup
    When When a <run> collection is due
    Then Then expiry-radar is not run
    And And the operator sees <message>

    Examples:
      | run       | message                                                                                                      |
      | automatic | nothing                                                                                                      |
      | manual    | "expiry-radar: no sources configured — every source is opt-in. :ExpiryRadarConfig creates one." as a warning |

  @id:editor-integrations.scheduled-scans.003 @rule:editor.scans.failed-run-keeps-last-inventory @verified @source:extensions/nvim/lua/expiry-radar/run.lua#report_from @test:extensions/nvim/tests/init_spec.lua#exit 2 is a failure, and leaves the last good inventory alone @test:extensions/nvim/tests/init_spec.lua#a report that cannot be decoded is a failure, not an empty inventory
  Scenario Outline: A run that produces no readable report is an error and keeps the last good inventory
    Given Given an earlier collection returned shop.example.com
    And And expiry-radar now <failure>
    When When the operator refreshes the inventory
    Then Then the operator sees an error containing "<message>"
    And And the inventory still shows the earlier collection

    Examples:
      | failure                                                       | message                                                           |
      | exits with status 2 and "expiry-radar: no sources configured" | expiry-radar failed (exit 2): expiry-radar: no sources configured |
      | exits 0 with output that is not JSON                          | expiry-radar wrote a report that could not be read                |

  @id:editor-integrations.scheduled-scans.004 @rule:editor.scans.one-collection-at-a-time @inferred @source:extensions/nvim/lua/expiry-radar/run.lua#M.collect @questionable
  Scenario Outline: Only one collection runs at a time, and a manual refresh replaces a running one
    Given Given a collection is already running
    When When a <run> collection is requested
    Then Then <outcome>

    Examples:
      | run       | outcome                                                   |
      | automatic | the running collection continues and no second one starts |
      | manual    | the running collection is stopped and a new one starts    |

  @id:editor-integrations.scheduled-scans.005 @rule:editor.scans.missing-binary-names-the-fix @inferred @source:extensions/nvim/lua/expiry-radar/core.lua#M.resolve_cmd
  Scenario: A refresh without the expiry-radar binary says how to install it
    Given Given no expiry-radar binary is in the project's bin directory, on PATH, or in the Go bin directories
    And And no command is configured
    When When the operator refreshes the inventory
    Then Then the operator sees the error "the expiry-radar binary was not found — install it with `go install github.com/fabiocicerchia/expiry-radar/cmd/expiry-radar@latest`, or set cmd"

  @id:editor-integrations.scheduled-scans.006 @rule:editor.scans.probe-ignores-config @verified @source:extensions/nvim/lua/expiry-radar/run.lua#M.probe @test:extensions/nvim/tests/core_spec.lua#drops the config, the options and the extra args for a one-off probe @test:extensions/nvim/tests/core_spec.lua#says "no config" out loud for a probe, rather than omitting the flag
  Scenario: Probing one host checks only its certificate and registration
    Given Given the project's config declares many hosts and cloud sources
    When When the operator probes admin.example.com:8443
    Then Then only the TLS certificate of admin.example.com:8443 and the registration of admin.example.com are checked
    And And none of the config file's sources, filters or extra arguments are used
    And And the result is shown in a window titled admin.example.com:8443

  @id:editor-integrations.scheduled-scans.007 @rule:editor.scans.export-writes-cli-output @verified @source:extensions/nvim/lua/expiry-radar/run.lua#M.export @test:extensions/nvim/tests/init_spec.lua#writes exactly the bytes the CLI produced, and none of its own
  Scenario: Exporting writes exactly the report the CLI rendered
    Given Given the project is configured with sources
    When When the operator exports the inventory as ical without naming a file
    Then Then a file expiry-radar-<today>.ics is written in the project root with exactly the bytes the CLI produced
    And And the operator is told "exported to" followed by that path

  @id:editor-integrations.scheduled-scans.008 @rule:editor.scans.export-refuses-unknown-format @verified @source:extensions/nvim/lua/expiry-radar/run.lua#M.export @test:extensions/nvim/tests/init_spec.lua#refuses a format the CLI does not render, without running anything
  Scenario: An unknown export format is refused without running anything
    Given Given the project is configured with sources
    When When the operator exports the inventory as pdf
    Then Then the operator sees the error "unknown format \"pdf\" (want one of: html, ical, json, prometheus)"
    And And no file is written
