@domain:configuration @area:manual-inventory @from:source/manual
Feature: Declaring expiries by hand
  Lets an operator record things no API can discover (a domain at a registrar with no RDAP, a hand-rotated credential, a support contract) so they are ranked alongside everything discovered instead of being forgotten.

  Background:
    Given the operator lists entries under "manual" in the expiry-radar config file

  @id:configuration.manual-inventory.001 @rule:configuration.manual-inventory.reported-with-ranking-signals @verified @source:internal/source/manual.go#ManualSource @test:internal/source/manual_test.go#TestManualSourceReportsWhatWasRecorded
  Scenario: A hand-declared entry appears in the inventory with its namespace and labels, marked as manual
    Given a manual entry named "acme-corp.co.uk" of kind "domain" expiring "2027-03-01"
    And the entry has namespace "corp" and label "public" set to "true"
    When the operator runs a scan
    Then the inventory contains "acme-corp.co.uk" of kind "domain" with source "manual"
    And the item has namespace "corp" and keeps the label "public" = "true"
    And the item expires 2027-03-01 00:00 UTC

  @id:configuration.manual-inventory.002 @rule:configuration.manual-inventory.invalid-entry-refused-at-load @verified @source:internal/source/manual.go#ValidateManual @test:internal/source/manual_test.go#TestValidateManualRejectsEntriesThatWouldRankWrongly @test:internal/source/manual_test.go#TestValidateManualRejectsHalfARotationPolicy @test:internal/source/manual_test.go#TestValidateManualNamesTheOffendingEntry
  Scenario Outline: A manual entry that would rank on a number nobody chose stops the config from loading, naming the entry
    Given the config file's manual entry 1 is named "broken.example.com"
    And that entry has <problem>
    When the operator runs a scan with that config file
    Then the configuration is refused and no scan runs
    And the error names manual item 1 "broken.example.com"
    And the error says "<message>"

    Examples:
      | problem                                                                  | message                            |
      | kind "tls-cert" (a typo for tls_cert)                                    | unknown kind "tls-cert"            |
      | no kind                                                                  | no kind                            |
      | no expires and no rotation policy                                        | no expires date                    |
      | created "2026-01-01" but no maxKeyAgeDays                                | no expires date                    |
      | expires "soon"                                                           | is neither YYYY-MM-DD nor RFC 3339 |
      | expires "03/01/2027"                                                     | is neither YYYY-MM-DD nor RFC 3339 |
      | maxKeyAgeDays 90 but no created                                          | maxKeyAgeDays needs created        |
      | created "last spring" with maxKeyAgeDays 90                              | is neither YYYY-MM-DD nor RFC 3339 |
      | expires "2027-03-01" and also created "2026-01-01" with maxKeyAgeDays 90 | both expires and a rotation policy |

  @id:configuration.manual-inventory.003 @rule:configuration.manual-inventory.nameless-entry-refused @verified @source:internal/source/manual.go#ValidateManual @test:internal/source/manual_test.go#TestValidateManualRejectsEntriesThatWouldRankWrongly
  Scenario: A manual entry without a name is refused, identified by its position
    Given the config file's manual entry 0 has kind "domain" and expires "2027-03-01" but no name
    When the operator runs a scan with that config file
    Then the configuration is refused and no scan runs
    And the error says "manual item 0 has no name"

  @id:configuration.manual-inventory.004 @rule:configuration.manual-inventory.every-kind-declarable @verified @source:internal/source/manual.go#Kinds @test:internal/source/manual_test.go#TestValidateManualAcceptsEveryKindTheToolReports
  Scenario Outline: Every kind the tool reports can be declared by hand
    Given a manual entry of kind "<kind>" expiring "2027-03-01"
    When the operator runs a scan with that config file
    Then the configuration is accepted
    And the entry is reported with kind "<kind>"

    Examples:
      | kind            |
      | tls_cert        |
      | intermediate_ca |
      | secret          |
      | iam_access_key  |
      | vault_lease     |
      | domain          |
      | trust_anchor    |
