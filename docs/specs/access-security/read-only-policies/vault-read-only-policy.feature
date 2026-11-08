@domain:access-security @area:read-only-policies @from:docs/vault-readonly-policy
Feature: Granting expiry-radar read-only access to Vault
  Gives Vault operators a least-privilege policy to attach to the scanning token, so expiry-radar can inventory Vault expiries without ever holding write rights.

  Background:
    Given the operator has attached the published read-only Vault policy to the token expiry-radar uses
    And the Vault source is pointed at the PKI mount "pki"

  @id:docs.access-security.read-only-policies.001 @rule:vault.policy.read-list-only @verified @source:docs/vault-readonly-policy.hcl#path "auth/token/lookup-self" @test:internal/source/vault_test.go#TestVaultPolicyDocCoversEveryRequest
  Scenario: The Vault policy grants only read and list rights
    Given the published read-only Vault policy
    When the Vault operator reviews the rights the policy grants
    Then "auth/token/lookup-self" is granted read only
    And "pki/certs" is granted list only
    And "pki/cert/*" is granted read only
    And no path is granted create, update, delete or sudo

  @id:docs.access-security.read-only-policies.002 @rule:vault.policy.covers-every-scan-request @verified @source:docs/vault-readonly-policy.hcl#path "pki/cert/*" @test:internal/source/vault_test.go#TestVaultPolicyDocCoversEveryRequest
  Scenario: A token with only the read-only policy can scan its own expiry and PKI certificates
    Given the token "token-radar" expires in 20 days
    And the "pki" mount holds the certificate for "api.example.com"
    When the operator runs a scan with the Vault source
    Then the inventory lists the Vault token "token-radar"
    And the inventory lists the certificate "api.example.com" from "vault:pki"
    And no request the scan makes is refused for lack of permission

  @id:docs.access-security.read-only-policies.003 @rule:vault.policy.no-lease-enumeration @verified @source:internal/source/vault.go#VaultSource @test:internal/source/vault_test.go#TestVaultPolicyDocCoversEveryRequest
  Scenario: Vault dynamic leases are not enumerated, so no update or sudo right is needed
    Given Vault holds dynamic secret leases
    When the operator runs a scan with the Vault source
    Then the inventory does not list the dynamic leases
    And the policy does not grant "sys/leases/lookup"

  @id:docs.access-security.read-only-policies.004 @rule:vault.policy.grants-per-pki-mount @inferred @source:docs/vault-readonly-policy.hcl#path "pki/certs" @questionable
  Scenario: Each extra PKI mount needs its own grants, otherwise that mount is reported as permission denied
    Given the Vault source is also pointed at the PKI mount "pki_int"
    And the policy has no blocks for "pki_int"
    When the operator runs a scan with the Vault source
    Then the inventory still lists the token and the "pki" certificates
    And the scan warns "pki_int: LIST /v1/pki_int/certs: permission denied (a read-only policy needs read+list on this path)"
