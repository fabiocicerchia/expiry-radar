@domain:certificates @area:kubernetes-certificates @from:source/k8s
Feature: Inventorying TLS certificates held in a Kubernetes cluster
  Gives cluster operators each TLS secret's real expiry, plus the ingress context that shows which certificates face users, so the cert on a public ingress outranks an internal one.

  Background:
    Given the k8s source is enabled and points at a reachable cluster API

  @id:certificates.kubernetes-certificates.001 @rule:k8s.tls-secrets.corrupt-leaf-not-dated-by-chain @verified @source:internal/source/k8s.go#firstCert @test:internal/source/k8s_test.go#TestACorruptLeafIsSkippedRatherThanReportedWithTheChainsExpiry @questionable
  Scenario: A secret whose leaf certificate is unreadable is left out rather than dated by its chain
    Given TLS secret "prod/shop-tls" holds a corrupt leaf certificate followed by a valid intermediate expiring in 300 days
    When the operator runs a scan
    Then the inventory has no entry for "prod/shop-tls"
    And the scan reports no error for it
