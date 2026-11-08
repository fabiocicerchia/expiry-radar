@domain:reporting @area:html-report @from:output/html
Feature: Sharing a standalone HTML report of the ranked inventory
  Gives operators a single web page of everything that expires, ranked by blast radius and grouped by domain, that they can email or publish and that stakeholders can skim and filter without any tooling.

  Background:
    Given the operator runs a scan with the html output format

  @id:reporting.html-report.001 @rule:reporting.html-report.self-contained @verified @source:internal/output/html.go#renderHTML @test:internal/output/output_test.go#TestHTMLReportIsSelfContainedAndFlagsTheDeadline
  Scenario: The report is one file that needs nothing from the network
    Given the inventory holds a TLS certificate and a domain registration
    When the operator opens the generated report offline or as an email attachment
    Then all styling and the filter script are inside the file itself
    And the report references no http:// or https:// resource
    And the page renders fully without network access

  @id:reporting.html-report.002 @rule:reporting.html-report.untrusted-names-escaped @verified @source:internal/output/html.go#renderHTML @test:internal/output/output_test.go#TestHTMLEscapesNamesFromUntrustedSources
  Scenario: Names read from certificates and registries cannot inject markup or script into the report
    Given a collected certificate is named "<script>alert(1)</script>"
    When a stakeholder opens the report
    Then the name is shown as literal text
    And no script from the name runs in the page

  @id:reporting.html-report.003 @rule:reporting.html-report.headline-counts @verified @source:internal/output/html.go#countHorizons @test:internal/output/output_test.go#TestHTMLGroupsRowsUnderTheDomainTheyBelongTo
  Scenario: The report leads with how many items are tracked, expired, due within 14 and within 30 days, and which is soonest
    Given the inventory holds 4 items
    And "payments/db-password" expired 2 days ago
    And "*.example.com" expires in 9 days
    And "example.com" expires in 40 days and "Issuing CA" in 400 days
    When a stakeholder opens the report
    Then the header says it was generated at the run time in UTC, such as "generated 2026-08-01 12:00 UTC"
    And the summary shows Tracked 4, Expired 1, Next 14 days 1, Next 30 days 1
    And the Soonest card names "payments/db-password" with "EXPIRED 2d ago"
    And the Expired card is highlighted because at least one item has expired

  @id:reporting.html-report.004 @rule:reporting.html-report.grouped-by-owning-domain @verified @source:internal/output/html.go#groupOf @test:internal/output/output_test.go#TestHTMLGroupsRowsUnderTheDomainTheyBelongTo
  Scenario Outline: Each item is listed under the tracked domain it belongs to, or else its namespace
    Given the inventory includes the domain registrations "example.com" and "shop.example.com"
    And it also holds the item "<name>" of kind <kind> in namespace "<namespace>"
    When a stakeholder opens the report
    Then the item is listed in the group "<group>"
    And groups appear in the order of their highest-ranked item

    Examples:
      | name                 | kind         | namespace | group            |
      | example.com          | domain       |           | example.com      |
      | *.example.com        | tls_cert     |           | example.com      |
      | api.shop.example.com | tls_cert     | payments  | shop.example.com |
      | db-password          | secret       | payments  | payments         |
      | Issuing CA           | intermediate |           | shared chain     |
      | deploy-token         | api_token    |           | other            |

  @id:reporting.html-report.005 @rule:reporting.html-report.group-shows-worst-deadline @verified @source:internal/output/html.go#groupRows @test:internal/output/output_test.go#TestHTMLGroupsRowsUnderTheDomainTheyBelongTo
  Scenario: A group's heading shows its most urgent deadline, not its first row's
    Given the group "example.com" holds the domain expiring in 40 days and "*.example.com" expiring in 9 days
    And the domain is ranked above the certificate
    When a stakeholder opens the report
    Then the "example.com" heading reads "2 of 2 · soonest 9d"
    And the heading's status dot is coloured urgent

  @id:reporting.html-report.006 @rule:reporting.html-report.deadline-severity-per-row @verified @source:internal/output/html.go#severity @test:internal/output/output_test.go#TestHTMLReportIsSelfContainedAndFlagsTheDeadline @questionable
  Scenario Outline: Each row states its deadline in words and is coloured by how close it is
    Given the inventory holds "payments/checkout.example.com" with <days_left> days left
    When a stakeholder reads its row
    Then the Expires in column reads "<expires_in>"
    And the row is marked <severity>
    And the row also shows priority, blast radius, expiry date, kind, source and the ranking reason

    Examples:
      | days_left | expires_in     | severity |
      | -4        | EXPIRED 4d ago | expired  |
      | -0.5      | EXPIRED 0d ago | expired  |
      | 0.5       | today          | urgent   |
      | 12        | 12d            | urgent   |
      | 14.5      | 14d            | soon     |
      | 30        | 30d            | soon     |
      | 200       | 200d           | ok       |

  @id:reporting.html-report.007 @rule:reporting.html-report.interactive-filtering @inferred @source:internal/output/report.html.tmpl#controls @questionable
  Scenario Outline: A reader can narrow the report by text, by kind and to the next 30 days
    Given the report lists a TLS certificate due in 12 days and a domain due in 200 days
    When a stakeholder chooses the <filter> filter
    Then only <visible> stays visible
    And groups with no visible rows are hidden
    And the counter reads "1 of 2 items"

    Examples:
      | filter          | visible             |
      | text "checkout" | the TLS certificate |
      | kind "domain"   | the domain          |
      | "≤ 30 days"     | the TLS certificate |

  @id:reporting.html-report.008 @rule:reporting.html-report.empty-state @verified @source:internal/output/report.html.tmpl#report @test:internal/output/output_test.go#TestHTMLSaysSoWhenThereIsNothing
  Scenario: A report with nothing to show says so instead of rendering blank
    Given the scan found no expiring items
    When a stakeholder opens the report
    Then the page reads "Nothing expiring — or no sources were enabled."
    And the summary shows Tracked 0 and no Soonest card
