@domain:reporting @area:format-selection @from:output/output
Feature: Choosing a report format
  Lets an operator pick how a ranked inventory comes out, as a terminal table or as a feed for CI, a calendar, a metrics scrape or an inbox, with the same names and time-left wording in every human-readable format.

  Background:
    Given an inventory of expiring items has been collected and ranked

  @id:reporting.format-selection.002 @rule:reporting.format.unknown-format-refused @verified @source:internal/output/output.go#RenderAt @test:internal/output/output_test.go#TestUnknownFormatIsAnError @questionable
  Scenario: An unknown report format is refused with the list of valid formats
    Given the inventory holds at least one expiring item
    When the operator runs a scan with format "csv"
    Then no report is written
    And the operator sees the error 'unknown format "csv" (want one of [table json ical prometheus html])'
    And the command exits with status 2

  @id:reporting.format-selection.004 @rule:reporting.item-name.namespace-qualified @verified @source:internal/output/output.go#displayName @test:internal/output/output_test.go#TestHTMLReportIsSelfContainedAndFlagsTheDeadline
  Scenario Outline: An item that belongs to a namespace is named with its namespace in front, never twice
    Given the inventory holds an item named "<name>" in namespace "<namespace>"
    When the operator views the report
    Then the item is shown as "<shown>"

    Examples:
      | name                 | namespace | shown                         |
      | checkout.example.com | payments  | payments/checkout.example.com |
      | payments/db-password | payments  | payments/db-password          |
      | example.com          |           | example.com                   |
