@domain:certificates @area:cluster-trust @from:source/k8strust
Feature: Watching the trust anchors a cluster validates against
  Gives cluster operators one list of the CA certificates behind admission webhooks, aggregated APIs and service meshes, so a lapsing CA, which takes the whole cluster down with it, is found before it expires.

  Background:
    Given the operator has enabled trust-anchor scanning for the cluster

  @id:certificates.cluster-trust.001 @rule:certificates.cluster-trust.webhook-and-apiservice-cas-reported @verified @source:internal/source/k8strust.go#webhookCAs @test:internal/source/k8s_test.go#TestALocalAPIServiceIsSkipped @test:internal/source/k8s_test.go#TestAWebhookWithNoCABundleIsSkipped @test:internal/source/k8s_test.go#TestTheSameWebhookCAInTwoConfigurationsIsReportedOnce
  Scenario: Every CA a webhook or aggregated API trusts is reported as a trust anchor
    Given the cluster has a validating webhook configuration "cert-manager-webhook" whose CA expires in 10 days
    And the cluster has an APIService "v1beta1.metrics.k8s.io" backed by service kube-system/metrics-server, with its own CA
    And the cluster has the built-in APIService "v1." served by the API server itself
    And the cluster has a webhook whose CA is injected, so it carries no CA bundle
    When the operator runs a scan
    Then the inventory lists trust anchor "validatingwebhookconfiguration/cert-manager-webhook" from source "k8s:webhook" expiring in 10 days
    And the inventory lists trust anchor "apiservice/v1beta1.metrics.k8s.io" from source "k8s:apiservice" with service "kube-system/metrics-server"
    And nothing is listed for "v1." or for the injected webhook

  @id:certificates.cluster-trust.002 @rule:certificates.cluster-trust.shared-ca-reported-once @verified @source:internal/source/k8strust.go#caAccumulator @test:internal/source/k8s_test.go#TestTheSameWebhookCAInTwoConfigurationsIsReportedOnce @test:internal/source/k8s_test.go#TestACASharedByAWebhookAndAnAPIServiceIsReportedOnce @test:internal/source/k8s_test.go#TestTwoCAsWithTheSameIssuerCNAndSerialAreBothReported @test:internal/source/k8s_test.go#TestACAKnownToTwoCollectorsKeepsWhatBothKnew
  Scenario: A CA shared by several cluster objects is one finding naming every object that relies on it
    Given the same CA is pinned into webhook configurations "cert-manager-validating" and "cert-manager-mutating" and into APIService "v1beta1.metrics.k8s.io"
    And a different CA with the same subject "kubernetes" and serial 1 is pinned into webhook configuration "other"
    When the operator runs a scan
    Then the shared CA appears once, and its used-by lists all three objects
    And the CA pinned into "other" appears as its own finding
    And when the shared CA is also an Istio mesh root, its single finding carries namespace "istio-system" and the mesh, role and key labels

  @id:certificates.cluster-trust.003 @rule:configuration.validation.mesh-anchor-must-be-matchable @verified @source:internal/source/k8strust.go#ValidateMeshAnchors @test:internal/config/config_test.go#TestLoadRejectsConfigsThatWouldSilentlyScanLess
  Scenario Outline: A mesh anchor that could never match is rejected when the config loads
    Given the config declares a mesh anchor <defect>
    When the operator runs a scan with that config
    Then the config is refused with an error containing "<message>"
    And no scan runs

    Examples:
      | defect                    | message                         |
      | with kind "configmap"     | want "secrets" or "configmaps"  |
      | with no keys              | at least one key is required    |
      | with a key in role "root" | want "trust-anchor" or "issuer" |
      | with no namespace         | namespace is required           |
