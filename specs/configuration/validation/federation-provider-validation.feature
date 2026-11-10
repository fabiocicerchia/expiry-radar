@domain:configuration @area:validation @from:source/federation
Feature: Validating federation provider configuration
  Serves operators by refusing a federation provider at config load when its metadata could never be fetched, instead of failing at scan time.

  @id:configuration.validation.002 @rule:configuration.validation.federation-url-must-be-http @verified @source:internal/source/federation.go#ValidateFederation @test:internal/source/identity_test.go#TestFederationRejectsAURLItCouldNeverFetch
  Scenario Outline: A federation provider without a fetchable http or https URL is refused
    Given the config declares a federation provider at position 0 with url "<url>"
    When the operator loads the configuration
    Then the configuration is rejected with "<error>"

    Examples:
      | url                            | error                                                                     |
      |                                | federation[0]: url is required                                            |
      | idp.example.com/metadata       | federation[0] (idp.example.com/metadata): url must be http or https       |
      | ftp://idp.example.com/metadata | federation[0] (ftp://idp.example.com/metadata): url must be http or https |
