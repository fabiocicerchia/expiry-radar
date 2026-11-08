@domain:inventory-panel @area:grouping @from:src/inventoryview
Feature: Grouping the inventory by kind
  Lets an operator answer "what certificates do we have" by grouping items by kind, while keeping the most urgent kind on top.

  Background:
    Given the operator has the expiry-radar inventory panel open

  @id:vscode.inventory-panel.grouping.001 @rule:inventory.grouping.kind-groups-by-worst-deadline @inferred @source:extensions/vscode/src/inventoryView.ts#InventoryView
  Scenario: Kind groups are ordered by their worst deadline
    Given the last scan found an expired secret, and TLS certificates expiring in 10 and 40 days
    And it found no other items
    When the operator groups the inventory by kind
    Then the "Secret" group comes before the "TLS certificate" group
    And the "TLS certificate" group reads "2 · soonest 10d"
    And inside each group items keep their ranked order and rows no longer repeat the kind

  @id:vscode.inventory-panel.grouping.002 @rule:inventory.grouping.expand-all-reopens-groups @inferred @source:extensions/vscode/src/inventoryView.ts#InventoryView
  Scenario: Expand All reopens every group, and hand-collapsed groups otherwise stay collapsed
    Given the inventory is grouped by kind
    And the operator collapsed the "Secret" group by hand
    When the operator chooses Expand All
    Then every group, including "Secret", is expanded
