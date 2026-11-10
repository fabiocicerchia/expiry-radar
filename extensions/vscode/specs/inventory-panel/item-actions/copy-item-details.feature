@domain:inventory-panel @area:item-actions @from:src/commands
Feature: Copying an item's details
  Lets an operator paste an item's deadline and blast-radius explanation into a ticket or chat.

  Background:
    Given the inventory panel lists the item "shop.example.com:443" with 12 days left

  @id:vscode.inventory-panel.item-actions.001 @rule:panel.item.copy-details @inferred @source:extensions/vscode/src/commands.ts#Commands
  Scenario: Copying an item puts its deadline, reason and details on the clipboard
    When the operator chooses "Copy Details" on "shop.example.com:443"
    Then the clipboard's first line is "shop.example.com:443 — 12d"
    And the next line is why it is ranked where it is
    And each further line is one of the item's details
