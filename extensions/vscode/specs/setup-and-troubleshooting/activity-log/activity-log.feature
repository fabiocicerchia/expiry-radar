@domain:setup-and-troubleshooting @area:activity-log @from:src/log
Feature: Reading the extension's activity log
  Lets an operator see what the extension ran and what failed, in one named log, without losing their place in the editor.

  @id:vscode.setup-and-troubleshooting.activity-log.001 @rule:vscode.activity-log.single-named-channel @inferred @source:extensions/vscode/src/log.ts#log
  Scenario: Show Log opens the one expiry-radar log without taking focus from the editor
    Given the extension has been active in the workspace
    When the operator runs the Show Log command
    Then the output panel shows the log named "expiry-radar"
    And the editor the operator was working in keeps keyboard focus
    And running Show Log again shows the same log, not a second "expiry-radar" entry
