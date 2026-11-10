@domain:config-insights @area:declaration-lookup @from:src/collector
Feature: Pointing recorded items at their config line
  Operators editing the config see squiggles and removal actions only for items they wrote down, never for items discovered elsewhere.

  @id:vscode.config-insights.declaration-lookup.001 @rule:config-insights.declaration.only-recorded-items-placed @inferred @source:extensions/vscode/src/collector.ts#withOrigins
  Scenario Outline: Only items recorded in the config file are tied to a config line
    Given the config file declares "api.example.com"
    And a scan finds an item named "api.example.com" from source <source>
    When the scan finishes
    Then the item <is> tied to the line in the config file that declares it

    Examples:
      | source       | is     |
      | tls:endpoint | is     |
      | domain:rdap  | is     |
      | domain:whois | is     |
      | manual       | is     |
      | k8s:secret   | is not |
