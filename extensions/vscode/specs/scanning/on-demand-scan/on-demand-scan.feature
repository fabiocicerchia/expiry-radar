@domain:scanning @area:on-demand-scan @from:src/commands
Feature: Scanning the estate on demand
  Lets an operator refresh the inventory when they choose, and tells them plainly when there is nothing to scan.

  @id:vscode.scanning.on-demand-scan.001 @rule:scanning.on-demand.requires-open-folder @inferred @source:extensions/vscode/src/commands.ts#Commands
  Scenario: A scan with no folder open is refused with a warning
    Given no workspace folder is open in the editor
    When the operator starts a scan
    Then the warning "expiry-radar: open a folder first." is shown
    And no collection runs

  @id:vscode.scanning.on-demand-scan.002 @rule:scanning.on-demand.no-sources-offers-setup @inferred @source:extensions/vscode/src/commands.ts#Commands
  Scenario: A scan with nothing configured offers to open the config or the settings
    Given the workspace folder has no expiry-radar config file
    And no endpoints or domains are set in the extension settings
    When the operator starts a scan
    Then the warning "expiry-radar: no sources configured — every source is opt-in, so there is nothing to collect." is shown
    And it offers "Open config file" and "Open settings"
    And choosing "Open settings" opens the settings filtered to expiryRadar
    And no collection runs

  @id:vscode.scanning.on-demand-scan.003 @rule:scanning.on-demand.cancellable-progress @inferred @source:extensions/vscode/src/commands.ts#Commands
  Scenario: An on-demand scan shows a progress notification the operator can cancel
    Given the workspace folder "payments" has a config file
    When the operator starts a scan
    Then a notification "expiry-radar: collecting payments" is shown while it runs
    And the notification has a Cancel action
    And cancelling it stops this scan
