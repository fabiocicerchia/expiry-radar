@domain:certificates @area:tls-endpoints @from:source/tls
Feature: Reading certificates from live TLS endpoints
  Lets operators list the hosts they serve and get the certificate each host actually presents, including the intermediate CAs nobody else tracks, so an expiring cert or CA is found before it causes an outage.

  Background:
    Given the operator has listed TLS endpoints in the expiry-radar configuration

  @id:certificates.tls-endpoints.001 @rule:certificates.tls-endpoints.expired-certificate-still-reported @verified @source:internal/source/tls.go#insecureInspectionConfig @test:internal/source/source_test.go#TestTLSSourceStillReportsAnExpiredCertificate
  Scenario: An already-expired or untrusted certificate is still reported
    Given the endpoint "expired.example.com" serves a self-signed certificate that expired two days ago
    When the operator runs a scan
    Then the inventory contains a TLS certificate for "expired.example.com"
    And its expiry date is the certificate's expiry, two days in the past
    And the scan does not report "expired.example.com" as a failed endpoint

  @id:certificates.tls-endpoints.002 @rule:certificates.tls-endpoints.leaf-carries-certificate-and-operator-labels @verified @source:internal/source/tls.go#leafItem @test:internal/source/source_test.go#TestTLSSourceReadsTheChainFromALiveHandshake
  Scenario: The served certificate is reported under the endpoint's host with its names, issuer and the operator's labels
    Given the endpoint "shop.example.com" is labelled criticality "high"
    And it serves a certificate for "shop.example.com,www.shop.example.com" issued by "Example Issuing CA"
    When the operator runs a scan
    Then the inventory contains a "tls_cert" item named "shop.example.com" from source "tls:endpoint"
    And its expiry date is the certificate's expiry date
    And its hosts are "shop.example.com,www.shop.example.com" and its issuer is "Example Issuing CA"
    And it carries the certificate serial number and the label criticality "high"

  @id:certificates.tls-endpoints.003 @rule:certificates.tls-endpoints.reachable-endpoint-assumed-public @verified @source:internal/source/tls.go#leafItem @test:internal/source/source_test.go#TestTLSSourceReadsTheChainFromALiveHandshake @questionable
  Scenario Outline: A reachable endpoint is treated as public unless the operator says otherwise
    Given the endpoint "<host>" has the public label <operator_public>
    When the operator runs a scan
    Then the TLS certificate for "<host>" is reported with public "<reported_public>"

    Examples:
      | host                 | operator_public | reported_public |
      | shop.example.com     | not set         | true            |
      | intranet.example.com | "false"         | false           |

  @id:certificates.tls-endpoints.004 @rule:certificates.tls-endpoints.default-port-and-server-name @inferred @source:internal/source/tls.go#probe
  Scenario Outline: An endpoint without a port is probed on 443, and the server name defaults to the host
    Given an endpoint with host "<host>" and server name "<server_name>"
    When the operator runs a scan
    Then the scanner connects to "<address>" and requests the certificate for "<sni>"
    And the TLS certificate is reported under the name "<item_name>"

    Examples:
      | host              | server_name     | address              | sni              | item_name        |
      | shop.example.com  | not set         | shop.example.com:443 | shop.example.com | shop.example.com |
      | 203.0.113.10:8443 | api.example.com | 203.0.113.10:8443    | api.example.com  | 203.0.113.10     |

  @id:certificates.tls-endpoints.005 @rule:certificates.tls-endpoints.intermediates-reported-roots-skipped @inferred @source:internal/source/tls.go#intermediateItem
  Scenario: Intermediate CAs in the served chain are reported, self-signed roots are not
    Given the endpoint "shop.example.com" serves its certificate, the intermediate "Example Issuing CA" and the self-signed root "Example Root CA"
    When the operator runs a scan
    Then the inventory contains an "intermediate_ca" item named "Example Issuing CA" from source "tls:chain" with hosts "shop.example.com"
    And its expiry date is the intermediate's expiry date
    And the inventory contains no item for "Example Root CA"

  @id:certificates.tls-endpoints.006 @rule:certificates.tls-endpoints.shared-intermediate-reported-once @verified @source:internal/source/tls.go#mergeIntermediates @test:internal/source/source_test.go#TestMergeIntermediatesDeduplicatesAndCollectsHosts @questionable
  Scenario: An intermediate CA shared by several endpoints is reported once, listing every host it covers
    Given the endpoints "a.example.com" and "b.example.com" are both served through the intermediate "Example Issuing CA" with the same serial number
    And the endpoint "c.example.com" is served through "Other CA"
    When the operator runs a scan
    Then the inventory contains one "Example Issuing CA" item with hosts "a.example.com,b.example.com"
    And the inventory contains one "Other CA" item with hosts "c.example.com"

  @id:certificates.tls-endpoints.007 @rule:certificates.tls-endpoints.legacy-tls-not-inspected @inferred @source:internal/source/tls.go#insecureInspectionConfig @questionable
  Scenario: An endpoint that only offers TLS 1.0 or 1.1 is reported as failed, not inventoried
    Given the endpoint "legacy.example.com" only accepts TLS 1.1
    When the operator runs a scan
    Then the scan reports "legacy.example.com" as a failed endpoint
    And the inventory contains no certificate for "legacy.example.com"
