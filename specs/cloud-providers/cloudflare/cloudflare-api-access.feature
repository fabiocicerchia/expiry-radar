@domain:cloud-providers @area:cloudflare @from:source/cloudflare-api
Feature: Reading the Cloudflare API
  Operators scanning a Cloudflare account need every expiring item the token can see, and a clear sentence when Cloudflare refuses or fails a read instead of a silently short report.

  Background:
    Given the Cloudflare source is enabled with an API token in $CLOUDFLARE_API_TOKEN

  @id:cloud-providers.cloudflare.001 @rule:cloudflare.api.denied-read-names-missing-permission @verified @source:internal/source/cloudflare_api.go#cfGet @test:internal/source/cloudflare_test.go#TestCloudflareOneDeniedScopeKeepsTheOthersFindings @test:internal/source/cloudflare_test.go#TestCloudflareDeniedAccountTokensKeepsTheRestOfTheScope @questionable
  Scenario Outline: A refused read tells the operator the token lacks a read permission
    Given Cloudflare refuses the token's request to list <resource> with status <status>
    When the operator runs a scan
    Then the scan reports a warning naming the refused request and the status <status>
    And the warning says the API token is missing a read permission for this resource
    And findings from the reads Cloudflare allowed are still in the report

    Examples:
      | resource                     | status           |
      | the user's API tokens        | 403 Forbidden    |
      | the account's API tokens     | 403 Forbidden    |
      | a zone's custom certificates | 401 Unauthorized |

  @id:cloud-providers.cloudflare.002 @rule:cloudflare.api.reported-failure-is-an-error @verified @source:internal/source/cloudflare_api.go#cfGet @test:internal/source/cloudflare_test.go#TestCloudflareTreatsSuccessFalseAsAnError
  Scenario Outline: A response Cloudflare marks as failed is reported, not read as an empty result
    Given Cloudflare answers a list request with status 200 but marks it as failed with <errors>
    When the operator runs a scan
    Then the scan reports a warning containing <message>
    And nothing from that request is counted as an empty, healthy result

    Examples:
      | errors                            | message                                       |
      | error 9109 "Invalid access token" | 9109 Invalid access token                     |
      | no error detail                   | the API reported failure with no error detail |

  @id:cloud-providers.cloudflare.003 @rule:cloudflare.api.reads-every-page @inferred @source:internal/source/cloudflare_api.go#cfGet
  Scenario Outline: Items beyond the first page of results are included
    Given the token can see <count> custom certificates in zone shop.example.com, spread over <pages> pages
    When the operator runs a scan
    Then the report lists all <count> custom certificates for shop.example.com

    Examples:
      | count | pages |
      | 50    | 1     |
      | 120   | 3     |

  @id:cloud-providers.cloudflare.004 @rule:cloudflare.api.page-limit-reports-truncation @inferred @source:internal/source/cloudflare_api.go#cfGet @questionable
  Scenario: A listing that never ends is cut off at 40 pages and the cut is reported
    Given Cloudflare keeps reporting more pages of certificate packs for zone shop.example.com than 40
    When the operator runs a scan
    Then the report includes the certificates read from the first 40 pages
    And the scan reports a warning that reading certificate packs for shop.example.com stopped after 40 pages
