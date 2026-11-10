@domain:cloud-providers @area:http-api-client @from:source/httpjson
Feature: Reading provider APIs with a credential
  Serves operators who point expiry-radar at REST provider accounts (Hetzner, Fastly, Harbor, Artifactory, App Store Connect, Gandi, DNSimple, GoDaddy): when the provider refuses or garbles a read, the operator gets a sentence saying what went wrong instead of a silently empty inventory.

  Background:
    Given the operator has enabled a provider source and given it a credential

  @id:cloud-providers.http-api-client.001 @rule:providers.api-access.denial-names-missing-permission @inferred @source:internal/source/httpjson.go#getJSON
  Scenario Outline: A refused credential is reported with what it is missing
    Given the <provider> credential is refused by the provider with <status>
    When the operator runs a scan
    Then the scan reports that <provider> could not be read, quoting the address and "<status>"
    And the message ends with the hint "<hint>"
    And no items are reported from that <provider> request

    Examples:
      | provider            | status           | hint                                                                                    |
      | Hetzner Cloud       | 401 Unauthorized | the API token needs read access to this project                                         |
      | Fastly certificates | 403 Forbidden    | the token needs the global:read scope                                                   |
      | Fastly tokens       | 403 Forbidden    | listing tokens needs an account-level credential                                        |
      | Harbor              | 403 Forbidden    | listing robot accounts needs a Harbor administrator                                     |
      | JFrog Artifactory   | 401 Unauthorized | listing tokens needs an admin access token                                              |
      | App Store Connect   | 403 Forbidden    | the App Store Connect key needs at least Developer access                               |
      | GoDaddy             | 401 Unauthorized | the key needs domain read access — note GoDaddy gates its API on holding enough domains |

  @id:cloud-providers.http-api-client.002 @rule:providers.api-access.unexpected-status-reported @inferred @source:internal/source/httpjson.go#getJSON @questionable
  Scenario Outline: Any answer other than OK is reported as a failure, not read as an empty account
    Given the provider answers the read with <status>
    When the operator runs a scan
    Then the scan reports "GET <address>: <status>" for that provider
    And no permission hint is added
    And no items are reported from that request

    Examples:
      | status                    |
      | 404 Not Found             |
      | 429 Too Many Requests     |
      | 500 Internal Server Error |
      | 204 No Content            |

  @id:cloud-providers.http-api-client.003 @rule:providers.api-access.unreadable-response-reported @inferred @source:internal/source/httpjson.go#getJSON
  Scenario: A provider answer that cannot be read is reported rather than treated as nothing expiring
    Given the provider answers 200 OK with a body that is not valid JSON
    When the operator runs a scan
    Then the scan reports "GET <address>:" followed by why the answer could not be read
    And no items are reported from that request

  @id:cloud-providers.http-api-client.004 @rule:providers.api-access.credentials-kept-out-of-errors @inferred @source:internal/source/httpjson.go#getJSON
  Scenario: Provider credentials never appear in reported failures
    Given the provider credential is "tok_example_123"
    And the provider cannot be reached, refuses the credential, or answers with an error
    When the operator runs a scan
    Then the reported failure names the request address and the reason
    And the reported failure does not contain "tok_example_123"
