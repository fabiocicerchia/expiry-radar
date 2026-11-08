@domain:reporting @area:json-export @from:output/json
Feature: Exporting the ranked inventory as JSON
  Gives scripts, CI jobs and the editor integrations a stable, machine-readable copy of the ranked inventory, so they can act on expiries without scraping the terminal table.

  Background:
    Given the operator runs a scan with the JSON output format

  @id:reporting.json-export.001 @rule:reporting.json-export.summary-totals @verified @source:internal/output/json.go#renderJSON @test:internal/output/output_test.go#TestJSONCountsExpired
  Scenario: The JSON report states when it was generated, how many items it holds and how many have already expired
    Given the inventory holds 2 ranked items
    And the domain "example.com" expired 1 day ago
    And the certificate "checkout.example.com" expires in 12 days
    When the operator exports the inventory as JSON
    Then "count" is 2
    And "expired" is 1
    And "generatedAt" is the time of the run, in UTC

  @id:reporting.json-export.002 @rule:reporting.json-export.item-fields @inferred @source:internal/output/json.go#jsonItem
  Scenario: Each exported item carries its ranking, its deadline and the reason it ranks where it does
    Given the certificate "checkout.example.com" in namespace "payments" from source "k8s:secret" expires in 12.3456 days
    And it has priority 0.88, blast radius 0.9 and the reason "base tls_cert, internet-facing, production"
    And it is labelled with hosts "checkout.example.com"
    When the operator exports the inventory as JSON
    Then the item shows "kind" "tls_cert", "name" "checkout.example.com", "namespace" "payments" and "source" "k8s:secret"
    And the item shows "priority" 0.88, "blastRadius" 0.9 and "daysLeft" 12.35
    And the item shows "expires" as a UTC timestamp and "why" "base tls_cert, internet-facing, production"
    And the item shows its labels, including the hosts "checkout.example.com"

  @id:reporting.json-export.003 @rule:reporting.json-export.optional-fields-omitted @inferred @source:internal/output/json.go#jsonItem
  Scenario: An item with no namespace or labels leaves those fields out of the export
    Given the domain "example.com" from source "domain:rdap" has no namespace and no labels
    When the operator exports the inventory as JSON
    Then the item for "example.com" has no "namespace" field
    And the item for "example.com" has no "labels" field

  @id:reporting.json-export.004 @rule:reporting.json-export.expired-flag @verified @source:internal/output/json.go#renderJSON @test:internal/output/output_test.go#TestJSONCountsExpired @questionable
  Scenario Outline: An item is marked expired only once its expiry has passed
    Given an item has <days left> days left
    When the operator exports the inventory as JSON
    Then the item shows "expired" <expired>
    And the report's "expired" total counts it: <counted>

    Examples:
      | days left | expired | counted |
      | -1        | true    | yes     |
      | -0.001    | true    | yes     |
      | 0.4       | false   | no      |
      | 12        | false   | no      |

  @id:reporting.json-export.005 @rule:reporting.json-export.ranked-order-and-empty @inferred @source:internal/output/json.go#renderJSON
  Scenario Outline: Items are exported in ranked order, and an empty inventory exports an empty list
    Given the inventory holds <items> ranked items
    When the operator exports the inventory as JSON
    Then "items" is <listed>
    And "count" is <items>

    Examples:
      | items | listed                              |
      | 0     | an empty list []                    |
      | 2     | the 2 items, highest priority first |
