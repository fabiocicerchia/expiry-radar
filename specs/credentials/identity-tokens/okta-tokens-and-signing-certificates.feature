@domain:credentials @area:identity-tokens @from:source/okta
Feature: Tracking Okta API tokens and SAML signing certificates
  Serves identity and platform operators who need to know before an Okta admin token or a SAML app's signing certificate lapses, because either one silently stops sign-ins or every other Okta finding.

  Background:
    Given the Okta source is enabled for the org https://acme.example.okta.com
    And a read-only admin API token is set in OKTA_API_TOKEN

  @id:credentials.identity-tokens.001 @rule:credentials.okta.insufficient-token-explained @verified @source:internal/source/okta.go#oktaPage @test:internal/source/identity_test.go#TestOktaExplainsAnInsufficientToken
  Scenario: A token without enough privilege is reported with the role it needs
    Given the API token is refused by Okta with 403 Forbidden
    When the operator runs a scan
    Then the Okta source reports that "the API token needs a read-only admin role"
    And the message names the request that was refused, e.g. "GET /api/v1/api-tokens: 403 Forbidden"

  @id:credentials.identity-tokens.002 @rule:credentials.okta.complete-listing-or-says-not @verified @source:internal/source/okta.go#oktaGet @test:internal/source/identity_test.go#TestOktaFollowsTheLinkHeader @questionable
  Scenario Outline: Every page of applications and tokens is read, and a truncated read says so
    Given the org's applications span <pages> pages of results
    When the operator runs a scan
    Then <outcome>

    Examples:
      | pages | outcome                                                                                                                                    |
      | 2     | signing certificates from applications on both pages are in the inventory                                                                  |
      | 41    | the Okta source warns "GET /api/v1/apps?limit=200: stopped after 40 pages; the rest were not read" and reports no application certificates |

  @id:credentials.identity-tokens.003 @rule:credentials.okta.token-never-sent-off-tenant @inferred @source:internal/source/okta.go#oktaGet
  Scenario: The Okta token is never sent to a host other than the configured org
    Given Okta's next-page link points at https://attacker.example/api/v1/apps
    When the operator runs a scan
    Then the next page is not requested
    And the Okta source warns that "the API returned a continuation URL on another host"
