@domain:setup-and-troubleshooting @area:workspace-folders @from:src/folder
Feature: Choosing the workspace folder a command applies to
  Lets a developer in a single- or multi-root workspace know which folder's inventory, config and settings a scan or command will use, so the panel and the squiggles always describe the same folder.

  Background:
    Given the workspace has the folders "payments" and "dashboards" open, in that order

  @id:vscode.setup-and-troubleshooting.workspace-folders.001 @rule:workspace.folders.command-targets-active-editor-folder @inferred @source:extensions/vscode/src/folder.ts#primaryFolder
  Scenario Outline: A command applies to the folder of the file being edited, otherwise to the first folder
    Given <editor_state>
    When the developer runs an expiry-radar command
    Then the command applies to the "<target>" folder

    Examples:
      | editor_state                                                  | target     |
      | the active editor shows a file inside "dashboards"            | dashboards |
      | the active editor shows a file inside "payments"              | payments   |
      | no editor is open                                             | payments   |
      | the active editor shows a file outside every workspace folder | payments   |

  @id:vscode.setup-and-troubleshooting.workspace-folders.002 @rule:workspace.folders.settings-read-per-folder @inferred @source:extensions/vscode/src/folder.ts#settingsFor
  Scenario: A command uses the settings of the folder it applies to
    Given the "dashboards" folder overrides the expiry-radar config path to "radar/staging.json"
    And the "payments" folder uses the default config path
    When the developer runs a scan while editing a file inside "dashboards"
    Then the scan reads the config at "radar/staging.json" in "dashboards"
    And the settings of "payments" are not used
