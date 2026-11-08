@domain:setup-and-troubleshooting @area:settings @from:src/config
Feature: Keeping extension settings within safe limits
  Operators who mistype or push a setting out of range still get a working extension, because the value is raised or lowered to the nearest safe limit instead of breaking scans or alerts.

  Background:
    Given the expiry-radar extension is installed in a workspace

  @id:vscode.setup-and-troubleshooting.settings.001 @rule:settings.limits.out-of-range-values-clamped @inferred @source:extensions/vscode/src/config.ts#readSettings
  Scenario Outline: An out-of-range setting is raised or lowered to its nearest allowed value
    Given the operator has set "<setting>" to <configured>
    When the extension reads its settings for the workspace folder
    Then the extension behaves as if "<setting>" were <effective>

    Examples:
      | setting                                | configured | effective |
      | expiryRadar.scan.intervalMinutes       | 1          | 5         |
      | expiryRadar.scan.debounceMs            | 100        | 250       |
      | expiryRadar.scan.timeoutSeconds        | 3          | 10        |
      | expiryRadar.diagnostics.warnWithinDays | -3         | 0         |
      | expiryRadar.view.withinDays            | -5         | 0         |
      | expiryRadar.view.minPriority           | 1.5        | 1         |
      | expiryRadar.view.minPriority           | -0.2       | 0         |
      | expiryRadar.status.warnWithinDays      | -1         | 0         |

  @id:vscode.setup-and-troubleshooting.settings.002 @rule:settings.limits.info-window-not-shorter-than-warning-window @inferred @source:extensions/vscode/src/config.ts#readSettings @questionable
  Scenario Outline: An information window shorter than the warning window never hides warnings
    Given the operator has set "expiryRadar.diagnostics.warnWithinDays" to 21
    And the operator has set "expiryRadar.diagnostics.infoWithinDays" to 7
    And the config file declares example.com, which expires in <days_left> days
    When a scan completes
    Then the declaration of example.com shows <squiggle>

    Examples:
      | days_left | squiggle    |
      | 15        | a warning   |
      | 25        | no squiggle |
