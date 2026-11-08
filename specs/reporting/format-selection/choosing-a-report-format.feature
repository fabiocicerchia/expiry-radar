@domain:reporting @area:format-selection @from:output/output
Feature: Choosing a report format
  Lets an operator pick how a ranked inventory comes out, as a terminal table or as a feed for CI, a calendar, a metrics scrape or an inbox, with the same names and time-left wording in every human-readable format.

  Background:
    Given an inventory of expiring items has been collected and ranked

  @id:reporting.format-selection.001 @rule:reporting.format.supported-formats @verified @source:internal/output/output.go#RenderAt @test:internal/output/output_test.go#TestTableShowsWhy @test:internal/output/output_test.go#TestJSONCountsExpired @test:internal/output/output_test.go#TestICalIsWellFormed @test:internal/output/output_test.go#TestPrometheusExposition @test:internal/output/output_test.go#TestHTMLReportIsSelfContainedAndFlagsTheDeadline
  Scenario Outline: The report can be produced in any of the five supported formats
    Given the inventory holds the TLS certificate "checkout.example.com" in namespace "payments", expiring in 12 days
    When the operator runs a scan with format "<format>"
    Then the report is written as <shape>

    Examples:
      | format     | shape                                                           |
      | table      | a ranked table with the day count and the reason for each rank  |
      | json       | a JSON document with a count of items and of expired items      |
      | ical       | an iCalendar feed with one all-day event per item               |
      | prometheus | Prometheus metrics with expiry and blast radius gauges per item |
      | html       | a self-contained HTML page with no external resources           |

  @id:reporting.format-selection.002 @rule:reporting.format.unknown-format-refused @verified @source:internal/output/output.go#RenderAt @test:internal/output/output_test.go#TestUnknownFormatIsAnError @questionable
  Scenario: An unknown report format is refused with the list of valid formats
    Given the inventory holds at least one expiring item
    When the operator runs a scan with format "csv"
    Then no report is written
    And the operator sees the error 'unknown format "csv" (want one of [table json ical prometheus html])'
    And the command exits with status 2

  @id:reporting.format-selection.003 @rule:reporting.time-left.expired-never-reads-as-zero @verified @source:internal/output/output.go#humanDays @test:internal/output/output_test.go#TestTableShowsWhy @test:internal/output/output_test.go#TestTableFlagsExpiredInsteadOfSayingZero @test:internal/output/output_test.go#TestHTMLReportIsSelfContainedAndFlagsTheDeadline @questionable
  Scenario Outline: Time left is shown in whole days, and an expired item is flagged as expired rather than as zero days
    Given the inventory holds an item with <days_left> days left
    When the operator views the report as a table
    Then the item's time left reads "<shown>"

    Examples:
      | days_left | shown          |
      | 12        | 12d            |
      | 12.9      | 12d            |
      | 0.4       | today          |
      | -4        | EXPIRED 4d ago |
      | -0.5      | EXPIRED 0d ago |

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
