@domain:inventory-panel @area:attention-indicators @from:src/inventoryview
Feature: Signalling what needs action today
  Tells an operator at a glance how many items are already expired or about to expire, and whether a scan is running.

  Background:
    Given the operator has the expiry-radar inventory panel

  @id:vscode.inventory-panel.attention-indicators.001 @rule:inventory.attention.badge-counts-expired-and-urgent @inferred @source:extensions/vscode/src/inventoryView.ts#InventoryView @questionable
  Scenario Outline: The panel badge counts items that are expired or expire within 14 days
    Given the last scan found <expired> expired items, <urgent> items expiring within 14 days and 5 items further out
    And no filter is applied
    When the operator looks at the inventory panel
    Then the panel badge shows <badge>

    Examples:
      | expired | urgent | badge                                       |
      | 1       | 2      | 3 with tooltip "3 expired or expiring soon" |
      | 0       | 0      | no badge                                    |

  @id:vscode.inventory-panel.attention-indicators.002 @rule:inventory.attention.collecting-indicator @inferred @source:extensions/vscode/src/inventoryView.ts#InventoryView
  Scenario: The panel header says which folder is being scanned while a scan runs
    Given a scan of the workspace folder "payments" is in progress
    When the operator looks at the inventory panel
    Then the panel header reads "collecting payments…"
    And after the scan the header shows the item count and the time it was collected
