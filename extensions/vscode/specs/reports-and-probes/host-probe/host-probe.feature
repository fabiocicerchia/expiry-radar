@domain:reports-and-probes @area:host-probe @from:src/commands
Feature: Probing a single host
  Answers how long one host's certificate chain and registration have left, without changing or scanning the configured estate.

  Background:
    Given a workspace folder is open

  @id:vscode.reports-and-probes.host-probe.001 @rule:probes.host.cert-and-domain-without-config @inferred @source:extensions/vscode/src/commands.ts#Commands
  Scenario: A probe checks the host's certificates and its domain, ignoring the config
    Given expiry-radar.json tracks many other endpoints
    When the operator probes "shop.example.com:8443"
    Then only the endpoint shop.example.com:8443 and the domain shop.example.com are checked
    And the configured items are not collected
    And the results are listed in rank order as "<name> — <time left>" under the title "expiry-radar: shop.example.com:8443"
    And the config file is left unchanged

  @id:vscode.reports-and-probes.host-probe.002 @rule:probes.host.nothing-returned-warns @inferred @source:extensions/vscode/src/commands.ts#Commands
  Scenario: A probe that finds nothing says so, with the CLI's warnings
    Given the host "gone.example.com" does not answer and its domain lookup fails
    When the operator probes "gone.example.com"
    Then a warning starting "expiry-radar: nothing came back for gone.example.com" is shown, followed by the CLI's source warnings joined by "; "
