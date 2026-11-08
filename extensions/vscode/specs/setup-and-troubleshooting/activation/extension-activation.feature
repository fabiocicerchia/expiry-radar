@domain:setup-and-troubleshooting @area:activation @from:src/extension
Feature: Extension activation
  Serves every user of the VS Code extension: the commands it advertises are actually available once the extension loads.

  @id:vscode.setup-and-troubleshooting.activation.001 @rule:activation.commands.all-advertised-available @verified @source:extensions/vscode/src/extension.ts#activate @test:extensions/vscode/src/test/activate.test.ts#activate registers exactly the commands the manifest contributes
  Scenario: Every command the extension advertises can be run
    Given the extension manifest lists the expiry-radar commands
    When the editor activates the extension
    Then every listed expiry-radar command can be run from the command palette
    And no command is available that the manifest does not list
