@domain:reports-and-probes @area:host-probe @from:src/runner
Feature: Probing a single host
  Lets an operator check one host straight away without collecting the whole configured estate or using every credential it needs.

  Background:
    Given the workspace folder has an expiry-radar.json listing configured.example.com
    And the settings list host configured.example.com and the extra argument "-fail-within 7"

  @id:vscode.reports-and-probes.host-probe.004 @rule:probe.scope.only-the-named-host @verified @source:extensions/vscode/src/runner.ts#buildArgs @test:extensions/vscode/src/test/runner.test.ts#a one-off probe drops the config, the settings and the extra args @test:extensions/vscode/src/test/runner.test.ts#a one-off probe says "no config" out loud, rather than omitting the flag @test:extensions/vscode/src/test/contract.test.ts#a probe ignores the config file sitting next to it @questionable
  Scenario: A probe checks only the host it names
    When the operator probes probe.example.com
    Then only probe.example.com is checked
    And configured.example.com is not checked, from the config file or from the settings
    And the extra arguments are not passed
    And the days-ahead and minimum-priority filters and the collection timeout still apply
