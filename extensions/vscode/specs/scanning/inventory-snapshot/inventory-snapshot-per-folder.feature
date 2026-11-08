@domain:scanning @area:inventory-snapshot @from:src/store
Feature: Keeping one current inventory per workspace folder
  Lets an operator trust that the panel, squiggles and status bar show exactly what the latest scan of a folder found, no more and no less.

  Background:
    Given a workspace folder "repo" with an expiry-radar config

  @id:vscode.scanning.inventory-snapshot.007 @rule:scanning.inventory.latest-scan-replaces-previous @verified @source:extensions/vscode/src/store.ts#ResultStore @test:extensions/vscode/src/test/store.test.ts#a newer collection replaces the previous one outright
  Scenario: An item the latest scan no longer reports disappears from the inventory
    Given the last scan of "repo" found "a.example.com" and "b.example.com"
    When the next scan of "repo" finds only "a.example.com"
    Then the inventory for "repo" lists "a.example.com"
    And "b.example.com" is no longer listed

  @id:vscode.scanning.inventory-snapshot.008 @rule:scanning.inventory.separate-per-workspace-folder @inferred @source:extensions/vscode/src/store.ts#ResultStore
  Scenario: Each workspace folder keeps its own inventory
    Given a second workspace folder "other" with an expiry-radar config
    And the last scan of "repo" found "y.example.com"
    When a scan of "other" finds "x.example.com"
    Then the inventory for "other" lists "x.example.com"
    And the inventory for "repo" still lists "y.example.com" only
