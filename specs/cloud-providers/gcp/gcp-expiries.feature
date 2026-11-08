@domain:cloud-providers @area:gcp @from:source/gcp
Feature: Finding what expires in Google Cloud
  Operators who run on Google Cloud get every certificate, secret deadline and service-account key in their named projects in the ranked inventory, with a clear sentence for anything that could not be read.

  Background:
    Given the Google Cloud source is enabled

  @id:cloud-providers.gcp.001 @rule:cloud.gcp.unreadable-api-explains-cause @verified @source:internal/source/gcp.go#gcpRaw @test:internal/source/gcp_test.go#TestGCPA404IsNotACleanProject
  Scenario Outline: An API that cannot be read is reported with what to check
    Given the project "acme-prod" is configured
    And the <service> API answers <status> for "acme-prod"
    When the operator runs a scan
    Then the scan reports a warning for <service> containing "<hint>"
    And no items from <service> in "acme-prod" are reported

    Examples:
      | service            | status           | hint                                                        |
      | compute            | 403 Forbidden    | the service account is missing a viewer role for this API   |
      | secretmanager      | 401 Unauthorized | the service account is missing a viewer role for this API   |
      | certificatemanager | 404 Not Found    | not found — check the project name, or enable the API on it |

  @id:cloud-providers.gcp.002 @rule:cloud.gcp.every-page-is-reported @verified @source:internal/source/gcp.go#gcpPages @test:internal/source/gcp_test.go#TestGCPFollowsPageTokens
  Scenario: Findings on every result page are reported, not just the first
    Given the project "acme-prod" holds compute certificates spread over two result pages
    When the operator runs a scan
    Then the certificates from both pages are reported
    And a listing longer than 50 pages is reported as a warning ending "stopped after 50 pages"
