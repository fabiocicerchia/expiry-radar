@domain:tracking-items @area:entry-validation @from:src/edit
Feature: Recording items in the config
  Lets an operator record hosts, domains and hand-tracked items from the editor without ever writing a config the CLI would refuse to load or rank wrongly.

  Background:
    Given the operator is recording an item for the workspace's expiry-radar config

  @id:vscode.tracking-items.entry-validation.001 @rule:tracking.entry-validation.expiry-accepted-only-as-cli-accepts @verified @source:extensions/vscode/src/edit.ts#invalidExpires @test:extensions/vscode/src/test/edit.test.ts#the date check accepts what the CLI accepts @test:extensions/vscode/src/test/edit.test.ts#the date check rejects what the CLI would reject at load
  Scenario Outline: An expiry date is accepted only in a form the CLI will load
    Given the operator is at the expiry step of recording a manual item
    When the operator enters <expires> as the expiry
    Then the prompt shows <message>

    Examples:
      | expires                   | message                                       |
      | 2027-03-01                | no error, and the date is accepted            |
      | 2027-03-01T15:04:05Z      | no error, and the date is accepted            |
      | 2027-03-01T15:04:05+02:00 | no error, and the date is accepted            |
      | (blank)                   | A date is required.                           |
      | next march                | Use YYYY-MM-DD, or a full RFC 3339 timestamp. |
      | 03/01/2027                | Use YYYY-MM-DD, or a full RFC 3339 timestamp. |
      | 2027-02-31                | 2027-02-31 is not a real date.                |
      | 2027-03-01T25:99:00Z      | 2027-03-01T25:99:00Z is not a valid time.     |

  @id:vscode.tracking-items.entry-validation.002 @rule:tracking.entry-validation.manual-entry-shape @verified @source:extensions/vscode/src/edit.ts#ENTRY_KINDS @test:extensions/vscode/src/test/edit.test.ts#a manual entry carries what ranking needs, and omits what it does not
  Scenario Outline: A manual item is recorded with its name, kind and expiry, and a namespace only when one is given
    Given the operator has named a manual item "<name>", chosen kind <kind>, expiry <expires> and namespace "<namespace>"
    When the operator records the item
    Then the config entry is <written>

    Examples:
      | name         | kind     | expires    | namespace      | written                                                                                |
      | code-signing | tls_cert | 2026-11-15 | release        | {"name":"code-signing","kind":"tls_cert","expires":"2026-11-15","namespace":"release"} |
      | a            | secret   | 2026-11-15 | (blank spaces) | {"name":"a","kind":"secret","expires":"2026-11-15"}                                    |

  @id:vscode.tracking-items.entry-validation.003 @rule:tracking.entry-validation.entries-trimmed @verified @source:extensions/vscode/src/edit.ts#ENTRY_KINDS @test:extensions/vscode/src/test/edit.test.ts#entries are trimmed, so a pasted hostname does not become a name with a space
  Scenario Outline: Recorded hosts and domains are written without surrounding whitespace
    Given the operator has pasted "<pasted>" as a <kind>
    When the operator records the item
    Then the config entry is <written>

    Examples:
      | kind     | pasted             | written                     |
      | domain   |   example.com \n   | "example.com"               |
      | endpoint |  shop.example.com  | {"host":"shop.example.com"} |

  @id:vscode.tracking-items.entry-validation.004 @rule:tracking.entry-validation.recorded-vs-discovered @verified @source:extensions/vscode/src/edit.ts#arrayForSource @test:extensions/vscode/src/test/edit.test.ts#a source maps to the array it records into, and discovery maps to none
  Scenario Outline: Only recorded items map to a config entry; discovered items have none to remove
    Given the inventory shows an item found by source <source>
    When the operator asks to stop tracking the item
    Then <outcome>

    Examples:
      | source       | outcome                                                                                          |
      | tls:endpoint | its entry is looked up under "endpoints" in the config                                           |
      | domain:rdap  | its entry is looked up under "domains" in the config                                             |
      | domain:whois | its entry is looked up under "domains" in the config                                             |
      | manual       | its entry is looked up under "manual" in the config                                              |
      | k8s:secret   | the warning "could not find the entry for <item> to remove" is shown and the config is unchanged |
      | aws:acm      | the warning "could not find the entry for <item> to remove" is shown and the config is unchanged |
      | tls:chain    | the warning "could not find the entry for <item> to remove" is shown and the config is unchanged |

  @id:vscode.tracking-items.entry-validation.005 @rule:tracking.entry-validation.manual-kind-choices @inferred @source:extensions/vscode/src/edit.ts#MANUAL_KINDS
  Scenario: A manual item's kind is chosen from the CLI's kinds, each explaining its blast radius
    Given the operator is at the kind step of recording a manual item
    When the operator opens the kind choice
    Then the choices are, in order: Trust anchor, Domain, Intermediate CA, TLS certificate, IAM access key, Secret, Vault lease
    And each choice shows a hint about what fails when it expires, such as "every leaf it signed, at once" for Intermediate CA
