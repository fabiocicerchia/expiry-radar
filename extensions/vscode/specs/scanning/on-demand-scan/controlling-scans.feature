@domain:scanning @area:on-demand-scan @from:src/scheduler
Feature: Controlling scans in progress
  Lets the user who asks for a scan get it right away, stop it, and keep scanning after a failure.

  Background:
    Given a workspace folder with sources configured in its expiry-radar config

  @id:vscode.scanning.on-demand-scan.010 @rule:scanning.on-demand-scan.failure-does-not-block-later-scans @verified @source:extensions/vscode/src/scheduler.ts#Scheduler @test:extensions/vscode/src/test/scheduler.test.ts#a failed run does not poison the queue
  Scenario: A failed scan does not stop later scans from running
    Given the previous scan failed
    When the user starts a scan
    Then the new scan runs
