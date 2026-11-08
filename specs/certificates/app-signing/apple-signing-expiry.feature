@domain:certificates @area:app-signing @from:source/apple
Feature: Tracking Apple signing certificates and provisioning profiles
  Mobile release owners see Apple signing certificates and provisioning profiles from App Store Connect in the ranked inventory before an annual lapse surfaces as a broken CI build.

  Background:
    Given the apple source is enabled with an App Store Connect issuer ID, key ID and .p8 private key file

  @id:certificates.app-signing.001 @rule:app-signing.apple.all-pages-read @verified @source:internal/source/apple.go#applePages @test:internal/source/apple_test.go#TestAppleFollowsItsNextLink
  Scenario: Every page of certificates and profiles is read, not just the first
    Given App Store Connect returns certificates over two pages, the second holding "cert-p2"
    When the operator runs a scan
    Then the inventory includes "certificate/cert-p2"
    And no warning is reported for the certificates

  @id:certificates.app-signing.002 @rule:app-signing.apple.incomplete-listing-warned @inferred @source:internal/source/apple.go#applePages
  Scenario Outline: A listing that cannot be read to the end keeps what was read and says why the rest is missing
    Given App Store Connect <condition> while listing certificates
    When the operator runs a scan
    Then the certificates already read appear in the inventory
    And the apple source warns "certificates: <message>"

    Examples:
      | condition                            | message                                             |
      | still has more pages after 40        | stopped after 40 pages; the rest were not read      |
      | points the next page at another host | the API returned a continuation URL on another host |
