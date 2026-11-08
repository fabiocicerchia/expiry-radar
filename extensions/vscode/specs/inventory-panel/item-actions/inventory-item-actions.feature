@domain:inventory-panel @area:item-actions @from:src/inventoryview
Feature: Acting on an inventory item
  Lets an operator jump to where a recorded item is declared and remove it, while keeping discovered items read-only.

  Background:
    Given the operator has the expiry-radar inventory panel open

  @id:vscode.inventory-panel.item-actions.002 @rule:inventory.actions.recorded-item-opens-declaration @inferred @source:extensions/vscode/src/inventoryView.ts#InventoryView
  Scenario: Opening a recorded item jumps to the line that declared it
    Given the domain "example.com" is recorded on line 12, column 7 of the config file
    When the operator opens the item in the inventory panel
    Then the config file opens with the cursor at line 12, column 7

  @id:vscode.inventory-panel.item-actions.003 @rule:inventory.actions.only-recorded-items-removable @inferred @source:extensions/vscode/src/inventoryView.ts#InventoryView
  Scenario Outline: Only items recorded in the config file can be removed from the panel
    Given the item "<item>" was <origin>
    When the operator opens the item's context menu
    Then Remove Item is <availability>

    Examples:
      | item                | origin                       | availability |
      | example.com         | recorded in the config file  | offered      |
      | default/ingress-tls | discovered by the k8s source | not offered  |
