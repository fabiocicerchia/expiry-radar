@domain:credentials @area:registry-credentials @from:source/registry
Feature: Tracking registry credential expiry
  Gives operators the expiry dates of Harbor robot accounts and JFrog Artifactory access tokens, which pipelines create once and then forget. When one lapses, image pulls and deploys stop.

  Background:
    Given the operator has enabled the Harbor or JFrog source in the configuration

  @id:credentials.registry-credentials.001 @rule:credentials.registry.harbor-password-only-in-header @verified @source:internal/source/httpjson.go#basicAuth @test:internal/source/edge_test.go#TestHarborSendsBasicAuthInAHeader
  Scenario: The Harbor password is never placed in the request address
    Given the Harbor source is configured with username "admin" and a password from $HARBOR_PASSWORD
    When the operator runs a scan
    Then the password is sent only as basic authentication in a request header
    And the request address never contains the password, so an error that names the address cannot reveal it
