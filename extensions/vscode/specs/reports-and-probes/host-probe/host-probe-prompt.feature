@domain:reports-and-probes @area:host-probe @from:src/prompts
Feature: Naming the host to probe
  Lets an operator check a hostname they are looking at without retyping it or editing the config.

  Background:
    Given a workspace folder is open

  @id:vscode.reports-and-probes.host-probe.003 @rule:reports-and-probes.host-probe.host-from-selection @inferred @source:extensions/vscode/src/prompts.ts#promptHost
  Scenario: The host to probe starts from the selected text and cannot be blank
    Given the operator has "shop.example.com" selected in the editor
    When the operator probes a host
    Then an input box titled "expiry-radar: probe a host" is shown, filled with "shop.example.com"
    And clearing the box shows "A host is required."
