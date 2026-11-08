@domain:domains @area:registrar-accounts @from:source/registrars
Feature: Reading domain renewal status from registrar accounts
  Gives operators who hold registrar credentials the one fact public RDAP cannot: whether each domain will actually auto-renew, so a lapsing domain ranks above one somebody else is already renewing.

  Background:
    Given the operator has configured registrar accounts with their credentials set in the environment

  @id:domains.registrar-accounts.002 @rule:domains.registrar-accounts.unreadable-registrar-config-refused @verified @source:internal/source/registrars.go#ValidateRegistrars @test:internal/source/registrars_test.go#TestRegistrarValidationRejectsWhatItCannotRead
  Scenario Outline: A registrar entry that could never be read is refused at load
    Given the config lists a registrar entry <entry>
    When the operator runs a scan
    Then the configuration is rejected with an error containing "<message>"

    Examples:
      | entry                                  | message                                                     |
      | with no name                           | name is required (one of dnsimple, gandi, godaddy, porkbun) |
      | named "notaregistrar"                  | unknown registrar "notaregistrar"                           |
      | named "dnsimple" without an account id | dnsimple needs an account id                                |
