@domain:configuration @area:validation @from:config/validate
Feature: Refusing source configuration that cannot scan
  Operators learn at load time, before any scan runs, that an enabled source is missing a credential or a required setting, instead of getting a report that silently covers less than they asked for.

  Background:
    Given a config file that the operator passes to expiry-radar

  @id:configuration.validation.001 @rule:config.validation.enabled-source-needs-its-credential @verified @source:internal/config/validate.go#requireEnv @test:internal/config/config_test.go#TestVaultWithoutCredentialsIsRefusedAtLoad
  Scenario Outline: An enabled source whose credential variable is unset is refused
    Given the <source> source is enabled in the config with every other setting it requires
    And the environment variable <variable> is not set
    When the operator runs expiry-radar with that config
    Then no scan runs and the command exits with status 2
    And the error names the config file and says "<source> source is enabled but $<variable> is not set"

    Examples:
      | source       | variable             |
      | cloudflare   | CLOUDFLARE_API_TOKEN |
      | gitlab       | GITLAB_TOKEN         |
      | github       | GITHUB_TOKEN         |
      | azure        | AZURE_CLIENT_SECRET  |
      | fastly       | FASTLY_API_TOKEN     |
      | hetzner      | HCLOUD_TOKEN         |
      | harbor       | HARBOR_PASSWORD      |
      | jfrog        | JFROG_ACCESS_TOKEN   |
      | okta         | OKTA_API_TOKEN       |
      | namecheap    | NAMECHEAP_API_KEY    |
      | digitalocean | DIGITALOCEAN_TOKEN   |
      | scaleway     | SCW_SECRET_KEY       |
      | vault        | VAULT_TOKEN          |
