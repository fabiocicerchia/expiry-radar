@domain:inventory-panel @area:attention-indicators @from:src/status
Feature: Status bar summary of what expires next
  Gives the operator an always-on-screen clock showing the soonest deadline in the inventory, and warns them when a scan came back incomplete.

  Background:
    Given the extension is running in a workspace with an expiry-radar config
    And the status bar warning window ("expiryRadar.status.warnWithinDays") is 14 days

  @id:vscode.inventory-panel.attention-indicators.003 @rule:inventory.status-bar.shows-soonest-deadline @inferred @source:extensions/vscode/src/status.ts#StatusBar
  Scenario Outline: The status bar shows the soonest deadline, coloured by how close it is
    Given the latest inventory holds an item "api.example.com" whose soonest deadline is <days_left> days away
    And every other item expires later than "api.example.com"
    And every source was read without failing
    When the operator looks at the status bar
    Then the status bar reads "<text>"
    And the status bar uses the <icon> icon
    And the status bar background is <background>

    Examples:
      | days_left | text                        | icon    | background         |
      | -3        | expiry-radar expired 3d ago | error   | the error colour   |
      | 0         | expiry-radar today          | warning | the warning colour |
      | 9         | expiry-radar 9d             | warning | the warning colour |
      | 40        | expiry-radar 40d            | watch   | the default colour |

  @id:vscode.inventory-panel.attention-indicators.004 @rule:inventory.status-bar.tooltip-explains-next-item @inferred @source:extensions/vscode/src/status.ts#StatusBar
  Scenario: The status bar tooltip explains the next item to expire and how many are tracked
    Given the latest inventory tracks 12 items, 2 of which have already expired
    And the soonest is the TLS certificate "pay.example.com" from the "tls:endpoint" source, blast radius 0.92, "serves the payment path"
    When the operator hovers over the status bar
    Then the tooltip starts with "Next: pay.example.com" and its time left
    And the tooltip names the item's kind and its source "tls:endpoint"
    And the tooltip reads "Blast radius 0.92: serves the payment path"
    And the tooltip reads "Tracking 12 item(s), 2 already expired."
    And the tooltip ends with "Click to open the report."

  @id:vscode.inventory-panel.attention-indicators.005 @rule:inventory.status-bar.failed-sources-always-flagged @inferred @source:extensions/vscode/src/status.ts#StatusBar
  Scenario Outline: A scan that lost a source is flagged on the status bar even when nothing is urgent
    Given the latest scan could not read 1 source: "aws: AccessDenied on iam:ListAccessKeys"
    And the soonest deadline is <days_left> days away
    When the operator hovers over the status bar
    Then the tooltip reads "⚠ 1 source(s) failed — this inventory is incomplete:"
    And the tooltip lists "aws: AccessDenied on iam:ListAccessKeys"
    And the status bar background is <background>

    Examples:
      | days_left | background         |
      | 60        | the warning colour |
      | 5         | the warning colour |
      | -1        | the error colour   |

  @id:vscode.inventory-panel.attention-indicators.006 @rule:inventory.status-bar.empty-inventory @inferred @source:extensions/vscode/src/status.ts#StatusBar
  Scenario: An inventory with nothing in it shows zero and says nothing is expiring
    Given the latest scan finished and found no items
    When the operator looks at the status bar
    Then the status bar reads "expiry-radar 0" with the radio-tower icon
    And the tooltip reads "Nothing expiring in the configured sources."

  @id:vscode.inventory-panel.attention-indicators.007 @rule:inventory.status-bar.not-yet-collected @inferred @source:extensions/vscode/src/status.ts#StatusBar
  Scenario: Before the first scan the status bar invites the operator to collect
    Given no scan has completed yet for the primary workspace folder
    When the operator hovers over the status bar
    Then the status bar reads "expiry-radar" with the radio-tower icon and no count
    And the tooltip reads "Nothing collected yet — click to open the report, or run \"expiry-radar: Refresh Inventory\"."

  @id:vscode.inventory-panel.attention-indicators.008 @rule:inventory.status-bar.shows-scan-in-progress @inferred @source:extensions/vscode/src/status.ts#StatusBar
  Scenario: While a scan runs the status bar shows it is collecting, and which folder
    Given a scan of the workspace folder "payments" is in progress
    When the operator hovers over the status bar
    Then the status bar shows a spinning icon next to "expiry-radar"
    And the tooltip reads "Collecting payments…"
    And the previous deadline is not shown until the scan ends

  @id:vscode.inventory-panel.attention-indicators.009 @rule:inventory.status-bar.reflects-primary-folder-only @inferred @source:extensions/vscode/src/collector.ts#Collector @questionable
  Scenario: In a multi-root workspace the status bar summarises only the primary folder
    Given the workspace has two folders, "payments" (primary) and "staging"
    And "staging" holds a certificate that expired yesterday
    And the soonest item in "payments" expires in 30 days
    When the operator looks at the status bar
    Then the status bar reads "expiry-radar 30d" with the watch icon
    And the expired certificate in "staging" is not counted in the tooltip
