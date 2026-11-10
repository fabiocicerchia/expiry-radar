@domain:credentials @area:code-hosting-tokens @from:source/github
Feature: Tracking GitHub token and signing-key expiry
  Gives platform operators the expiry dates of the GitHub credentials the API exposes (org fine-grained personal access tokens and the user's GPG signing keys), so a lapsing token or key is ranked before it breaks automation or signed-commit pushes.

  Background:
    Given the github source is enabled

  @id:credentials.code-hosting-tokens.001 @rule:credentials.github.gpg-denial-message @inferred @source:internal/source/github.go#ghGet @questionable
  Scenario: A denied GPG key listing is reported with the organization-owner hint
    Given GitHub refuses the token's request for the user's GPG keys with 403 Forbidden
    When the operator runs a scan
    Then the run warns "gpg-keys: GET /user/gpg_keys: 403 Forbidden — listing an organization's tokens needs an owner credential"
    And no GPG keys are reported
