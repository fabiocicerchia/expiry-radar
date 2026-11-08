@domain:reports-and-probes @area:report-export @from:src/commands
Feature: Exporting the report to a file
  Lets an operator save the inventory in a chosen format to share outside the editor.

  Background:
    Given a workspace folder is open

  @id:vscode.reports-and-probes.report-export.001 @rule:reports.export.dated-file-in-chosen-format @inferred @source:extensions/vscode/src/commands.ts#Commands
  Scenario Outline: An export is saved where the operator chooses, named with today's date by default
    Given today is 2026-11-08
    When the operator exports the report as <format>
    Then the save dialog suggests expiry-radar-2026-11-08.<ext> in the workspace folder
    And the file is written with the CLI's <format> output
    And "expiry-radar: exported to expiry-radar-2026-11-08.<ext>." is shown with an Open action

    Examples:
      | format | ext  |
      | HTML   | html |
      | JSON   | json |
