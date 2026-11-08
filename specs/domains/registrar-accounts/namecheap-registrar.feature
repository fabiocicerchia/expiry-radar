@domain:domains @area:registrar-accounts @from:source/namecheap
Feature: Tracking domains and certificates held at Namecheap
  Lets an operator see every domain and resold SSL certificate in a Namecheap account, and whether each renewal is actually set up, so a lapsing domain is caught before it goes dark.

  Background:
    Given the Namecheap source is enabled with an API user, a user name, a client IP and $NAMECHEAP_API_KEY

  @id:domains.registrar-accounts.001 @rule:domains.namecheap.allowlist-error-explained @verified @source:internal/source/namecheap.go#ncError @test:internal/source/namecheap_test.go#TestNamecheapExplainsTheAllowlistError
  Scenario: An unallowlisted machine is told it is the IP, not the credentials
    Given the machine running the scan is not on the Namecheap account's IP allowlist
    When the operator runs a scan
    Then the scan reports error 1011150 followed by "(this machine's public IP is not allowlisted in the Namecheap account)"
