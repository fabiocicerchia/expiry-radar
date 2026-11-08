@domain:tracking-items @area:config-writing @from:src/commands
Feature: Creating a starter config file
  Gives an operator a working config to edit when they open one that does not exist yet.

  Background:
    Given a workspace folder is open with no expiry-radar config file

  @id:vscode.tracking-items.config-writing.001 @rule:config.starter.seeded-from-example @inferred @source:extensions/vscode/src/commands.ts#Commands @questionable
  Scenario Outline: A new config is seeded from the repository example, or from a minimal default
    Given the folder <example_state>
    When the operator opens the config and chooses "Create it"
    Then expiry-radar.json is created with <contents>
    And it opens in the editor
    And an existing file at that path is never overwritten

    Examples:
      | example_state                 | contents                                                |
      | has expiry-radar.example.json | the example file's contents                             |
      | has no example file           | an endpoint shop.example.com and the domain example.com |
