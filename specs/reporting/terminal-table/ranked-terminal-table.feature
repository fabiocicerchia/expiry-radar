@domain:reporting @area:terminal-table @from:output/table
Feature: Reading the ranked inventory in the terminal
  Operators running a scan at the console see every expiring item in one aligned table, ranked and explained, so they can tell at a glance what to renew first and why.

  Background:
    Given the operator runs a scan with the table output format

  @id:reporting.terminal-table.001 @rule:reporting.terminal-table.ranked-row-with-explanation @verified @source:internal/output/table.go#renderTable @test:internal/output/output_test.go#TestTableShowsWhy
  Scenario: Each expiring item is one row carrying its priority, blast radius and the reason for its rank
    Given the inventory holds a TLS certificate "checkout.example.com" in namespace "payments" from source "k8s:secret" expiring in 12 days
    And ranking gave it priority 0.88 and blast radius 0.9 because it is "base tls_cert, internet-facing, production"
    When the operator views the scan results
    Then the header shows the aligned columns PRIORITY, BLAST, EXPIRES IN, KIND, NAME, SOURCE and WHY
    And the certificate's row shows "0.88", "0.90", "12d", "tls_cert", "payments/checkout.example.com", "k8s:secret" and "base tls_cert, internet-facing, production"
    And rows appear in the order the ranking produced

  @id:reporting.terminal-table.002 @rule:reporting.terminal-table.time-left-wording @verified @source:internal/output/output.go#humanDays @test:internal/output/output_test.go#TestTableFlagsExpiredInsteadOfSayingZero @test:internal/output/output_test.go#TestTableShowsWhy
  Scenario Outline: Time left is shown in whole days, and an expired item says how long ago it expired
    Given an item has <days_left> days left
    When the operator views the scan results
    Then its EXPIRES IN column reads "<shown>"

    Examples:
      | days_left | shown          |
      | 200       | 200d           |
      | 12.7      | 12d            |
      | 0.4       | today          |
      | -4        | EXPIRED 4d ago |
      | -0.5      | EXPIRED 0d ago |

  @id:reporting.terminal-table.003 @rule:reporting.terminal-table.namespaced-name @inferred @source:internal/output/output.go#displayName
  Scenario Outline: An item from a namespace is named with its namespace, never twice
    Given an item is named "<name>" in namespace "<namespace>"
    When the operator views the scan results
    Then its NAME column reads "<shown>"

    Examples:
      | name                  | namespace | shown                         |
      | checkout.example.com  | payments  | payments/checkout.example.com |
      | payments/checkout-tls | payments  | payments/checkout-tls         |
      | example.com           |           | example.com                   |

  @id:reporting.terminal-table.004 @rule:reporting.terminal-table.empty-inventory-notice @inferred @source:internal/output/table.go#renderTable
  Scenario: An empty scan says nothing is expiring or no source was enabled, instead of a bare header
    Given the scan found no expiring items
    When the operator views the scan results
    Then the table shows only its header
    And below it reads "(nothing expiring — or no sources were enabled)"
