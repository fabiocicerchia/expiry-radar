@domain:inventory-panel @area:ranked-list @from:src/inventoryview
Feature: Seeing what expires in priority order
  Gives an operator one list of everything that expires, worst deadline and biggest blast radius first, without hiding that the inventory may be incomplete.

  Background:
    Given the operator has the expiry-radar inventory panel open

  @id:vscode.inventory-panel.ranked-list.001 @rule:inventory.panel.ranked-by-deadline-then-priority @inferred @source:extensions/vscode/src/inventoryView.ts#InventoryView
  Scenario: Items are listed by deadline band, then by priority
    Given the last scan found an expired secret "db-password" with priority 0.40
    And it found a TLS certificate "pay.example.com" expiring in 5 days with priority 0.90
    And it found a TLS certificate "staging.example.com" expiring in 5 days with priority 0.20
    When the operator looks at the inventory panel in its default grouping
    Then the list reads "db-password", "pay.example.com", "staging.example.com" in that order
    And each row shows the days left, the kind, the source and the priority, e.g. "5d · TLS certificate · k8s · p 0.90"

  @id:vscode.inventory-panel.ranked-list.002 @rule:inventory.panel.severity-icon @inferred @source:extensions/vscode/src/inventoryView.ts#ICONS
  Scenario Outline: Each item's icon shows how close its deadline is
    Given the last scan found an item whose deadline band is <band>
    When the operator looks at the inventory panel
    Then the item is shown with the <icon> icon in <colour>

    Examples:
      | band           | icon    | colour                      |
      | expired        | error   | red                         |
      | within 14 days | warning | the Problems warning colour |
      | within 30 days | clock   | yellow                      |
      | further out    | pass    | green                       |

  @id:vscode.inventory-panel.ranked-list.003 @rule:inventory.panel.item-tooltip-explains-rank @inferred @source:extensions/vscode/src/inventoryView.ts#InventoryView
  Scenario: An item's tooltip explains why it ranks where it does
    Given the last scan found the TLS certificate "pay.example.com" expiring on 2026-11-20 with blast radius 0.85, discovered by the "k8s" source, labelled env=prod
    When the operator hovers over the item
    Then the tooltip shows the name, the days left and the reason it was ranked
    And it shows the kind, source, expiry date 2026-11-20, priority and blast radius 0.85
    And it shows the label "env=prod"
    And it says the item was discovered by a source, not recorded in the config file

  @id:vscode.inventory-panel.ranked-list.004 @rule:inventory.panel.failed-sources-notice @inferred @source:extensions/vscode/src/inventoryView.ts#InventoryView
  Scenario: Failed sources are announced at the top of the panel
    Given the last scan could not read the "aws" and "vault" sources but read the others
    When the operator looks at the inventory panel
    Then the top of the panel says "⚠ 2 source(s) failed — this inventory is incomplete:"
    And each failed source's warning is listed beneath it
    And the items the other sources found are still listed

  @id:vscode.inventory-panel.ranked-list.005 @rule:inventory.panel.empty-state-guidance @inferred @source:extensions/vscode/src/inventoryView.ts#InventoryView
  Scenario Outline: An empty panel says why it is empty
    Given <state>
    When the operator looks at the inventory panel
    Then the panel says "<message>"

    Examples:
      | state                                                        | message                                                                |
      | no scan has run for the workspace folder yet                 | Nothing collected yet — run "expiry-radar: Refresh Inventory".         |
      | the last scan found nothing                                  | Nothing expiring — or no sources were enabled.                         |
      | the last scan found 3 items and the filter hides all of them | 3 item(s) hidden by the current filter — "expiry-radar: Filter Items". |

  @id:vscode.inventory-panel.ranked-list.006 @rule:inventory.panel.follows-active-folder @inferred @source:extensions/vscode/src/inventoryView.ts#InventoryView @questionable
  Scenario: The panel shows the inventory of the workspace folder being edited
    Given a workspace with folders "payments" and "web", each with its own scan results
    And the operator is editing a file in "web"
    When the operator looks at the inventory panel
    Then the panel lists the items found for "web"
    And the panel header shows the number of items found for "web" and when they were collected
