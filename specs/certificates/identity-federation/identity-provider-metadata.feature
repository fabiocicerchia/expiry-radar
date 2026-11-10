@domain:certificates @area:identity-federation @from:source/federation
Feature: Watching identity-provider metadata for expiry
  Serves operators who rely on SAML federated login by surfacing the two deadlines nobody owns, the IdP signing certificates and the metadata's own validUntil, before logins stop working.

  Background:
    Given the operator has configured a federation provider named "corp-idp" with a metadata URL such as https://idp.example.com/metadata

  @id:certificates.identity-federation.001 @rule:certificates.identity-federation.certificate-and-metadata-are-separate-deadlines @verified @source:internal/source/federation.go#federationItems @test:internal/source/identity_test.go#TestFederationReportsBothTheCertificateAndTheMetadataExpiry
  Scenario Outline: A provider's certificates and its metadata validity are reported as separate deadlines
    Given the metadata document is valid until 2026-11-22
    And it embeds a key with use "<use>" whose certificate expires on 2026-12-18
    When the operator runs a scan
    Then the inventory lists "corp-idp metadata" as a trust anchor expiring on 2026-11-22
    And the inventory lists "corp-idp <shown use> certificate" as a trust anchor expiring on 2026-12-18
    And the certificate item is labelled with use "<shown use>" and the certificate's issuer and subject

    Examples:
      | use        | shown use              |
      | signing    | signing                |
      | encryption | encryption             |
      |            | signing and encryption |

  @id:certificates.identity-federation.002 @rule:certificates.identity-federation.duplicate-certificate-reported-once @verified @source:internal/source/federation.go#federationItems @test:internal/source/identity_test.go#TestFederationDedupesACertificateListedTwice
  Scenario: A certificate listed under several key uses is reported once
    Given the provider lists the same certificate under both a signing key and an encryption key
    When the operator runs a scan
    Then the inventory holds exactly one certificate item for that provider
    And it is labelled with the first use listed, "signing"

  @id:certificates.identity-federation.003 @rule:certificates.identity-federation.marked-public-with-operator-labels @inferred @source:internal/source/federation.go#federationItems
  Scenario: Federation findings are ranked as publicly reachable and carry the operator's labels
    Given the provider is configured with labels env "prod" and criticality "high"
    When the operator runs a scan
    Then every item from that provider is marked public
    And every item carries env "prod" and criticality "high"

  @id:certificates.identity-federation.004 @rule:certificates.identity-federation.unreadable-dates-silently-dropped @inferred @source:internal/source/federation.go#federationItems @questionable
  Scenario Outline: A metadata validity date or certificate the tool cannot read is left out without a warning
    Given the metadata declares validity until "<valid until>"
    And it embeds one signing certificate expiring on 2026-12-18 and one certificate that is not valid base64 DER
    When the operator runs a scan
    Then the inventory lists only "corp-idp signing certificate" expiring on 2026-12-18
    And no warning mentions the missing metadata deadline or the unreadable certificate

    Examples:
      | valid until         |
      | 2026-11-22T00:00:00 |
      | 2026-11-22          |
