@domain:inventory-panel @area:filtering @from:src/inventoryview
Feature: Filtering the inventory
  Lets an operator narrow the panel by how close the deadline is and by kind, without ever being able to strand themselves with an empty panel.

  Background:
    Given the operator has the expiry-radar inventory panel open

  @id:vscode.inventory-panel.filtering.001 @rule:inventory.filtering.match-both-axes @inferred @source:extensions/vscode/src/inventoryView.ts#InventoryView
  Scenario: An item is shown only if it matches a chosen deadline and a chosen kind
    Given the last scan found an expired TLS certificate, an expired secret and a TLS certificate expiring in 60 days
    When the operator filters to the "Expired" deadline and the "TLS certificate" kind
    Then only the expired TLS certificate is listed
    And the panel header says "filtered"

  @id:vscode.inventory-panel.filtering.002 @rule:inventory.filtering.choices-show-counts @inferred @source:extensions/vscode/src/inventoryView.ts#InventoryView
  Scenario: The filter offers every deadline band and only the kinds present, each with its count
    Given the last scan found 2 TLS certificates and 1 domain, none expired
    When the operator opens the filter
    Then the Deadline section lists Expired, Within 14 days, Within 30 days and Further out, with Expired showing 0
    And the Kind section lists only Domain and TLS certificate, showing 1 and 2

  @id:vscode.inventory-panel.filtering.003 @rule:inventory.filtering.empty-axis-means-all @inferred @source:extensions/vscode/src/inventoryView.ts#InventoryView
  Scenario Outline: Leaving a whole filter axis unselected shows everything on that axis
    Given the last scan found items of several kinds and deadlines
    When the operator applies the filter with <selection>
    Then <outcome>

    Examples:
      | selection                                            | outcome                                                 |
      | no deadline selected and only "Secret" kind selected | secrets of every deadline band are listed               |
      | "Expired" selected and no kind selected              | expired items of every kind are listed                  |
      | every kind selected                                  | items of kinds introduced by later scans are listed too |
