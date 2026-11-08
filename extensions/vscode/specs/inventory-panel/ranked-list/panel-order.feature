@domain:inventory-panel @area:ranked-list @from:src/parse
Feature: Ordering the inventory panel
  Gives operators one list that puts what needs action first.

  @id:vscode.inventory-panel.ranked-list.007 @rule:inventory-panel.order.deadline-then-priority @verified @source:extensions/vscode/src/parse.ts#compareItems @test:extensions/vscode/src/test/parse.test.ts#items sort worst deadline first, then by priority @questionable
  Scenario: The panel lists the worst deadline first, then the highest priority
    Given "calm" has 80 days left and priority 0.9
    And "broken" has expired and has priority 0.1
    And "urgent-low" has 3 days left and priority 0.2
    And "urgent-high" has 9 days left and priority 0.8
    When the operator opens the inventory panel
    Then the entries are listed as "broken", "urgent-high", "urgent-low", "calm"
