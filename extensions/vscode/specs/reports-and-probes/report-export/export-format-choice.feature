@domain:reports-and-probes @area:report-export @from:src/prompts
Feature: Choosing an export format
  Lets an operator export the inventory in the format the destination consumes: a mailable report, a calendar, CI or a metrics scrape.

  Background:
    Given a workspace folder is open

  @id:vscode.reports-and-probes.report-export.002 @rule:reports-and-probes.report-export.four-formats @inferred @source:extensions/vscode/src/prompts.ts#pickExportFormat
  Scenario Outline: The inventory can be exported as HTML, iCal, JSON or Prometheus metrics
    Given today is 2026-11-08
    When the operator exports the report as "<choice>"
    Then the picker titled "expiry-radar: export as" describes it as "<description>"
    And the save dialog suggests "expiry-radar-2026-11-08.<ext>"

    Examples:
      | choice             | description                                              | ext  |
      | HTML report        | self-contained, for mailing or publishing                | html |
      | iCal feed          | renewals as calendar events, with alarms by blast radius | ics  |
      | JSON               | the ranked inventory, for CI                             | json |
      | Prometheus metrics | a scrape body                                            | prom |
