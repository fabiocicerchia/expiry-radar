@domain:scanning @area:inventory-snapshot @from:src/parse
Feature: Reading the inventory report
  Turns the CLI's JSON report into the entries operators see in the panel, status bar and hovers, so every entry reads the same everywhere and keeps its identity across refreshes.

  Background:
    Given the expiry-radar CLI has produced an inventory report for the workspace folder

  @id:vscode.scanning.inventory-snapshot.001 @rule:inventory.snapshot.severity-by-deadline @verified @source:extensions/vscode/src/parse.ts#severity @test:extensions/vscode/src/test/parse.test.ts#severity follows the deadline, on the report thresholds @test:extensions/vscode/src/test/parse.test.ts#severity thresholds are configurable, and 0 still means urgent-only-if-due @test:extensions/vscode/src/test/parse.test.ts#toItems buckets each item by its deadline
  Scenario Outline: Each item's severity follows how many days it has left
    Given the warning window is <warn> days and the notice window is <info> days
    And an item has <days> days left
    When the operator loads the inventory
    Then the item is marked "<severity>"

    Examples:
      | warn | info | days | severity |
      | 14   | 30   | -1   | expired  |
      | 14   | 30   | 0    | urgent   |
      | 14   | 30   | 14   | urgent   |
      | 14   | 30   | 14.5 | soon     |
      | 14   | 30   | 30   | soon     |
      | 14   | 30   | 31   | ok       |
      | 3    | 10   | 3    | urgent   |
      | 3    | 10   | 5    | soon     |
      | 3    | 10   | 11   | ok       |

  @id:vscode.scanning.inventory-snapshot.002 @rule:inventory.snapshot.days-left-wording @verified @source:extensions/vscode/src/parse.ts#humanDays @test:extensions/vscode/src/test/parse.test.ts#humanDays never says a cheerful zero for something already broken @questionable
  Scenario Outline: Days left are shown in whole days, and expired items say how long ago
    Given an item has <days> days left
    When the operator views the item
    Then its time left reads "<shown>"

    Examples:
      | days | shown          |
      | -3.5 | expired 3d ago |
      | -0.5 | expired 0d ago |
      | 0.4  | today          |
      | 1    | 1d             |
      | 41.9 | 41d            |

  @id:vscode.scanning.inventory-snapshot.003 @rule:inventory.snapshot.namespace-prefixed-once @verified @source:extensions/vscode/src/parse.ts#displayName @test:extensions/vscode/src/test/parse.test.ts#displayName prefixes the namespace exactly once
  Scenario Outline: An item's name carries its namespace exactly once
    Given an item named "<name>" in namespace "<namespace>"
    When the operator views the item
    Then it is shown as "<shown>"

    Examples:
      | name             | namespace | shown            |
      | web-tls          | prod      | prod/web-tls     |
      | prod/web-tls     | prod      | prod/web-tls     |
      | shop.example.com |           | shop.example.com |

  @id:vscode.scanning.inventory-snapshot.004 @rule:inventory.snapshot.duplicate-reports-kept @verified @source:extensions/vscode/src/parse.ts#toItems @test:extensions/vscode/src/test/parse.test.ts#toItems assigns a stable id, and disambiguates a genuine duplicate
  Scenario: The same item reported twice by one source is listed twice, not merged
    Given the report lists the TLS certificate "shop.example.com" twice from source "tls:endpoint" and once from source "vault"
    When the operator loads the inventory
    Then three separate entries are listed
    And each keeps its own identity when the inventory is refreshed, even after a renewal moves its expiry date

  @id:vscode.scanning.inventory-snapshot.005 @rule:inventory.snapshot.readable-kind-labels @verified @source:extensions/vscode/src/parse.ts#kindLabel @test:extensions/vscode/src/test/parse.test.ts#kindLabel stays readable for a kind this build has never heard of
  Scenario Outline: Every kind gets a readable label, including kinds the extension does not know
    Given an item of kind "<kind>"
    When the operator views the item
    Then its kind reads "<label>"

    Examples:
      | kind            | label           |
      | intermediate_ca | Intermediate CA |
      | tls_cert        | TLS certificate |
      | iam_access_key  | IAM access key  |
      | ssh_host_key    | ssh host key    |

  @id:vscode.scanning.inventory-snapshot.006 @rule:inventory.snapshot.item-facts @verified @source:extensions/vscode/src/parse.ts#describe @test:extensions/vscode/src/test/parse.test.ts#describe names the namespace only when there is one
  Scenario Outline: An item's details list kind, source, namespace when present, expiry, priority and blast radius
    Given a TLS certificate "shop.example.com" from source "tls:endpoint" expiring 2026-10-09 with 40 days left, priority 0.5 and blast radius 0.5
    And it has <namespace>
    When the operator inspects the item's details
    Then the details read "kind: TLS certificate", "source: tls:endpoint", "expires: 2026-10-09 (40d)", "priority: 0.50" and "blast radius: 0.50"
    And a namespace line is <namespace_line>

    Examples:
      | namespace      | namespace_line             |
      | no namespace   | absent                     |
      | namespace prod | shown as "namespace: prod" |
