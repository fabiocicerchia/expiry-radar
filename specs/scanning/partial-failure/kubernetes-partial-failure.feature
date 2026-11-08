@domain:scanning @area:partial-failure @from:source/k8s
Feature: Reporting what a Kubernetes scan could not read
  Lets operators trust a cluster scan: a missing permission or a misconfigured server shows up as a named error and never looks like a clean estate, and it doesn't take the readable findings with it.

  Background:
    Given the k8s source is enabled

  @id:scanning.partial-failure.001 @rule:k8s.partial-failure.denied-read-keeps-other-findings @verified @source:internal/source/k8s.go#listPaths @test:internal/source/k8s_test.go#TestOneDeniedResourceKeepsTheOthersFindings @test:internal/source/k8s_test.go#TestADeniedNamespaceKeepsTheOtherNamespacesSecrets
  Scenario Outline: A denied resource or namespace does not lose the findings that could be read
    Given namespace "prod" holds TLS secret "shop-tls"
    And the cluster denies reading <denied>
    When the operator runs a scan with <scope>
    Then the inventory still lists "prod/shop-tls"
    And the k8s source reports an error naming <named>

    Examples:
      | denied                            | scope                           | named                      |
      | validating webhook configurations | trust anchors enabled           | webhooks                   |
      | secrets in namespace "staging"    | namespaces "prod" and "staging" | namespaces/staging/secrets |

  @id:scanning.partial-failure.002 @rule:k8s.partial-failure.not-found-is-not-a-clean-estate @verified @source:internal/source/k8s.go#listEach @test:internal/source/k8s_test.go#TestA404OnTheSecretsListIsNotACleanEstate
  Scenario: A cluster API that answers "not found" for secrets fails the scan instead of reporting nothing
    Given the configured server answers every request with "not found", such as a server URL with an extra path prefix
    When the operator runs a scan
    Then the inventory has no Kubernetes entries
    And the k8s source reports an error naming secrets

  @id:scanning.partial-failure.003 @rule:k8s.partial-failure.uninstalled-cert-manager-is-not-an-error @verified @source:internal/source/k8s.go#listEachIfPresent @test:internal/source/k8s_test.go#TestAMissingCertManagerCRDIsStillNotAnError
  Scenario: A cluster without cert-manager installed is not reported as a failure
    Given cert-manager reads are enabled
    And cert-manager is not installed in the cluster
    And namespace "prod" holds TLS secret "shop-tls"
    When the operator runs a scan
    Then the inventory lists "prod/shop-tls"
    And the k8s source reports no error
