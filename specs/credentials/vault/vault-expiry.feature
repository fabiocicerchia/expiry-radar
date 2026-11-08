@domain:credentials @area:vault @from:source/vault
Feature: Tracking what expires in Vault
  Gives operators one ranked view of the Vault token the scanner holds and the certificates their Vault PKI mounts issued, read with a read-only policy, so a Vault expiry is never discovered by an outage.

  Background:
    Given the Vault source is enabled with an address and a token

  @id:credentials.vault.001 @rule:credentials.vault.read-only-policy-suffices @verified @source:internal/source/vault.go#VaultSource @test:internal/source/vault_test.go#TestVaultPolicyDocCoversEveryRequest
  Scenario: The published read-only Vault policy grants exactly what a scan needs
    Given the Vault token carries only the published read-only policy for the "pki" mount
    When the operator runs a scan
    Then the token and the "pki" certificates are listed with no permission warning
    And the policy grants only read and list, and every path it grants is used by the scan
