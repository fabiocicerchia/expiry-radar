@domain:cloud-providers @area:aws-coverage-gaps @from:source/awsgaps
Feature: Tracking AWS expiries beyond certificates, keys and secrets
  Gives operators of an AWS account RDS CA certificates, private CAs, legacy IAM server certificates, SAML providers and Route 53 domain registrations in the same ranked inventory, because these are the expiries nobody remembers to check.

  Background:
    Given the AWS source is enabled for account 111122223333 with read-only credentials

  @id:cloud-providers.aws-coverage-gaps.001 @rule:cloud.aws-gaps.rds-instance-certificate-reported @verified @source:internal/source/awsgaps.go#rdsCertificates @test:internal/source/awsgaps_test.go#TestRDSReportsBothTheBundleAndWhatEachInstanceIsPinnedTo
  Scenario: Each RDS database is reported with the certificate it actually presents
    Given the RDS database "payments" runs postgres, is publicly accessible, and presents CA "rds-ca-rsa2048-g1" valid until 2027-05-27
    When the operator runs a scan
    Then the inventory lists "rds/payments" as a TLS certificate from "aws:rds" expiring 2027-05-27
    And the item records CA "rds-ca-rsa2048-g1" and engine "postgres"
    And the item is marked as publicly reachable

  @id:cloud-providers.aws-coverage-gaps.002 @rule:cloud.aws-gaps.rds-ca-bundle-not-default-is-not-in-use @verified @source:internal/source/awsgaps.go#rdsCertificates @test:internal/source/awsgaps_test.go#TestRDSReportsBothTheBundleAndWhatEachInstanceIsPinnedTo @questionable
  Scenario Outline: A regional RDS CA that is not the account default is reported as not in use
    Given the region offers RDS CA "rds-ca-rsa2048-g1" valid until 2027-05-27
    And <default>
    When the operator runs a scan
    Then the inventory lists "rds-ca/rds-ca-rsa2048-g1" as an intermediate CA from "aws:rds-ca"
    And the item's in-use mark is <in_use>

    Examples:
      | default                                                 | in_use                    |
      | the account has not overridden its default RDS CA to it | false                     |
      | the account has overridden its default RDS CA to it     | unset (treated as in use) |

  @id:cloud-providers.aws-coverage-gaps.003 @rule:cloud.aws-gaps.private-ca-is-trust-anchor @verified @source:internal/source/awsgaps.go#privateCAs @test:internal/source/awsgaps_test.go#TestPrivateCAsAreTrustAnchors
  Scenario Outline: An ACM private CA is reported as a trust anchor, and an inactive one as not in use
    Given the account has a private CA with common name "<common_name>" in status <status>, valid until 2027-03-08
    When the operator runs a scan
    Then the inventory lists "<name>" as a trust anchor from "aws:acm-pca" expiring 2027-03-08
    And the item's in-use mark is <in_use>

    Examples:
      | common_name         | status   | name                                                                        | in_use                    |
      | payments-issuing-ca | ACTIVE   | private-ca/payments-issuing-ca                                              | unset (treated as in use) |
      | (none)              | DISABLED | private-ca/arn:aws:acm-pca:eu-west-1:111122223333:certificate-authority/old | false                     |

  @id:cloud-providers.aws-coverage-gaps.004 @rule:cloud.aws-gaps.iam-server-certs-and-saml-providers-reported-independently @inferred @source:internal/source/awsgaps.go#iamCertificates
  Scenario Outline: Legacy IAM server certificates and SAML providers are both reported, and one being denied keeps the other
    Given the account has the IAM server certificate "legacy-elb" expiring 2027-01-15
    And the account has the SAML provider "arn:aws:iam::111122223333:saml-provider/corp-okta" valid until 2027-02-01
    And <denied>
    When the operator runs a scan
    Then the inventory lists <listed>
    And the scan reports <warning>

    Examples:
      | denied                                | listed                                                                               | warning                                               |
      | nothing is denied                     | "server-cert/legacy-elb" as a TLS certificate and "saml/corp-okta" as a trust anchor | no warning for this service                           |
      | listing server certificates is denied | only "saml/corp-okta" as a trust anchor                                              | a warning beginning "iam-certs: server certificates:" |
      | listing SAML providers is denied      | only "server-cert/legacy-elb" as a TLS certificate                                   | a warning beginning "iam-certs: saml providers:"      |

  @id:cloud-providers.aws-coverage-gaps.005 @rule:cloud.aws-gaps.route53-auto-renew-is-managed-renewal @verified @source:internal/source/awsgaps.go#route53Domains @test:internal/source/awsgaps_test.go#TestRoute53DomainsAutoRenewIsDeRanked
  Scenario Outline: A Route 53 domain on auto-renew is marked as managed renewal; every registration counts as public
    Given the account registers "<domain>" with auto-renew <auto_renew>, expiring 2026-12-23
    When the operator runs a scan
    Then the inventory lists "<domain>" as a domain from "aws:route53domains" expiring 2026-12-23
    And the item is marked as publicly reachable
    And the item's renewal state is <renewal>

    Examples:
      | domain             | auto_renew | renewal                   |
      | auto.example.com   | on         | managed                   |
      | manual.example.com | off        | unset (full blast radius) |

  @id:cloud-providers.aws-coverage-gaps.006 @rule:cloud.aws-gaps.route53-domains-read-whatever-the-scan-region @inferred @source:internal/source/awsgaps.go#route53Domains
  Scenario: Route 53 domain registrations are read whatever region the account is scanned in
    Given the AWS source is set to scan region "eu-west-1"
    And the account registers "shop.example.com" in Route 53
    When the operator runs a scan
    Then the inventory lists "shop.example.com" as a domain
    And no warning about route53domains is reported
