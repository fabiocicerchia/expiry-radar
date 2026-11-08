@domain:scanning @area:run-command @from:cmd/expiry-radar
Feature: Running a scan from the command line
  Lets an operator or a CI job run one scan over the chosen sources and read from the report and the exit status whether anything is about to expire or whether the scan is incomplete.

  Background:
    Given the binary is installed as expiry-radar

  @id:scanning.run-command.001 @rule:scanning.run-command.fail-within-gate @inferred @source:cmd/expiry-radar/main.go#breached
  Scenario Outline: A scan fails when anything expires inside the -fail-within window
    Given the inventory holds a certificate for api.example.com that expires in <days_left> days
    And every source answers
    When the operator runs a scan with -fail-within <fail_within>
    Then the ranked report is printed
    And the exit status is <exit>
    And stderr shows <message>

    Examples:
      | days_left | fail_within | exit | message                                                              |
      | 5         | 14          | 1    | "expiry-radar: api.example.com expires in 5 days (-fail-within 14)"  |
      | 14        | 14          | 1    | "expiry-radar: api.example.com expires in 14 days (-fail-within 14)" |
      | 40        | 14          | 0    | nothing                                                              |
      | 5         | 0           | 0    | nothing                                                              |

  @id:scanning.run-command.002 @rule:scanning.run-command.partial-failure-exit-status @inferred @source:cmd/expiry-radar/main.go#run @questionable
  Scenario: A scan with a failed source still reports and exits 3
    Given the aws source is configured and its credentials are denied
    And the domain:rdap source finds example.com expiring in 60 days
    When the operator runs a scan
    Then the report lists example.com
    And stderr shows a line starting "expiry-radar: warning:" that describes the aws failure
    And the exit status is 3

  @id:scanning.run-command.003 @rule:scanning.run-command.only-narrows-sources @verified @source:cmd/expiry-radar/main.go#pick @test:cmd/expiry-radar/main_test.go#TestOnlyRunsJustTheNamedSources @test:cmd/expiry-radar/main_test.go#TestOnlyKeepsTheConfiguredOrderNotTheFlagOrder @test:cmd/expiry-radar/main_test.go#TestNoOnlyRunsEveryConfiguredSource
  Scenario Outline: -only runs just the named sources, in configured order
    Given the config builds the sources tls:endpoint, domain:rdap, aws and k8s, in that order
    When the operator runs a scan with -only <only>
    Then the scan runs the sources <runs>

    Examples:
      | only                   | runs                                |
      | "aws,domain:rdap"      | domain:rdap, aws                    |
      | "k8s,tls:endpoint,aws" | tls:endpoint, aws, k8s              |
      | (not set)              | tls:endpoint, domain:rdap, aws, k8s |

  @id:scanning.run-command.004 @rule:scanning.run-command.only-unknown-source-refused @verified @source:cmd/expiry-radar/main.go#pick @test:cmd/expiry-radar/main_test.go#TestOnlyNamingAnUnconfiguredSourceIsAnErrorNotAnEmptyRun @test:cmd/expiry-radar/main_test.go#TestOnlyReportsEveryNameThatDidNotMatch
  Scenario Outline: Naming a source the config did not build is refused, not run empty
    Given the config builds the sources tls:endpoint, domain:rdap, aws and k8s
    When the operator runs a scan with -only <only>
    Then no source is scanned
    And stderr shows "expiry-radar: unknown source <unknown> in -only; this config built: aws, domain:rdap, k8s, tls:endpoint"
    And the exit status is 2

    Examples:
      | only             | unknown         |
      | "cloudflare"     | "cloudflare"    |
      | "aws,vault,okta" | "okta", "vault" |

  @id:scanning.run-command.005 @rule:scanning.run-command.no-sources-refused @verified @source:cmd/expiry-radar/main.go#run @test:cmd/expiry-radar/main_test.go#TestOnlyWithNothingConfiguredDefersToTheEmptyConfigMessage
  Scenario Outline: A scan with nothing to scan is refused
    Given there is no config file at expiry-radar.json
    When the operator runs a scan with <flags>
    Then stderr shows "expiry-radar: no sources configured — pass -endpoints/-domains, or add manual items / enable k8s/vault/aws in expiry-radar.json"
    And the exit status is 2

    Examples:
      | flags     |
      | no flags  |
      | -only aws |

  @id:scanning.run-command.006 @rule:scanning.run-command.flags-add-to-config @inferred @source:cmd/expiry-radar/main.go#loadConfig
  Scenario Outline: Endpoints and domains given as flags add to the config and need no config file
    Given <config_state>
    When the operator runs a scan with -endpoints "shop.example.com, api.example.com" -domains example.com
    Then the report includes the TLS certificates served by shop.example.com and api.example.com
    And the report includes the registration of example.com
    And <also>

    Examples:
      | config_state                                         | also                                                    |
      | there is no config file at expiry-radar.json         | nothing else is scanned                                 |
      | expiry-radar.json lists the endpoint pay.example.com | the TLS certificate of pay.example.com is also reported |

  @id:scanning.run-command.007 @rule:scanning.run-command.report-filters @inferred @source:cmd/expiry-radar/main.go#filter @questionable
  Scenario Outline: -within and -min-priority narrow both the report and the gate
    Given the inventory holds <item>, expiring in <days_left> days with priority <priority>
    When the operator runs a scan with <flags>
    Then <item> is <shown> in the report

    Examples:
      | item                                    | days_left | priority | flags                      | shown                               |
      | the certificate for pay.example.com     | 10        | 0.9      | -within 30                 | shown                               |
      | the certificate for staging.example.com | 90        | 0.9      | -within 30                 | not shown                           |
      | the domain example.org                  | -2        | 0.95     | -within 30                 | shown                               |
      | the certificate for dash.example.com    | 10        | 0.2      | -min-priority 0.5          | not shown                           |
      | the certificate for dash.example.com    | 10        | 0.5      | -min-priority 0.5          | shown                               |
      | the certificate for staging.example.com | 20        | 0.2      | -within 10 -fail-within 30 | not shown, and the exit status is 0 |

  @id:scanning.run-command.008 @rule:scanning.run-command.unwritable-report-refused @inferred @source:cmd/expiry-radar/main.go#writeReport @questionable
  Scenario Outline: A report that cannot be written fails the scan with exit 2
    Given the inventory holds a certificate for api.example.com
    When the operator runs a scan with <flags>
    Then stderr shows <message>
    And the exit status is 2

    Examples:
      | flags                             | message                                                  |
      | -out /nonexistent-dir/report.html | an error that the file could not be created              |
      | -format pdf                       | "expiry-radar: unknown format \"pdf\" (want one of ...)" |
