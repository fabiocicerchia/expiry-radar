@domain:scanning @area:automatic-refresh @from:src/extension
Feature: Keeping the inventory fresh automatically
  Serves the engineer working in the repository: the inventory refreshes on its own when the window opens, on a timer and when the config file is saved, without nagging in folders that do not use expiry-radar.

  Background:
    Given a workspace folder is open in the editor

  @id:vscode.scanning.automatic-refresh.001 @rule:scanning.automatic.config-save-triggers-scan @inferred @source:extensions/vscode/src/extension.ts#onSave
  Scenario Outline: Saving the config file starts a scan, saving anything else does not
    Given the scan trigger setting is <trigger>
    And the folder's config file is expiry-radar.json
    When the user saves <saved file>
    Then <outcome>

    Examples:
      | trigger                 | saved file        | outcome                                      |
      | onConfigSave            | expiry-radar.json | a scan starts with the reason "config saved" |
      | onConfigSaveAndInterval | expiry-radar.json | a scan starts with the reason "config saved" |
      | onConfigSaveAndInterval | README.md         | no scan starts                               |
      | interval                | expiry-radar.json | no scan starts                               |
      | manual                  | expiry-radar.json | no scan starts                               |

  @id:vscode.scanning.automatic-refresh.002 @rule:scanning.automatic.startup-scan @inferred @source:extensions/vscode/src/extension.ts#scheduleStartupCollection
  Scenario Outline: A scan runs shortly after the window opens unless startup scanning is off
    Given the scan on startup setting is <on startup>
    And the scan trigger setting is <trigger>
    And the folder has a config file
    When the editor opens the workspace
    Then <outcome>

    Examples:
      | on startup | trigger                 | outcome                                                          |
      | true       | onConfigSaveAndInterval | a few seconds later a scan starts with the reason "startup"      |
      | true       | interval                | a few seconds later a scan starts with the reason "startup"      |
      | false      | onConfigSaveAndInterval | no scan starts                                                   |
      | true       | manual                  | no scan starts                                                   |

  @id:vscode.scanning.automatic-refresh.003 @rule:scanning.automatic.quiet-without-sources @inferred @source:extensions/vscode/src/extension.ts#scheduleAutomatic
  Scenario Outline: Automatic scans stay silent in a folder with nothing configured
    Given the folder has no expiry-radar config file
    And no endpoints or domains are set in the extension settings
    When an automatic scan comes due at <moment>
    Then no scan runs
    And no message is shown to the user

    Examples:
      | moment           |
      | startup          |
      | periodic refresh |

  @id:vscode.scanning.automatic-refresh.004 @rule:scanning.automatic.periodic-refresh @inferred @source:extensions/vscode/src/extension.ts#setSweep @questionable
  Scenario Outline: Periodic refresh runs only when the trigger includes an interval, for the folder in focus
    Given the scan trigger setting is <trigger>
    And the folder of the active editor has a config file
    When the refresh interval elapses while the window is focused and no scan finished recently
    Then <outcome>

    Examples:
      | trigger                 | outcome                                                                        |
      | interval                | a scan of the active editor's folder starts with the reason "periodic refresh" |
      | onConfigSaveAndInterval | a scan of the active editor's folder starts with the reason "periodic refresh" |
      | onConfigSave            | no scan starts                                                                 |
      | manual                  | no scan starts                                                                 |

  @id:vscode.scanning.automatic-refresh.005 @rule:scanning.automatic.settings-apply-live @inferred @source:extensions/vscode/src/extension.ts#activate
  Scenario: Changing the extension settings takes effect without reloading the window
    Given the extension is running
    When the user changes an expiry-radar setting such as the trigger, refresh interval or binary path
    Then the periodic refresh is re-armed using the new trigger and interval
    And the next scan uses the newly configured binary
    And the config file squiggles and status bar are redrawn under the new settings
    And a missing-binary warning can be shown again
