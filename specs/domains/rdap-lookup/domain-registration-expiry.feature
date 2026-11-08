@domain:domains @area:rdap-lookup @from:source/rdap
Feature: Reading domain registration expiry
  Lets an operator see when each listed domain registration lapses, read from public RDAP or WHOIS with no registrar credentials, because a lapsed domain takes everything under it down at once.

  Background:
    Given the operator has listed domains to check in the config or with -domains

  @id:domains.rdap-lookup.001 @rule:domains.whois.recognised-expiry-formats @verified @source:internal/source/rdap.go#whoisExpiration @test:internal/source/source_test.go#TestWhoisExpirationReadsTheFormatsRegistriesActuallyUse
  Scenario Outline: WHOIS expiry dates are recognised under the labels and date formats registries use
    Given RDAP has no answer for the domain's TLD
    And the registry's WHOIS reply contains the line "<line>"
    When the operator runs a scan
    Then the domain is listed expiring on <expires>

    Examples:
      | line                                       | expires                 |
      | Expire Date:        2026-08-18             | 2026-08-18 00:00 UTC    |
      | Expiry date:  13-Dec-2034                  | 2034-12-13 00:00 UTC    |
      | paid-till:     2026-09-30T21:00:00Z        | 2026-09-30 21:00 UTC    |
      | Registry Expiry Date: 2027-03-04T05:06:07Z | 2027-03-04 05:06:07 UTC |
      | renewal date: 2028-01-02                   | 2028-01-02 00:00 UTC    |
      | valid until: 02.01.2028                    | 2028-01-02 00:00 UTC    |
