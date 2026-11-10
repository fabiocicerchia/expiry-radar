@domain:credentials @area:rotation-policy @from:source/source
Feature: Marking rotation-policy deadlines
  Operators tracking keys that never expire need the deadline derived from creation date plus their rotation policy, and need every such row to say it is a policy date, not one the issuer stated.

  @id:credentials.rotation-policy.009 @rule:credentials.rotation-policy.deadline-is-created-plus-policy-and-labelled @verified @source:internal/source/source.go#policyDeadline @test:internal/source/manual_test.go#TestManualSourceDatesAnUnexpiringCredentialFromThePolicy @test:internal/source/rotation_test.go#TestARotationDeadlineSaysItIsAPolicyNotAnIssuerDate
  Scenario Outline: A credential with no expiry is due its creation date plus the rotation policy, and says so
    Given a credential with no issuer expiry was created on "<created>"
    And the operator's rotation policy is <days> days
    When the operator runs a scan
    Then the credential's deadline is "<deadline>"
    And its labels show created "<created_label>", policy.days "<days>" and deadline "rotation policy"

    Examples:
      | created    | days | deadline             | created_label        |
      | 2026-01-01 | 90   | 2026-04-01T00:00:00Z | 2026-01-01T00:00:00Z |
      | 2026-03-15 | 30   | 2026-04-14T00:00:00Z | 2026-03-15T00:00:00Z |
