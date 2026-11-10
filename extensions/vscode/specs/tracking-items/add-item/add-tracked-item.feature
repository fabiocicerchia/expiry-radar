@domain:tracking-items @area:add-item @from:src/commands
Feature: Recording a new item from the editor
  Lets an operator start tracking a host, domain or manual item without editing JSON, and see the result right away.

  Background:
    Given a workspace folder is open

  @id:vscode.tracking-items.add-item.001 @rule:tracking.add.writes-shows-and-scans @inferred @source:extensions/vscode/src/commands.ts#Commands
  Scenario: An added item is written to the config, shown at its line and scanned at once
    Given expiry-radar.json already lists one domain
    When the operator adds the domain "example.org"
    Then "example.org" is added to the domains list of expiry-radar.json
    And the config file opens with the cursor on the new entry's line
    And a scan starts right away

  @id:vscode.tracking-items.add-item.002 @rule:tracking.add.creates-config-when-missing @inferred @source:extensions/vscode/src/commands.ts#Commands @questionable
  Scenario: Adding an item when no config exists creates one around it
    Given the folder has no expiry-radar config file
    When the operator adds the endpoint "shop.example.com"
    Then a config file is created at the configured config path, or expiry-radar.json in the folder
    And it contains only the new endpoint
