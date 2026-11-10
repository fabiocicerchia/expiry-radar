@domain:config-insights @area:expiry-squiggles @from:src/diagnostics
Feature: Flagging expiring items in the config file
  Shows the person editing expiry-radar.json, right on the line that declares a host, domain or entry, which items are expired or close to expiring and why they rank where they do, so nobody edits past a cert that dies on Friday.

  Background:
    Given a workspace folder with an expiry-radar.json config
    And the folder's warning window is 14 days and its information window is 30 days

  @id:vscode.config-insights.expiry-squiggles.001 @rule:config-insights.squiggles.severity-follows-deadline @verified @source:extensions/vscode/src/diagnostics.ts#severityFor @test:extensions/vscode/src/test/diagnostics.test.ts#severity follows the deadline, on the configured windows
  Scenario Outline: A squiggle's severity follows how close the deadline is, not the item's priority
    Given the config declares shop.example.com
    And the latest scan found shop.example.com with <days_left> days left
    When the scan results are published
    Then the line declaring shop.example.com shows <squiggle>

    Examples:
      | days_left | squiggle            |
      | -1        | an error            |
      | 5         | a warning           |
      | 14        | a warning           |
      | 20        | an information hint |
      | 30        | an information hint |
      | 90        | no squiggle         |

  @id:vscode.config-insights.expiry-squiggles.002 @rule:config-insights.squiggles.message-explains-ranking @verified @source:extensions/vscode/src/diagnostics.ts#message @test:extensions/vscode/src/test/diagnostics.test.ts#the message says what expires, when, and why it is ranked where it is @test:extensions/vscode/src/test/diagnostics.test.ts#an expired item says how long it has been broken
  Scenario Outline: A squiggle's message says what expires, when, and why it ranks where it does
    Given the config declares shop.example.com
    And the latest scan found shop.example.com with <days_left> days left, ranked because it is a "public endpoint, 2400 req/s" with blast radius 0.80
    When the user hovers over the squiggle on shop.example.com
    Then the message starts with "<headline>"
    And the message includes "public endpoint, 2400 req/s"
    And the message lists the kind, source, expiry date, priority and "blast radius: 0.80"

    Examples:
      | days_left | headline                               |
      | 5         | shop.example.com expires in 5d.        |
      | -3.2      | shop.example.com expired 3 day(s) ago. |

  @id:vscode.config-insights.expiry-squiggles.003 @rule:config-insights.squiggles.same-day-wording @inferred @source:extensions/vscode/src/diagnostics.ts#message @questionable
  Scenario: An item expiring later today is announced as "expires in today"
    Given the config declares shop.example.com
    And the latest scan found shop.example.com expiring in under one day
    When the user hovers over the squiggle on shop.example.com
    Then the message starts with "shop.example.com expires in today."
