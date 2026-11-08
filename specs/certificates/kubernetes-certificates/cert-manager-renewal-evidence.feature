@domain:certificates @area:kubernetes-certificates @from:source/k8scert
Feature: Checking that cert-manager renewal is actually working
  Lets an operator see which cluster certificates cert-manager is reliably renewing and which it has failed to issue or renew, so the ones nobody is renewing rise above the ones that will renew themselves.

  Background:
    Given Given cert-manager collection is enabled for the cluster

  @id:certificates.kubernetes-certificates.002 @rule:certificates.cert-manager.renewal-evidence-on-issued-secret @verified @source:internal/source/k8scert.go#renewalState @test:internal/source/k8s_test.go#TestAHealthyCertManagerCertificateMarksItsSecretManaged @test:internal/source/k8s_test.go#TestAStuckCertManagerRenewalIsNotMarkedManaged
  Scenario Outline: A certificate cert-manager manages is reported once, carrying whether its renewal is working
    Given the TLS secret "prod/shop-tls" holds a readable certificate
    And the cert-manager Certificate "shop" manages that secret
    And the Certificate's Ready status is <ready>
    And its next renewal is <renewal>
    When the operator runs a scan
    Then the inventory holds one finding "prod/shop-tls", dated by the secret's certificate
    And the finding names "shop" as its cert-manager Certificate
    And its renewal state is <state>

    Examples:
      | ready | renewal          | state   |
      | True  | 15 days from now | managed |
      | True  | 2 days ago       | stuck   |
      | False | 2 days ago       | stuck   |
      | False | not reported     | stuck   |
      | True  | not reported     | (none)  |

  @id:certificates.kubernetes-certificates.003 @rule:certificates.cert-manager.unissued-certificate-reported @verified @source:internal/source/k8scert.go#certificateItemFor @test:internal/source/k8s_test.go#TestACertificateWithNoIssuedSecretIsStillReported @test:internal/source/k8s_test.go#TestUnissuedIsOnlyClaimedForANamespaceWeActuallyRead @test:internal/source/k8s_test.go#TestADeletedSecretCannotBeCalledAHealthyRenewal
  Scenario Outline: A certificate cert-manager never produced is reported as missing and stuck
    Given the operator's scan could read the TLS secrets in namespace "prod"
    And the cert-manager Certificate "shop" for shop.example.com should produce the secret "prod/shop-tls"
    And no secret "prod/shop-tls" exists
    And the Certificate records an expiry of <recorded expiry>
    When the operator runs a scan
    Then the inventory holds a TLS certificate finding "prod/shop-tls" from "k8s:cert-manager"
    And its secret state is "missing" and its renewal state is "stuck", even when cert-manager reports it Ready
    And it expires <expires>

    Examples:
      | recorded expiry  | expires                 |
      | none             | at the time of the scan |
      | 40 days from now | 40 days from now        |

  @id:certificates.kubernetes-certificates.004 @rule:certificates.cert-manager.no-missing-claim-without-reading-secrets @verified @source:internal/source/k8scert.go#certificateItemFor @test:internal/source/k8s_test.go#TestSkippingSecretsDoesNotMakeEveryCertificateLookUnissued @test:internal/source/k8s_test.go#TestUnissuedIsOnlyClaimedForANamespaceWeActuallyRead @test:internal/source/k8s_test.go#TestAnUnreadSecretCannotEarnTheManagedDeRank
  Scenario Outline: A certificate is never called missing or healthy in a namespace whose secrets were not read
    Given the operator's scan <how> the TLS secrets in namespace "staging"
    And the cert-manager Certificate "dash" in "staging" records an expiry 40 days from now and a renewal 15 days from now, and reports Ready
    When the operator runs a scan
    Then the inventory holds a finding "staging/dash-tls" from "k8s:cert-manager" expiring 40 days from now
    And it carries no "missing" secret state
    And its renewal state is not "managed"

    Examples:
      | how                          |
      | skips                        |
      | is denied permission to list |

  @id:certificates.kubernetes-certificates.005 @rule:certificates.cert-manager.unreadable-secret-falls-back-to-certificate @verified @source:internal/source/k8scert.go#certificateItemFor @test:internal/source/k8s_test.go#TestACertificateWhoseSecretIsUnreadableIsStillReported
  Scenario: A certificate whose secret cannot be parsed is still reported, dated by cert-manager
    Given the secret "prod/shop-tls" exists but its certificate data is corrupt
    And the cert-manager Certificate "shop" records an expiry 30 days from now
    When the operator runs a scan
    Then the inventory holds a finding "prod/shop-tls" from "k8s:cert-manager" expiring 30 days from now
    And its secret state is "unreadable"

  @id:certificates.kubernetes-certificates.006 @rule:certificates.cert-manager.unreadable-undated-certificate-dropped @inferred @source:internal/source/k8scert.go#certificateItemFor @questionable
  Scenario: A certificate whose secret is corrupt and whose Certificate records no expiry disappears from the report
    Given the secret "prod/shop-tls" exists but its certificate data is corrupt
    And the cert-manager Certificate "shop" manages it but records no expiry
    When the operator runs a scan
    Then the inventory holds no finding for "prod/shop-tls"
    And the scan reports no warning about it
