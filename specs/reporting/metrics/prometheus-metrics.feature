@domain:reporting @area:metrics @from:output/prometheus
Feature: Exposing expiries as Prometheus metrics
  Gives operators a scrape body of expiry, blast radius and priority per item so their existing Prometheus alerting can page on what expires next and matters most.

  Background:
    Given the operator runs a scan with the prometheus output format

  @id:reporting.metrics.001 @rule:reporting.metrics.per-item-expiry-and-rank-gauges @verified @source:internal/output/prometheus.go#renderPrometheus @test:internal/output/output_test.go#TestPrometheusExposition
  Scenario Outline: Every inventoried item is exposed with its time left, blast radius and priority
    Given the inventory holds the TLS certificate "checkout.example.com" in namespace "payments" from "k8s:secret"
    And it expires <time_left> after the scan
    And it has blast radius 0.9 and priority 0.88
    When the operator runs the scan
    Then the output declares expiry_radar_seconds_left, expiry_radar_blast_radius and expiry_radar_priority as gauges, each with a help line
    And expiry_radar_seconds_left for the certificate is <seconds>
    And expiry_radar_blast_radius for the certificate is 0.90
    And expiry_radar_priority for the certificate is 0.88

    Examples:
      | time_left                 | seconds |
      | 12 days                   | 1036800 |
      | -4 days (already expired) | -345600 |

  @id:reporting.metrics.002 @rule:reporting.metrics.item-count-by-kind @verified @source:internal/output/prometheus.go#renderPrometheus @test:internal/output/output_test.go#TestPrometheusExposition
  Scenario: The number of inventoried items is reported per kind
    Given the inventory holds 1 TLS certificate and 1 domain
    When the operator runs the scan
    Then the output declares expiry_radar_items as a gauge
    And expiry_radar_items{kind="domain"} is 1
    And expiry_radar_items{kind="tls_cert"} is 1
    And the kinds are listed in alphabetical order

  @id:reporting.metrics.003 @rule:reporting.metrics.low-cardinality-labels-only @verified @source:internal/output/prometheus.go#promLabels @test:internal/output/output_test.go#TestPrometheusExposition
  Scenario: Metrics carry only kind, name, namespace and source as labels
    Given the TLS certificate "checkout.example.com" in namespace "payments" from "k8s:secret" lists its hosts
    And the domain "example.com" from "domain:rdap" has no namespace
    When the operator runs the scan
    Then the certificate's samples are labelled kind="tls_cert", name="checkout.example.com", namespace="payments", source="k8s:secret"
    And no sample carries a hosts label or any other item detail
    And the domain's samples have no namespace label

  @id:reporting.metrics.004 @rule:reporting.metrics.label-values-escaped @verified @source:internal/output/prometheus.go#escapeLabel @test:internal/output/output_test.go#TestPrometheusEscapesLabelValues
  Scenario Outline: Odd characters in an item's name cannot break the scrape
    Given the inventory holds a TLS certificate named <raw_name>
    When the operator runs the scan
    Then the certificate's samples carry the label name=<escaped_name>

    Examples:
      | raw_name               | escaped_name  |
      | we"ird                 | "we\"ird"     |
      | back\slash             | "back\\slash" |
      | two (line break) lines | "two\nlines"  |
