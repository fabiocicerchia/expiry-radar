@domain:config-insights @area:declaration-lookup @from:src/locate
Feature: Locating where tracked items are declared in the config
  Lets an operator jump from a recorded host, domain or manual item straight to the exact place in expiry-radar.json that declares it, so a warning lands on the line someone has to edit.

  Background:
    Given the workspace has an expiry-radar config file

  @id:vscode.config-insights.declaration-lookup.002 @rule:config-insights.declaration-lookup.points-at-declared-value @verified @source:extensions/vscode/src/locate.ts#declaredIn @test:extensions/vscode/src/test/locate.test.ts#an endpoint host is placed on the line that declares it @test:extensions/vscode/src/test/locate.test.ts#domains are placed individually, not as one array
  Scenario Outline: A recorded item is located at the value that declares it
    Given the config declares the <kind> "<name>" on line <line>
    When the extension locates where "<name>" is declared
    Then the location is line <line> of the config file
    And the column is the opening quote of "<name>", not the key in front of it
    And each item in a list of domains gets its own column on that line

    Examples:
      | kind              | name                 | line |
      | endpoint host     | shop.example.com     | 3    |
      | domain            | example.net          | 6    |
      | manual entry name | payments-signing-key | 9    |

  @id:vscode.config-insights.declaration-lookup.003 @rule:config-insights.declaration-lookup.exact-name-match @verified @source:extensions/vscode/src/locate.ts#declaredIn @test:extensions/vscode/src/test/locate.test.ts#a host with a port keeps the port, because the item name does @test:extensions/vscode/src/test/locate.test.ts#an escaped character in a host round-trips to the name the CLI reports
  Scenario Outline: An item is located only under the exact name the CLI reports
    Given the config declares "<declared>"
    When the extension locates where "<looked_up>" is declared
    Then the item is <outcome>

    Examples:
      | declared                        | looked_up                       | outcome                     |
      | admin.internal.example.com:8443 | admin.internal.example.com:8443 | found on its declaring line |
      | admin.internal.example.com:8443 | admin.internal.example.com      | not found                   |
      | example.com                     | example.com                     | found                       |

  @id:vscode.config-insights.declaration-lookup.004 @rule:config-insights.declaration-lookup.only-recorded-items-located @verified @source:extensions/vscode/src/locate.ts#declaredIn @test:extensions/vscode/src/test/locate.test.ts#only the two declared arrays are scanned @questionable
  Scenario: Only hosts, domains and manual entries get a config location
    Given the config also lists Kubernetes namespaces "prod" and "payments" and an override matching "payments/*"
    And the inventory has a certificate discovered on a cluster Ingress
    When the extension locates declarations in the config
    Then "prod" and "payments/*" have no declared location
    And the discovered certificate has no declared location and is shown in the panel only

  @id:vscode.config-insights.declaration-lookup.005 @rule:config-insights.declaration-lookup.first-declaration-wins @verified @source:extensions/vscode/src/locate.ts#declaredIn @test:extensions/vscode/src/test/locate.test.ts#the first declaration of a duplicated host wins
  Scenario: A duplicated item points at its first declaration
    Given the config declares the host "a.example.com" on line 3 and again on line 4
    When the extension locates where "a.example.com" is declared
    Then the location is line 3

  @id:vscode.config-insights.declaration-lookup.006 @rule:config-insights.declaration-lookup.incomplete-config-claims-nothing @verified @source:extensions/vscode/src/locate.ts#arraySpan @test:extensions/vscode/src/test/locate.test.ts#a config being typed claims nothing rather than guessing @test:extensions/vscode/src/test/locate.test.ts#a config with neither array is not an error
  Scenario Outline: A config that is mid-edit or not JSON locates nothing rather than guessing
    Given the config text is <state>
    When the extension locates declarations in the config
    Then no item from the affected list gets a declared location
    And no error is reported

    Examples:
      | state                                                       |
      | an endpoints list still being typed with no closing bracket |
      | a config with no endpoints, manual or domains list          |
      | not JSON at all                                             |

  @id:vscode.config-insights.declaration-lookup.007 @rule:config-insights.declaration-lookup.brackets-in-values-ignored @verified @source:extensions/vscode/src/locate.ts#arraySpan @test:extensions/vscode/src/test/locate.test.ts#a brace inside a value cannot close the array early
  Scenario: Brackets inside a value do not hide later declarations
    Given the config's first endpoint has a label note "]}"
    And a second endpoint "b.example.com" is declared on line 4
    When the extension locates where "b.example.com" is declared
    Then the location is line 4
