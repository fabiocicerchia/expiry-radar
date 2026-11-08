@domain:developer-tooling @area:live-verification @from:source/awsverify
Feature: Verifying the AWS adapters against a live account
  Gives an operator a pass/fail report on whether the ACM, IAM and Secrets Manager adapters really work against their account, in a form they can paste into an issue without leaking anything.

  Background:
    Given the aws source is enabled in the configuration
    And the operator has read-only credentials for an AWS account

  @id:developer-tooling.live-verification.003 @rule:tooling.aws-verify.report-redacts-identifiers @verified @source:internal/source/awsverify.go#AWSVerdict @test:internal/source/awsverify_test.go#TestNoIdentifierReachesTheReport
  Scenario: The verification report carries no account identifiers, only counts and day offsets
    Given the account holds an ACM certificate for payments.internal.example.com in account 123456789012 expiring in 9 days
    When the operator runs expiry-radar -verify-aws
    Then the report shows the acm service with 1 item
    And the report lists the expiries as "expiries, in days from now: [9]"
    And the report contains no certificate name, no account id and no ARN

  @id:developer-tooling.live-verification.004 @rule:tooling.aws-verify.exit-status @inferred @source:cmd/expiry-radar/main.go#runVerifyAWS
  Scenario Outline: Verification exits non-zero when any check failed, but not for inconclusive checks
    Given <state>
    When the operator runs expiry-radar -verify-aws
    Then the command exits with status <status>

    Examples:
      | state                                              | status                                                                     |
      | every check passed or is inconclusive              | 0                                                                          |
      | at least one check is marked FAIL                  | 1                                                                          |
      | the aws source is not enabled in the configuration | 2, with the error "-verify-aws needs the aws source enabled in the config" |
