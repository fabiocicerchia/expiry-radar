@domain:scanning @area:on-demand-scan @from:src/runner
Feature: Running a collection
  Makes sure the operator gets an inventory whenever the CLI produced one, sees the CLI's own reason when it did not, and can stop a scan that is taking too long.

  @id:vscode.scanning.on-demand-scan.004 @rule:scanning.run.report-kept-on-threshold-or-partial-exit @verified @source:extensions/vscode/src/runner.ts#runRadar @test:extensions/vscode/src/test/process.test.ts#exit 1 with a report on stdout is a report, and its warnings are kept @test:extensions/vscode/src/test/contract.test.ts#a report that lost a source is still a report, and says which one
  Scenario Outline: A breached threshold or a partial run still yields the inventory
    Given the CLI writes a report and exits with <exit>
    When the operator starts a scan
    Then the inventory from that report is shown
    And every source the CLI reported as failed is kept as a failed source

    Examples:
      | exit | meaning                                                 |
      | 0    | clean run                                               |
      | 1    | a -fail-within threshold in the extra args was breached |
      | 3    | partial results, a source failed                        |

  @id:vscode.scanning.on-demand-scan.005 @rule:scanning.run.failure-shows-cli-message @verified @source:extensions/vscode/src/runner.ts#runRadar @test:extensions/vscode/src/test/process.test.ts#exit 2 is a failure that carries the CLI's own message
  Scenario Outline: A run that produced no report fails with the CLI's own message
    Given the CLI <outcome>
    When the operator starts a scan
    Then the scan fails with "expiry-radar failed (exit <exit>): <detail>"
    And no inventory is shown for that run
    And the activity log has the CLI's full error output

    Examples:
      | outcome                                                  | exit | detail                    |
      | exits 2 and prints "expiry-radar: bad -within" on stderr | 2    | expiry-radar: bad -within |
      | exits 0 and prints nothing                               | 0    | no output                 |

  @id:vscode.scanning.on-demand-scan.006 @rule:scanning.run.no-sources-is-an-error-not-empty @verified @source:extensions/vscode/src/runner.ts#runRadar @test:extensions/vscode/src/test/contract.test.ts#a run with no sources at all is an error, not an empty inventory
  Scenario: A run with nothing to collect is an error, never an empty inventory
    Given the run may not read the config file and names no hosts and no domains to check
    When the CLI is run
    Then the run fails with a message containing "exit 2"
    And the panel does not show an empty inventory

  @id:vscode.scanning.on-demand-scan.007 @rule:scanning.run.timeout-past-cli-budget @inferred @source:extensions/vscode/src/runner.ts#runRadar
  Scenario: A scan that runs past the collection timeout is stopped
    Given the collection timeout setting is 120 seconds
    And the CLI is still running 130 seconds after it started
    When the operator starts a scan
    Then the CLI and everything it started are stopped
    And the scan fails with "expiry-radar timed out after 130s"

  @id:vscode.scanning.on-demand-scan.008 @rule:scanning.run.cancel-stops-everything-started @verified @source:extensions/vscode/src/runner.ts#runRadar @test:extensions/vscode/src/test/process.test.ts#cancelling a collection kills what the CLI started, not just the CLI
  Scenario: Cancelling a scan stops the CLI and everything it started
    Given a scan is in progress with connections still open
    When the operator cancels the scan
    Then the scan ends as cancelled, with no error and no new inventory
    And no process the scan started is still running

  @id:vscode.scanning.on-demand-scan.009 @rule:scanning.sources.nothing-configured-means-nothing-collected @verified @source:extensions/vscode/src/runner.ts#hasSources @test:extensions/vscode/src/test/runner.test.ts#a folder with no config and no settings has nothing to collect
  Scenario Outline: A folder with nothing configured is not scanned
    Given the workspace folder has <config>
    And the settings list <hosts> as hosts and <domains> as domains
    When a scan is due for that folder
    Then a collection <runs>

    Examples:
      | config               | hosts         | domains     | runs         |
      | no config file       | none          | none        | does not run |
      | no config file       | a.example.com | none        | runs         |
      | no config file       | none          | example.com | runs         |
      | an expiry-radar.json | none          | none        | runs         |
