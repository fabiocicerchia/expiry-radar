@domain:configuration @area:config-file @from:config/config
Feature: Loading the config file
  Operators keep one JSON config in git, and the run must refuse a file it cannot read exactly as written (or one holding a credential) rather than quietly watching less.

  Background:
    Given the operator keeps the expiry-radar config file in a git repository

  @id:configuration.config-file.001 @rule:configuration.config-file.unreadable-file-refused @verified @source:internal/config/config.go#Load @test:internal/config/config_test.go#TestLoadRejectsConfigsThatWouldSilentlyScanLess
  Scenario Outline: A config file that cannot be read as written is refused, naming the file
    Given the config file "expiry-radar.json" contains <content>
    When the operator runs a scan with that config file
    Then the run is refused before any source is scanned
    And the error names "expiry-radar.json" and says "<message>"

    Examples:
      | content                                                  | message          |
      | a truncated document {"domains": [                       | unexpected EOF   |
      | text where a list is expected {"domains": "example.com"} | cannot unmarshal |

  @id:configuration.config-file.002 @rule:configuration.config-file.unknown-key-refused @verified @source:internal/config/config.go#Load @test:internal/config/config_test.go#TestLoadRejectsConfigsThatWouldSilentlyScanLess
  Scenario: A misspelt key in the config file is refused instead of silently disabling a source
    Given the config file enables Vault under the misspelt key "vualt"
    When the operator runs a scan with that config file
    Then the run is refused before any source is scanned
    And the error says "unknown field" and names the config file

  @id:configuration.config-file.003 @rule:configuration.config-file.credentials-never-from-file @inferred @source:internal/config/config.go#File
  Scenario Outline: A credential written into the config file is refused
    Given the config file enables <source> and also sets "<key>" to a secret value
    When the operator runs a scan with that config file
    Then the run is refused before any source is scanned
    And the error says unknown field "<key>"
    And the secret from the file is never used

    Examples:
      | source     | key          |
      | vault      | token        |
      | cloudflare | token        |
      | azure      | clientSecret |
      | harbor     | password     |
      | namecheap  | apiKey       |
      | scaleway   | secretKey    |
