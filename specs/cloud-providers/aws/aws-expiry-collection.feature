@domain:cloud-providers @area:aws @from:source/aws
Feature: Collecting expiring items from AWS
  Gives operators one ranked list of the ACM certificates, IAM access keys and Secrets Manager rotations in an AWS account. A permission the read-only role is missing loses only the service that needs it.

  Background:
    Given the operator has enabled the aws source for account 123456789012

  @id:cloud-providers.aws.003 @rule:aws.iam.access-key-rotation-deadline @verified @source:internal/source/aws.go#accessKeyItems @test:internal/source/aws_test.go#TestAWSSourceTurnsAccessKeyAgeIntoARotationDeadline @test:internal/source/aws_test.go#TestAWSSourceDefaultsToANinetyDayRotationWindow
  Scenario Outline: An active IAM access key is due for rotation a fixed number of days after it was created
    Given user alice has an active access key AKIAALICE1 created on 2026-01-01
    And the aws source has maxKeyAgeDays <configured>
    When the operator runs a scan
    Then the inventory lists IAM key alice/AKIAALICE1 from aws:iam in account 123456789012
    And its deadline is <deadline>
    And it is labelled as a rotation-policy deadline of <days> days from its creation date

    Examples:
      | configured | deadline   | days |
      | 30         | 2026-01-31 | 30   |
      | unset      | 2026-04-01 | 90   |

  @id:cloud-providers.aws.004 @rule:aws.iam.inactive-keys-not-listed @verified @source:internal/source/aws.go#accessKeyItems @test:internal/source/aws_test.go#TestAWSSourceTurnsAccessKeyAgeIntoARotationDeadline
  Scenario: An inactive IAM access key is not listed
    Given user alice has an inactive access key AKIAALICE2
    When the operator runs a scan
    Then the inventory has no entry for alice/AKIAALICE2
