@domain:reports-and-probes @area:html-report @from:src/report
Feature: Viewing the HTML report in the editor
  Lets an operator read the same self-contained HTML report the CLI produces for CI, inside an editor tab, without it reaching the network.

  Background:
    Given the expiry-radar CLI has rendered its HTML report for a workspace folder named "payments-api"

  @id:vscode.reports-and-probes.html-report.002 @rule:reports.html-report.shown-as-cli-renders-it @inferred @source:extensions/vscode/src/report.ts#ReportView
  Scenario: The report opens in an editor tab exactly as the CLI rendered it
    When the operator opens the report
    Then an editor tab titled "expiry-radar — payments-api" shows the CLI's report, with its own layout and filters
    And the find widget is available in that tab

  @id:vscode.reports-and-probes.html-report.003 @rule:reports.html-report.single-tab-reused @inferred @source:extensions/vscode/src/report.ts#ReportView
  Scenario: Opening the report again reuses the one report tab
    Given the report tab is already open for "payments-api"
    When the operator opens the report again
    Then no second report tab appears
    And the existing tab comes to the front showing the latest report

  @id:vscode.reports-and-probes.html-report.004 @rule:reports.html-report.external-links-open-in-browser @inferred @source:extensions/vscode/src/report.ts#adapt
  Scenario: Web links in the report open in the user's browser
    Given the report tab is open
    And the report links to "https://ca.example.com/renew"
    When the operator clicks that link
    Then the link opens in the system browser
    And the report tab keeps showing the report

  @id:vscode.reports-and-probes.html-report.005 @rule:reports.html-report.no-network-access @inferred @source:extensions/vscode/src/report.ts#adapt
  Scenario: The report in the editor cannot load anything from the network
    Given the report tab is open
    When the report is displayed
    Then only the report's own inline styles and scripts run
    And images load only from the extension itself or embedded data
    And any other remote style, script, image, font or connection is blocked
