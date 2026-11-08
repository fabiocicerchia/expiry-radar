@domain:tracking-items @area:remove-item @from:src/commands
Feature: Stopping tracking of a recorded item
  Lets an operator drop an item from the config file in the panel without editing JSON, and never deletes the wrong entry or touches the estate.

  Background:
    Given the inventory panel shows the results of the last scan

  @id:vscode.tracking-items.remove-item.001 @rule:tracking.remove.only-recorded-items @inferred @source:extensions/vscode/src/commands.ts#Commands
  Scenario: Only items recorded in the config file can be removed
    Given the item "api-tls" was discovered by a source rather than recorded in the config file
    When the operator asks to stop tracking "api-tls"
    Then no confirmation is shown
    And the config file is left unchanged

  @id:vscode.tracking-items.remove-item.002 @rule:tracking.remove.requires-confirmation @inferred @source:extensions/vscode/src/commands.ts#Commands
  Scenario: Removing an item needs confirmation that says the estate is not touched
    Given "shop.example.com:443" is recorded in expiry-radar.json
    When the operator asks to stop tracking "shop.example.com:443"
    Then a modal asks "Stop tracking shop.example.com:443?"
    And its detail says "Removes its entry from expiry-radar.json. Nothing in your estate is touched."
    And dismissing it leaves the config file unchanged

  @id:vscode.tracking-items.remove-item.003 @rule:tracking.remove.refuses-when-config-changed @inferred @source:extensions/vscode/src/commands.ts#Commands @questionable
  Scenario: Removal is refused when the config line no longer names the item
    Given "shop.example.com:443" was recorded on line 4 of expiry-radar.json at the last scan
    And line 4 has since been edited to name a different host
    When the operator confirms stopping tracking of "shop.example.com:443"
    Then the warning "expiry-radar: expiry-radar.json has changed since the last collection — refresh and try again." is shown
    And the config file is left unchanged

  @id:vscode.tracking-items.remove-item.004 @rule:tracking.remove.removes-entry-and-rescans @inferred @source:extensions/vscode/src/commands.ts#Commands
  Scenario: A confirmed removal deletes the entry and refreshes the inventory
    Given "example.com" is recorded in the domains list of expiry-radar.json
    And the config file has not changed since the last scan
    When the operator confirms stopping tracking of "example.com"
    Then the "example.com" entry is removed from the domains list
    And the other entries are left as they were
    And a new scan starts, so the item leaves the panel
