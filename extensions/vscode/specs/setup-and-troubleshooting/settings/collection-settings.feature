@domain:setup-and-troubleshooting @area:settings @from:src/runner
Feature: Shaping a collection from settings
  Lets an operator decide what a scan collects, and how much of it shows up, from extension settings, with the panel, report and export all giving the same answer.

  @id:vscode.setup-and-troubleshooting.settings.003 @rule:settings.collection.hosts-and-domains-add-to-config @verified @source:extensions/vscode/src/runner.ts#buildArgs @test:extensions/vscode/src/test/runner.test.ts#settings endpoints and domains add to the config, comma-joined @test:extensions/vscode/src/test/runner.test.ts#a resolved config file is passed as -config
  Scenario: Hosts and domains in settings are checked in addition to the config file
    Given the workspace folder has an expiry-radar.json
    And the settings list hosts a.example.com and b.example.com:8443 and domain example.com
    When the operator starts a scan
    Then the scan reads the config file
    And the scan also checks a.example.com and b.example.com:8443
    And the scan also checks the registration of example.com

  @id:vscode.setup-and-troubleshooting.settings.004 @rule:settings.collection.config-path-resolution @inferred @source:extensions/vscode/src/runner.ts#resolveConfig
  Scenario Outline: The config file setting is resolved against the workspace folder
    Given the config path setting is <setting>
    And a file exists at <file>
    When the operator starts a scan
    Then the scan reads <file>

    Examples:
      | setting                | file                       |
      | empty                  | <folder>/expiry-radar.json |
      | ops/radar.json         | <folder>/ops/radar.json    |
      | /etc/expiry-radar.json | /etc/expiry-radar.json     |
      | ~/radar.json           | <home>/radar.json          |

  @id:vscode.setup-and-troubleshooting.settings.005 @rule:settings.collection.view-filters-applied-by-cli @verified @source:extensions/vscode/src/runner.ts#buildArgs @test:extensions/vscode/src/test/runner.test.ts#the view filters are pushed down to the CLI, and only when set
  Scenario Outline: The days-ahead and minimum-priority filters cap every output alike
    Given the days-ahead filter is <within> and the minimum priority is <priority>
    When the operator starts a scan, opens the report or exports the inventory
    Then the CLI is asked for items expiring within <within_flag> and at or above priority <priority_flag>
    And the panel, the report and the export contain the same items

    Examples:
      | within | priority | within_flag | priority_flag |
      | 0      | 0        | no limit    | no limit      |
      | 45     | 0.3      | 45 days     | 0.3           |

  @id:vscode.setup-and-troubleshooting.settings.006 @rule:settings.collection.extra-args-override @verified @source:extensions/vscode/src/runner.ts#buildArgs @test:extensions/vscode/src/test/runner.test.ts#extra args go last, so a user can override what we chose
  Scenario: Extra CLI arguments override what the extension chose
    Given the collection timeout setting is 120 seconds
    And the extra arguments setting is "-timeout 5s"
    When the operator starts a scan
    Then the CLI gives up collecting after 5 seconds, not 120
