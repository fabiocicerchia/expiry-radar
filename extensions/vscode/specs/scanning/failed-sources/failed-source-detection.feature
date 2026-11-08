@domain:scanning @area:failed-sources @from:src/parse
Feature: Detecting failed sources
  Lets operators see which sources failed during a run, so an inventory missing a whole cloud account never looks clean.

  @id:vscode.scanning.failed-sources.001 @rule:scanning.failed-sources.warning-lines-reported @verified @source:extensions/vscode/src/parse.ts#parseWarnings @test:extensions/vscode/src/test/parse.test.ts#parseWarnings picks the failed sources out of stderr @test:extensions/vscode/src/test/parse.test.ts#parseWarnings finds nothing in a clean run
  Scenario: Each failed source the CLI warns about is reported, and other output is ignored
    Given the CLI run printed "expiry-radar: warning: aws: AccessDenied: not authorized to perform acm:ListCertificates"
    And it printed "  expiry-radar: warning: vault: 403 permission denied  "
    And it also printed "some unrelated chatter" and "expiry-radar: something that is not a warning"
    When the run finishes
    Then the failed sources reported are "aws: AccessDenied: not authorized to perform acm:ListCertificates" and "vault: 403 permission denied"
    And nothing else is reported as a failure
