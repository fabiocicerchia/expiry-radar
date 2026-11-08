@domain:configuration @area:source-setup @from:config/sources
Feature: Choosing which sources a scan runs
  Lets operators decide exactly which systems expiry-radar contacts and with which credentials, so nothing is queried unless the config file asks for it.

  Background:
    Given the operator runs expiry-radar with a config file

  @id:configuration.source-setup.001 @rule:configuration.sources.credentialed-sources-are-opt-in @verified @source:internal/config/sources.go#Sources @test:internal/config/config_test.go#TestNothingIsEnabledImplicitly @test:internal/config/config_test.go#TestSourcesBuildsOneSourcePerEnabledBlock
  Scenario Outline: A source that needs credentials is only scanned when its section is enabled
    Given the config file has a "<section>" section with enabled set to <enabled>
    When the operator runs a scan
    Then the "<source>" source is <outcome> in the scan

    Examples:
      | section | enabled | source | outcome      |
      | vault   | true    | vault  | included     |
      | vault   | false   | vault  | not included |
      | aws     | true    | aws    | included     |
      | aws     | false   | aws    | not included |
      | k8s     | true    | k8s    | included     |
      | k8s     | false   | k8s    | not included |

  @id:configuration.source-setup.002 @rule:configuration.sources.listed-targets-are-scanned @verified @source:internal/config/sources.go#Sources @test:internal/config/config_test.go#TestSourcesBuildsOneSourcePerEnabledBlock @test:internal/config/config_test.go#TestEveryConfiguredProviderIsConstructed
  Scenario Outline: Listing endpoints, domains, manual entries, identity providers, registrars or rotation policies is enough to scan them
    Given the config file lists at least one valid entry under "<list>" and has no enabled flag for it
    And any credential that entry needs is set in the environment
    When the operator runs a scan
    Then the "<source>" source is included in the scan

    Examples:
      | list       | source       |
      | endpoints  | tls:endpoint |
      | domains    | domain:rdap  |
      | manual     | manual       |
      | federation | federation   |
      | registrars | registrar    |
      | rotation   | rotation     |

  @id:configuration.source-setup.003 @rule:configuration.sources.every-configured-provider-runs @verified @source:internal/config/sources.go#Sources @test:internal/config/config_test.go#TestEveryConfiguredProviderIsConstructed
  Scenario: Every provider the config enables takes part in the scan, none dropped silently
    Given the config file enables every supported provider, each with its credentials
    When the operator runs a scan
    Then the scan includes exactly one source for each of tls:endpoint, domain:rdap, manual, k8s, vault, aws, cloudflare, gitlab, github, gcp, digitalocean, scaleway, namecheap, rotation, azure, okta, federation, fastly, hetzner, harbor, jfrog, registrar and apple
    And the scan includes 23 sources in total

  @id:configuration.source-setup.004 @rule:configuration.sources.empty-config-scans-nothing @verified @source:internal/config/sources.go#Sources @test:internal/config/config_test.go#TestNothingIsEnabledImplicitly @test:cmd/expiry-radar/e2e_test.go#TestE2ENoSourcesIsAUsageError
  Scenario: A config that enables nothing is refused as a usage error, not reported as an empty inventory
    Given the config file enables no source and no endpoints or domains are passed on the command line
    When the operator runs a scan
    Then the run exits with the usage-error status
    And the operator sees "no sources configured"
