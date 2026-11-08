@domain:cloud-providers @area:azure @from:source/azure
Feature: Finding what expires in Entra ID and Key Vault
  Lets operators see Azure app-registration secrets, SAML signing certificates and Key Vault objects before they expire and break an integration or every sign-in through an app.

  Background:
    Given the Azure source is enabled for tenant "tnt_acme" with a client id and $AZURE_CLIENT_SECRET set

  @id:cloud-providers.azure.001 @rule:cloud-providers.azure.app-registration-credentials-reported @verified @source:internal/source/azure.go#azureCredentialItems @test:internal/source/identity_test.go#TestAzureReportsAppRegistrationSecretsAndCertificates
  Scenario: App registration client secrets and certificates are each reported with their own expiry
    Given app registration "payments-api" has a client secret "ci-secret" ending 2026-11-28
    And and a certificate credential "signing-cert" with usage "Sign" ending 2026-11-28
    When the operator runs a scan
    Then "payments-api/ci-secret" is listed as a secret from source "azure:app" labelled credential "client secret"
    And "payments-api/signing-cert" is listed as a TLS certificate labelled credential "certificate"
    And both expire on 2026-11-28

  @id:cloud-providers.azure.002 @rule:cloud-providers.azure.saml-signing-cert-is-trust-anchor @verified @source:internal/source/azure.go#azureCredentialItems @test:internal/source/identity_test.go#TestAzureSAMLSigningCertificatesAreTrustAnchors
  Scenario: A service principal's SAML signing certificate ranks as a trust anchor
    Given service principal "sso-app" has a certificate "saml-signing" with usage "Verify" ending 2026-11-23
    When the operator runs a scan
    Then "sso-app/saml-signing" is listed as a trust anchor from source "azure:serviceprincipal"
    And it is not listed as an ordinary TLS certificate

  @id:cloud-providers.azure.003 @rule:cloud-providers.azure.client-secret-hint-never-reported @verified @source:internal/source/azure.go#azureCredentialItems @test:internal/source/identity_test.go#TestAzureDoesNotPutASecretPrefixInTheReport
  Scenario: No prefix of a client secret appears in the report
    Given app registration "app" has a client secret whose hint is "Xy9"
    When the operator runs a scan
    Then the client secret is listed
    And none of its labels contains "Xy9"

  @id:cloud-providers.azure.004 @rule:cloud-providers.azure.off-host-continuation-refused @verified @source:internal/source/azure.go#sameHost @test:internal/source/identity_test.go#TestAzureWillNotFollowAContinuationURLToAnotherHost @questionable
  Scenario: A continuation link to another host is not followed, and the scan says so
    Given the first page of app registrations lists "app/s"
    And and that page's continuation link points at https://attacker.example/v1.0/applications
    When the operator runs a scan
    Then no request carrying the Azure token is sent to attacker.example
    And the scan reports "the API returned a continuation URL on another host"
    And "app/s" from the first page is still listed
