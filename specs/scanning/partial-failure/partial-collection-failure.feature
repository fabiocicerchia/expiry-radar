@domain:scanning @area:partial-failure @from:source/source
Feature: Keeping findings when part of a scan fails
  Operators need a scan where one broken credential, denied permission or unreachable host still reports everything else it found, and says what it could not read, so a partial outage never looks like a clean estate.

  Background:
    Given the operator has configured several sources

  @id:scanning.partial-failure.004 @rule:scanning.partial-failure.source-failure-keeps-other-findings @verified @source:internal/source/source.go#CollectAll @test:internal/source/collect_test.go#TestCollectAllKeepsPartialResults
  Scenario: A failing source does not hide the other sources' findings
    Given source "partial" reads item "found-anyway" and then fails with "1 endpoint failed"
    And source "clean" reads item "also-found"
    When the operator runs a scan
    Then the inventory contains "found-anyway" and "also-found"
    And exactly one warning is reported
    And the warning reads "partial: 1 endpoint failed"

  @id:scanning.partial-failure.005 @rule:scanning.partial-failure.denied-service-keeps-other-services @verified @source:internal/source/source.go#collectUnits @test:internal/source/awsverify_test.go#TestOneDeniedServiceKeepsTheOthersFindings @test:internal/source/k8s_test.go#TestADeniedNamespaceKeepsTheOtherNamespacesSecrets
  Scenario: One denied service or resource inside a source does not lose the others' findings
    Given the AWS source reads ACM, IAM and Secrets Manager
    And ACM is denied with "AccessDeniedException"
    And IAM has access key "k" and Secrets Manager has secret "s"
    When the operator runs a scan
    Then the inventory contains "k" and "s"
    And a warning names the "acm" service and its access-denied reason
    And no warning names "iam" or "secretsmanager"

  @id:scanning.partial-failure.006 @rule:scanning.partial-failure.partial-reads-are-kept @verified @source:internal/source/source.go#collectUnits @test:internal/source/awsverify_test.go#TestPartialItemsFromAFailingServiceAreStillKept @test:internal/source/k8s_test.go#TestPartialItemsFromAFailingResourceAreStillKept
  Scenario: What a failing service read before it failed is still reported
    Given ACM returns certificate "page-1" and then fails with "throttled on page 2"
    When the operator runs a scan
    Then the inventory contains "page-1"
    And a warning names "acm" with "throttled on page 2"

  @id:scanning.partial-failure.007 @rule:scanning.partial-failure.skipped-service-is-not-a-failure @verified @source:internal/source/source.go#collectUnits @test:internal/source/k8s_test.go#TestASkippedResourceIsNotAFailure @test:internal/source/awsverify_test.go#TestASkippedServiceIsNotAFailure
  Scenario: A service the operator did not enable is skipped without a warning
    Given the operator has not enabled webhook CA collection for the cluster
    And the cluster has TLS secret "prod/shop-tls"
    When the operator runs a scan
    Then the inventory contains "prod/shop-tls"
    And no warning is reported
    And the skipped service is never queried
