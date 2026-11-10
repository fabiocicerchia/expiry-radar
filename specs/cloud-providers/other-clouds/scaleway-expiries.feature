@domain:cloud-providers @area:other-clouds @from:source/scaleway
Feature: Tracking Scaleway domains, load-balancer certificates and API keys
  Gives operators who run on Scaleway one ranked view of their registered domains, zonal load-balancer certificates and expiring IAM API keys, including whether each domain will actually renew.

  Background:
    Given the Scaleway source is enabled
    And the operator has set $SCW_SECRET_KEY to a Scaleway secret key

  @id:cloud-providers.other-clouds.002 @rule:cloud.scaleway.no-deadline-without-expiry @verified @source:internal/source/cloudflare.go#cfTime @test:internal/source/hosting_test.go#TestScalewayKeysWithoutAnExpiryAreNotDeadlines @questionable
  Scenario Outline: A Scaleway item with no readable expiry is left out of the inventory
    Given Scaleway holds a <what> whose expiry is <expiry>
    When the operator runs a scan
    Then the inventory does not list "<name>"
    And no warning is shown for it

    Examples:
      | what                                    | name                | expiry                | what_note                             |
      | API key described as forever            | forever             | not set               | keys may be created without an expiry |
      | registered domain pending.example.com   | pending.example.com | empty                 | no registration date yet              |
      | load-balancer certificate fr-par-1/edge | fr-par-1/edge       | not a valid timestamp | malformed API value                   |

  @id:cloud-providers.other-clouds.003 @rule:cloud.scaleway.truncated-listing-warns @verified @source:internal/source/scaleway.go#truncationWarning @test:internal/source/hosting_test.go#TestScalewayTruncationIsNotSilent
  Scenario: A Scaleway listing cut short at the page limit says how many were not read
    Given the Scaleway account holds 500 registered domains
    And the source reads only the first 100
    When the operator runs a scan
    Then the inventory lists the domains that were read
    And the scan warns "read 100 of 500; the rest were not fetched" for domains
