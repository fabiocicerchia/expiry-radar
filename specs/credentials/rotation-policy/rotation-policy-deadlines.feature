@domain:credentials @area:rotation-policy @from:source/rotation
Feature: Giving never-expiring API keys a rotation deadline
  Lets an operator see Anthropic, OpenAI and Docker Hub keys that never expire in the ranked inventory, due on a rotation deadline the operator chose and clearly marked as policy rather than a date the provider stated.

  Background:
    Given the operator has configured a rotation provider with an admin credential and a maximum key age in days

  @id:credentials.rotation-policy.001 @rule:credentials.rotation-policy.deadline-is-creation-plus-max-age @verified @source:internal/source/rotation.go#rotationItem @test:internal/source/rotation_test.go#TestARotationDeadlineSaysItIsAPolicyNotAnIssuerDate
  Scenario: A key with no stated expiry is due its maximum age after it was created, marked as a rotation policy date
    Given the provider's maximum key age is 90 days
    And the Anthropic organisation has an active key "prod-key" created 60 days ago with no expiry
    When the operator runs a scan
    Then "prod-key" is listed as an IAM access key from source "rotation:anthropic"
    And its expiry is about 30 days from now
    And its deadline label reads "rotation policy"
    And its labels show the key's creation date and "policy.days" 90

  @id:credentials.rotation-policy.002 @rule:credentials.rotation-policy.issuer-expiry-wins-over-policy @verified @source:internal/source/rotation.go#dockerHubKeys @test:internal/source/rotation_test.go#TestAnIssuerStatedExpiryIsNotLabelledAPolicy
  Scenario: A token that states its own expiry is due on the provider's date, not the policy
    Given the provider's maximum key age is 30 days
    And the Docker Hub account has a token "bounded" created 10 days ago that expires in 40 days
    And it also has a token "forever" created 10 days ago with no expiry
    When the operator runs a scan
    Then "bounded" is due in 40 days and its deadline label reads "issuer"
    And "forever" is due in 20 days and its deadline label reads "rotation policy"

  @id:credentials.rotation-policy.003 @rule:credentials.rotation-policy.dead-keys-marked-not-in-use @verified @source:internal/source/rotation.go#rotationItem @test:internal/source/rotation_test.go#TestAnInactiveKeyIsNotADeadline @test:internal/source/rotation_test.go#TestOpenAIUnixTimestampsAreRead
  Scenario Outline: A key that is inactive or has never been used is marked as not in use
    Given the <provider> organisation has a key "<key>" that <state>
    When the operator runs a scan
    Then "<key>" is still listed with its deadline
    And its "in-use" label reads "false", so it ranks below keys in use

    Examples:
      | provider   | key          | state                                         |
      | Anthropic  | retired      | has status inactive                           |
      | Docker Hub | old-ci-token | is not active                                 |
      | OpenAI     | never-used   | has never been used (last-used reads "never") |

  @id:credentials.rotation-policy.004 @rule:credentials.rotation-policy.provider-config-rejected-at-load @verified @source:internal/source/rotation.go#ValidateRotation @test:internal/source/rotation_test.go#TestARotationProviderWithNoPolicyIsRejected
  Scenario Outline: A rotation provider that could not produce a usable deadline is refused when the config loads
    Given the config file has a rotation entry with <entry>
    When the operator runs a scan
    Then the config is refused before any provider is contacted
    And the error reads "<message>"

    Examples:
      | entry                                                       | message                                                                                                                                                 |
      | no name                                                     | rotation[0]: name is required (one of anthropic, dockerhub, openai)                                                                                     |
      | name "notaprovider"                                         | rotation[0]: unknown provider "notaprovider", want one of anthropic, dockerhub, openai                                                                  |
      | name "anthropic" and no maxKeyAgeDays                       | rotation[0] (anthropic): maxKeyAgeDays is required and must be positive — these keys never expire, so the rotation policy is the only deadline there is |
      | name "openai", maxKeyAgeDays 30, and OPENAI_ADMIN_KEY unset | rotation provider "openai" is configured but $OPENAI_ADMIN_KEY is not set                                                                               |

  @id:credentials.rotation-policy.005 @rule:credentials.rotation-policy.denied-provider-asks-for-admin-credential @verified @source:internal/source/rotation.go#rotationGet @test:internal/source/rotation_test.go#TestOneUnreadableRotationProviderKeepsTheOthers
  Scenario: A provider that refuses the credential is reported as needing an admin key, and the other providers' keys are kept
    Given Anthropic is configured with a credential that can list keys
    And OpenAI is configured with an ordinary API key that the provider refuses
    When the operator runs a scan
    Then Anthropic's keys appear in the inventory
    And the scan reports for openai that listing keys needs an admin credential, not an ordinary API key

  @id:credentials.rotation-policy.006 @rule:credentials.rotation-policy.operator-labels-on-every-key @inferred @source:internal/source/rotation.go#rotationItem
  Scenario: Labels the operator sets on a rotation provider appear on every key from it
    Given the OpenAI provider is configured with labels environment "production" and blast radius "0.9"
    And the OpenAI organisation has keys "ci-admin" and "billing-admin"
    When the operator runs a scan
    Then both keys carry environment "production" and blast radius "0.9"
    And where the provider reports a label with the same name, the provider's value replaces the operator's

  @id:credentials.rotation-policy.007 @rule:credentials.rotation-policy.keys-without-creation-date-omitted @inferred @source:internal/source/rotation.go#anthropicKeys @questionable
  Scenario: A key whose creation date cannot be read is left out of the inventory without a warning
    Given the Anthropic organisation has a key "legacy" whose creation date is missing or not RFC 3339
    When the operator runs a scan
    Then "legacy" does not appear in the inventory
    And the scan reports no warning about it

  @id:credentials.rotation-policy.008 @rule:credentials.rotation-policy.only-first-hundred-keys-read @inferred @source:internal/source/rotation.go#rotationGet @questionable
  Scenario: Only the first 100 keys of each provider are inventoried
    Given the OpenAI organisation has 150 admin keys
    When the operator runs a scan
    Then only 100 of its keys appear in the inventory
    And the scan reports no warning that keys were left out
