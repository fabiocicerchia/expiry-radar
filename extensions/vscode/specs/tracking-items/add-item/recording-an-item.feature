@domain:tracking-items @area:add-item @from:src/prompts
Feature: Recording an item to track
  Lets an operator add a host, a domain or an item nothing can discover to the inventory through guided prompts, without learning the config schema.

  Background:
    Given a workspace folder is open

  @id:vscode.tracking-items.add-item.003 @rule:tracking-items.add-item.three-recordable-kinds @verified @source:extensions/vscode/src/prompts.ts#pickEntryKind @test:extensions/vscode/src/test/activate.test.ts#adding an endpoint prompts for it, writes it, and opens the file it wrote
  Scenario: The operator chooses between an endpoint, a domain and something nothing can discover
    When the operator starts adding an item
    Then a picker titled "expiry-radar: record what?" is shown
    And it offers "Endpoint", "Domain" and "Something nothing can discover", each with a line explaining what it is
    And its placeholder says "Everything else is discovered, not recorded"

  @id:vscode.tracking-items.add-item.004 @rule:tracking-items.add-item.single-value-defaults-to-selection @verified @source:extensions/vscode/src/prompts.ts#promptValue @test:extensions/vscode/src/test/activate.test.ts#adding an endpoint prompts for it, writes it, and opens the file it wrote
  Scenario Outline: An endpoint or domain is asked for once, starting from the selected text
    Given the operator has <selected> selected in the editor
    When the operator records a <kind> and accepts the suggested value
    Then an input box titled "<title>" is shown, already filled with "<value>"
    And the config gains the entry <written> under "<array>"

    Examples:
      | kind     | selected                                 | title                            | value            | written                     | array     |
      | Endpoint | "shop.example.com" with spaces around it | expiry-radar: record an endpoint | shop.example.com | {"host":"shop.example.com"} | endpoints |
      | Domain   | "example.com"                            | expiry-radar: record a domain    | example.com      | "example.com"               | domains   |

  @id:vscode.tracking-items.add-item.005 @rule:tracking-items.add-item.blank-value-refused @inferred @source:extensions/vscode/src/prompts.ts#promptValue
  Scenario Outline: A blank host, domain or name cannot be accepted
    When the operator leaves the <field> blank or enters only spaces
    Then the input box shows "<message>"
    And nothing is written to the config

    Examples:
      | field            | message              |
      | endpoint host    | A value is required. |
      | domain           | A value is required. |
      | manual item name | A name is required.  |

  @id:vscode.tracking-items.add-item.006 @rule:tracking-items.add-item.manual-kind-sets-blast-radius @inferred @source:extensions/vscode/src/prompts.ts#promptManualEntry
  Scenario: An undiscoverable item is recorded with a name, a kind that sets its blast radius, and an expiry date
    When the operator records "acme-corp.co.uk" as something nothing can discover, of kind "Domain", expiring "2027-03-01"
    Then the operator is asked three times, titled "record an item — 1 of 3", "2 of 3" and "3 of 3"
    And the kind step says "What kind? This sets its base blast radius." and offers Trust anchor, Domain, Intermediate CA, TLS certificate, IAM access key, Secret and Vault lease, in that order, each with a hint
    And the config gains {"name":"acme-corp.co.uk","kind":"domain","expires":"2027-03-01"} under "manual"

  @id:vscode.tracking-items.add-item.007 @rule:tracking-items.add-item.backing-out-records-nothing @verified @source:extensions/vscode/src/prompts.ts#promptManualEntry @test:extensions/vscode/src/test/activate.test.ts#backing out of the first prompt writes no config at all
  Scenario Outline: Backing out of any prompt records nothing
    When the operator dismisses the <step> prompt
    Then no further prompt is shown
    And the config file is not created or changed

    Examples:
      | step                    |
      | record what?            |
      | endpoint host           |
      | manual item name        |
      | manual item kind        |
      | manual item expiry date |
