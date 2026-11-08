@domain:setup-and-troubleshooting @area:environment-check @from:src/doctor
Feature: Diagnosing an empty or short inventory
  Serves the operator who got an empty or suspiciously small panel by explaining, in the log and a one-line summary, what stops a scan running or limits its results.

  Background:
    Given a workspace folder is open

  @id:vscode.setup-and-troubleshooting.environment-check.001 @rule:setup.doctor.summary-reflects-worst-finding @verified @source:extensions/vscode/src/doctor.ts#runDoctor @test:extensions/vscode/src/test/doctor.test.ts#a binary that runs and a config with both free sources reads clean @test:extensions/vscode/src/test/doctor.test.ts#something on disk that is not expiry-radar is named, not trusted @test:extensions/vscode/src/test/doctor.test.ts#a config that enables nothing at all is an error, and the summary says so
  Scenario Outline: The doctor's summary reflects its most serious finding
    Given the doctor's checks produce findings whose worst level is <worst>
    When the operator runs the doctor
    Then the activity log opens showing the report headed "expiry-radar doctor — <folder name>"
    And the notification reads "<summary>"

    Examples:
      | worst                          | summary                                                                         |
      | error                          | expiry-radar: the doctor found something that stops it running — see the log.   |
      | warning                        | expiry-radar: the doctor found something that will limit results — see the log. |
      | only ok or informational notes | expiry-radar: everything the doctor checks looks fine.                          |

  @id:vscode.setup-and-troubleshooting.environment-check.002 @rule:setup.doctor.missing-binary-is-error @inferred @source:extensions/vscode/src/doctor.ts#locateBinary
  Scenario: A missing expiry-radar binary is an error that says how to install it
    Given no expiry-radar binary can be found for the folder
    When the operator runs the doctor
    Then the report says "✗ the expiry-radar binary was not found"
    And the report shows the install command "go install github.com/fabiocicerchia/expiry-radar/cmd/expiry-radar@latest" and 'or set "expiryRadar.path"'
    And the report does not check the config file
    And the summary says the doctor found something that stops it running

  @id:vscode.setup-and-troubleshooting.environment-check.003 @rule:setup.doctor.foreign-binary-is-warned @verified @source:extensions/vscode/src/doctor.ts#checkBinaryIsRadar @test:extensions/vscode/src/test/doctor.test.ts#something on disk that is not expiry-radar is named, not trusted
  Scenario: A binary that does not look like expiry-radar is named, not trusted
    Given the binary the extension found runs but its usage text does not mention expiry-radar
    When the operator runs the doctor
    Then the report says "! <binary path> ran, but does not look like expiry-radar"
    And the report quotes the first line the binary printed
    And the summary says the doctor found something that will limit results

  @id:vscode.setup-and-troubleshooting.environment-check.004 @rule:setup.doctor.missing-config-explained @verified @source:extensions/vscode/src/doctor.ts#checkConfig @test:extensions/vscode/src/test/doctor.test.ts#no config and nothing in the settings is the warning that explains an empty panel
  Scenario Outline: A missing config file is a warning only when settings declare nothing either
    Given there is no config file at expiry-radar.json
    And the settings declare <declared>
    When the operator runs the doctor
    Then the report says "<line>"

    Examples:
      | declared                    | line                                                                           |
      | no endpoints and no domains | ! no config file at expiry-radar.json, and no endpoints or domains in settings |
      | 1 endpoint                  | · no config file at expiry-radar.json — running on settings alone              |

  @id:vscode.setup-and-troubleshooting.environment-check.005 @rule:setup.doctor.unusable-config-is-error @verified @source:extensions/vscode/src/doctor.ts#describeConfig @test:extensions/vscode/src/test/doctor.test.ts#a config that is not JSON is reported as unusable rather than as empty @test:extensions/vscode/src/test/doctor.test.ts#a config that enables nothing at all is an error, and the summary says so @questionable
  Scenario Outline: A config file that is not valid JSON, or enables no sources, is an error
    Given the config file <problem>
    When the operator runs the doctor
    Then the report says "<line>"
    And the summary says the doctor found something that stops it running

    Examples:
      | problem                                                               | line                                        |
      | is not valid JSON                                                     | ✗ the config file is not valid JSON         |
      | lists no endpoints or domains and enables no kubernetes, vault or aws | ✗ the config file enables no sources at all |

  @id:vscode.setup-and-troubleshooting.environment-check.006 @rule:setup.doctor.enabled-source-needs-credentials @verified @source:extensions/vscode/src/doctor.ts#describeConfig @test:extensions/vscode/src/test/doctor.test.ts#an enabled source with no credentials in the environment is a warning each
  Scenario Outline: An enabled cloud source with no way to reach it is a warning
    Given the config enables <source>
    And the editor's environment has <environment>
    When the operator runs the doctor
    Then the report says "<line>"
    And the summary says the doctor found something that will limit results

    Examples:
      | source                             | environment                                            | line                                                                                                   |
      | kubernetes with no server          | no in-cluster service host                             | ! kubernetes is enabled with no server, and this is not a cluster pod                                  |
      | vault with no addr                 | no $VAULT_ADDR                                         | ! vault is enabled with no addr and no $VAULT_ADDR                                                     |
      | vault at https://vault.example.com | no $VAULT_TOKEN                                        | ! vault is enabled (https://vault.example.com) but $VAULT_TOKEN is not set in the editor's environment |
      | aws with no profile                | no AWS access key, profile, role or web identity token | ! aws is enabled but the editor has no AWS credentials in its environment                              |

  @id:vscode.setup-and-troubleshooting.environment-check.007 @rule:setup.doctor.shrinking-settings-noted @inferred @source:extensions/vscode/src/doctor.ts#noteSettings
  Scenario: Settings that narrow what a scan collects are pointed out without raising the summary
    Given "expiryRadar.view.withinDays" is 30
    And "expiryRadar.view.minPriority" is 0.5
    When the operator runs the doctor
    Then the report says "· \"expiryRadar.view.withinDays\" is 30 — anything further out is not collected"
    And the report says "· \"expiryRadar.view.minPriority\" is 0.5 — lower-ranked items are not collected"
    And these notes alone leave the summary at "everything the doctor checks looks fine"
