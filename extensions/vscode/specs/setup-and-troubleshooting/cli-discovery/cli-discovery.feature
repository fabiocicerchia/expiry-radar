@domain:setup-and-troubleshooting @area:cli-discovery @from:src/runner
Feature: Finding the expiry-radar CLI
  Lets an operator use the extension without wiring up paths by hand: it finds the binary where builds and installs usually put it, and offers a one-click fix when it cannot find one.

  @id:vscode.setup-and-troubleshooting.cli-discovery.001 @rule:cli.discovery.most-explicit-location-wins @inferred @source:extensions/vscode/src/runner.ts#resolveBinary
  Scenario Outline: The most explicit binary location is used first
    Given an expiry-radar binary exists at <present>
    And no binary exists at any location listed before it
    When the operator starts a scan
    Then the scan runs the binary at <present>
    And the activity log says "using <present>"

    Examples:
      | present                                                                |
      | the path set in "expiryRadar.path" (a leading ~ means the home folder) |
      | bin/expiry-radar inside the workspace folder                           |
      | the first expiry-radar on the PATH                                     |
      | expiry-radar in GOBIN                                                  |
      | expiry-radar in GOPATH/bin                                             |
      | ~/go/bin/expiry-radar                                                  |

  @id:vscode.setup-and-troubleshooting.cli-discovery.002 @rule:cli.discovery.missing-binary-offers-install @inferred @source:extensions/vscode/src/runner.ts#promptInstall
  Scenario: A missing binary comes with an offer to install it
    Given no expiry-radar binary exists in any of the usual locations
    When the operator starts a scan
    Then an error says the expiry-radar binary was not found and names the command "go install github.com/fabiocicerchia/expiry-radar/cmd/expiry-radar@latest"
    And the error offers Install, Copy command and Open settings
    And choosing Install runs that command in a terminal named "expiry-radar: install", and the next scan looks for the binary again
    And choosing Copy command puts that command on the clipboard
    And choosing Open settings opens the expiryRadar settings
