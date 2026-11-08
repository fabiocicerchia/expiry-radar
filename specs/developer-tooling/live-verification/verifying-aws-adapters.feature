@domain:developer-tooling @area:live-verification @from:cmd/expiry-radar
Feature: Verifying the AWS adapters against a real account
  Gives an operator a pass/fail report on the AWS adapters, run against a real account and safe to paste into an issue, so the field mappings can be confirmed beside the console.

  Background:
    Given the binary is installed as expiry-radar

  @id:developer-tooling.live-verification.001 @rule:verification.aws.requires-aws-source @inferred @source:cmd/expiry-radar/main.go#runVerifyAWS
  Scenario: AWS verification is refused when the aws source is not enabled
    Given the config does not enable the aws source
    When the operator runs expiry-radar -verify-aws
    Then stderr shows "expiry-radar: -verify-aws needs the aws source enabled in the config"
    And the exit status is 2

  @id:developer-tooling.live-verification.002 @rule:verification.aws.exit-status-reflects-checks @inferred @source:cmd/expiry-radar/main.go#runVerifyAWS
  Scenario Outline: AWS verification exits non-zero when a check fails
    Given the config enables the aws source
    And the AWS checks <outcome>
    When the operator runs expiry-radar -verify-aws
    Then the verification report is printed to stdout
    And the exit status is <exit>

    Examples:
      | outcome                      | exit |
      | all pass                     | 0    |
      | include at least one failure | 1    |
