@domain:cloud-providers @area:other-clouds @from:source/digitalocean
Feature: Tracking DigitalOcean certificates
  Gives operators of a DigitalOcean account every load-balancer and app certificate in the ranked inventory, telling apart the ones DigitalOcean renews itself from the uploaded ones that lapse unless someone acts.

  Background:
    Given the DigitalOcean source is enabled in the config file

  @id:cloud-providers.other-clouds.001 @rule:cloud-providers.digitalocean.token-required @verified @source:internal/config/validate.go#requireEnv @test:internal/source/hosting_test.go#TestHostingSourcesNeedTheirCredentialBeforeRunning
  Scenario: An enabled DigitalOcean source without a token is refused, naming the variable
    Given $DIGITALOCEAN_TOKEN is not set
    When the operator runs a scan
    Then the run is refused with "digitalocean source is enabled but $DIGITALOCEAN_TOKEN is not set"
