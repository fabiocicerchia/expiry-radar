@domain:editor-integrations @area:health-check @from:extensions/nvim
Feature: Diagnosing the Neovim setup
  Tells an operator why the editor shows nothing or too little: no binary, no usable config, or a source with no way in.

  Background:
    Given Given the operator uses the expiry-radar plugin in Neovim

  @id:editor-integrations.health-check.001 @rule:editor.health.binary-is-expiry-radar @verified @source:extensions/nvim/lua/expiry-radar/health.lua#check_binary @test:extensions/nvim/tests/health_spec.lua#does not vouch for something on disk that is not expiry-radar
  Scenario Outline: The health check verifies the binary is really expiry-radar
    Given Given <binary>
    When When the operator runs the health check
    Then Then it reports <report>

    Examples:
      | binary                                                                 | report                                                                                                     |
      | no expiry-radar binary can be found                                    | an error "the expiry-radar binary was not found" with the go install, make build and cmd options as advice |
      | the configured command prints usage that does not mention expiry-radar | a warning that the command ran but does not look like expiry-radar                                         |
      | the configured command prints expiry-radar usage                       | that the command runs                                                                                      |

  @id:editor-integrations.health-check.002 @rule:editor.health.config-must-enable-a-source @verified @source:extensions/nvim/lua/expiry-radar/health.lua#M.check_config @test:extensions/nvim/tests/health_spec.lua#says a config it cannot parse is unusable, rather than saying nothing @test:extensions/nvim/tests/health_spec.lua#calls a config that enables nothing an error, not an empty inventory @test:extensions/nvim/tests/health_spec.lua#explains an empty list when there is no config and nothing configured @test:extensions/nvim/tests/health_spec.lua#reports both free sources when the config declares them
  Scenario Outline: The health check says when the config cannot produce an inventory
    Given Given <config>
    When When the operator runs the health check
    Then Then it reports <report>

    Examples:
      | config                                                                     | report                                                                                                                                            |
      | expiry-radar.json contains "{ not json"                                    | an error "the config file could not be used: it is not valid JSON"                                                                                |
      | expiry-radar.json has empty endpoints and domains and enables nothing else | an error "the config file enables no sources at all"                                                                                              |
      | there is no config file and no endpoints or domains in setup               | a warning "no config file at expiry-radar.json, and no endpoints or domains configured" with advice to copy the example or run :ExpiryRadarConfig |
      | there is no config file but setup lists example.com                        | the note "no config file at expiry-radar.json — running on setup() options alone"                                                                 |
      | expiry-radar.json lists 2 endpoints and 1 domain                           | "2 endpoint(s) to probe over TLS" and "1 domain(s) to check via RDAP"                                                                             |

  @id:editor-integrations.health-check.003 @rule:editor.health.enabled-source-needs-credentials @verified @source:extensions/nvim/lua/expiry-radar/health.lua#check_vault @test:extensions/nvim/tests/health_spec.lua#warns per source when the environment has nothing to reach it with
  Scenario Outline: The health check warns about an enabled source the environment cannot reach
    Given Given the config enables <source>
    And And <environment>
    When When the operator runs the health check
    Then Then it warns "<warning>"

    Examples:
      | source                             | environment                            | warning                                                                  |
      | kubernetes with no server          | Neovim is not running in a cluster pod | kubernetes is enabled with no server, and this is not a cluster pod      |
      | vault with no addr                 | VAULT_ADDR is not set                  | vault is enabled with no addr and no $VAULT_ADDR                         |
      | vault at https://vault.example.com | VAULT_TOKEN is not set                 | vault is enabled (https://vault.example.com) but $VAULT_TOKEN is not set |
      | aws in eu-west-1 with no profile   | no AWS credential variables are set    | aws is enabled but there are no AWS credentials in the environment       |

  @id:editor-integrations.health-check.004 @rule:editor.health.names-limits-hiding-rows @verified @source:extensions/nvim/lua/expiry-radar/health.lua#check_limits @test:extensions/nvim/tests/health_spec.lua#names the collection options that are silently hiding rows
  Scenario: The health check names the options that leave items out
    Given Given the plugin collects only items within 7 days and with priority at least 0.5
    When When the operator runs the health check
    Then Then it notes "collect.within_days is 7 — anything further out is not collected"
    And And it notes "collect.min_priority is 0.5 — lower-ranked items are not collected"

  @id:editor-integrations.health-check.005 @rule:editor.health.reports-lost-sources @inferred @source:extensions/nvim/lua/expiry-radar/health.lua#M.check
  Scenario: The health check repeats which sources the last collection lost
    Given Given the last collection lost the aws source with "aws: AccessDenied"
    When When the operator runs the health check
    Then Then it warns "the last collection lost 1 source(s)" with "aws: AccessDenied" as detail
