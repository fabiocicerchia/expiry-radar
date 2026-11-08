@domain:reports-and-probes @area:html-report @from:src/commands
Feature: Viewing the HTML report
  Shows operators the CLI's report in an editor tab that matches the inventory panel next to it.

  Background:
    Given a workspace folder is open

  @id:vscode.reports-and-probes.html-report.001 @rule:reports.html.re-rendered-when-stale @inferred @source:extensions/vscode/src/commands.ts#Commands @questionable
  Scenario Outline: The report is re-rendered only when a newer scan exists
    Given the report tab was rendered <when_rendered>
    When the operator shows the report
    Then the report is <outcome>

    Examples:
      | when_rendered               | outcome                                                                          |
      | before the most recent scan | rendered again with a "expiry-radar: rendering the report" progress notification |
      | after the most recent scan  | shown again without contacting any source                                        |
      | never                       | rendered for the first time                                                      |
