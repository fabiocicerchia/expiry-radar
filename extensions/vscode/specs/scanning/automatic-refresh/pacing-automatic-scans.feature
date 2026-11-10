@domain:scanning @area:automatic-refresh @from:src/scheduler
Feature: Pacing automatic scans
  Keeps automatic scans from hammering rate-limited registries and cloud APIs, so an editor left open all day produces a handful of collections, not hundreds.

  Background:
    Given a workspace folder with sources configured in its expiry-radar config

  @id:vscode.scanning.automatic-refresh.006 @rule:scanning.automatic-refresh.saves-debounced-into-one-run @verified @source:extensions/vscode/src/scheduler.ts#Scheduler @test:extensions/vscode/src/test/scheduler.test.ts#a burst of requests collapses into one run, and the newest wins
  Scenario: A burst of automatic scan requests collapses into one scan
    Given several automatic scan requests arrive within the debounce delay, the last one being a config save
    When the debounce delay passes with no further request
    Then exactly one scan runs
    And the scan runs for the last request received, and the log shows that request's reason

  @id:vscode.scanning.automatic-refresh.007 @rule:scanning.automatic-refresh.one-collection-at-a-time @verified @source:extensions/vscode/src/scheduler.ts#Scheduler @test:extensions/vscode/src/test/scheduler.test.ts#only one collection runs at a time
  Scenario: Only one collection runs at a time
    Given a scan is already collecting
    When another scan is requested
    Then the second scan does not start until the first has finished
    And no more than one expiry-radar collection is ever running at once

  @id:vscode.scanning.automatic-refresh.008 @rule:scanning.automatic-refresh.periodic-skips-unfocused-or-fresh @inferred @source:extensions/vscode/src/scheduler.ts#Scheduler
  Scenario Outline: The periodic refresh skips when the window is in the background or the inventory is still fresh
    Given the periodic refresh is enabled with an interval of 60 minutes
    And the editor window is <focus>
    And the last scan finished <last_scan> ago
    When the refresh interval elapses
    Then a periodic refresh scan is <outcome>

    Examples:
      | focus       | last_scan  | outcome   |
      | focused     | 60 minutes | scheduled |
      | not focused | 60 minutes | skipped   |
      | focused     | 50 minutes | skipped   |

  @id:vscode.scanning.automatic-refresh.009 @rule:scanning.automatic-refresh.one-pending-request-across-folders @inferred @source:extensions/vscode/src/scheduler.ts#Scheduler @questionable
  Scenario: A newer automatic request for another folder replaces the queued one
    Given a multi-root workspace with folders "payments" and "web", both with sources configured
    And an automatic scan of "payments" is waiting out its debounce delay
    When the user saves the config file of "web"
    Then only "web" is scanned
    And the queued scan of "payments" never runs
