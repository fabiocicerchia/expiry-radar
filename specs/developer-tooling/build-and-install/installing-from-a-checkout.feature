@domain:developer-tooling @area:build-and-install @from:expiry-radar/makefile
Feature: Installing expiry-radar from a checkout
  Lets an operator install, remove and wire the editor integrations to the working-tree build of expiry-radar without a package manager.

  Background:
    Given the operator has a checkout of expiry-radar with a Go toolchain available

  @id:developer-tooling.build-and-install.001 @rule:tooling.install.system-prefix-places-binary-and-man-page @inferred @source:Makefile#install
  Scenario Outline: A system install places the binary and its manual page under the chosen prefix
    Given the operator chooses the prefix <prefix> and the staging root <destdir>
    When the operator installs expiry-radar
    Then the binary is at <destdir><prefix>/bin/expiry-radar and is executable
    And the manual page is at <destdir><prefix>/share/man/man1/expiry-radar.1
    And the operator sees "installed <destdir><prefix>/bin/expiry-radar"

    Examples:
      | prefix     | destdir      |
      | /usr/local |              |
      | /usr       | /tmp/pkgroot |

  @id:developer-tooling.build-and-install.002 @rule:tooling.install.per-user-install-reports-success @inferred @source:Makefile#install @questionable
  Scenario: A per-user install fails on the manual page step unless a manual directory is supplied
    Given the operator has not chosen a prefix
    And no per-user manual directory is supplied
    When the operator installs expiry-radar
    Then the binary is installed where the local Go toolchain puts binaries
    And the install stops with an error creating a directory with an empty name
    And the operator does not see the "installed" confirmation or the PATH note

  @id:developer-tooling.build-and-install.003 @rule:tooling.install.uninstall-removes-installed-files @inferred @source:Makefile#uninstall
  Scenario Outline: Uninstalling removes the binary and manual page that install placed
    Given expiry-radar was installed with the prefix <prefix>
    When the operator uninstalls expiry-radar
    Then <binary> no longer exists
    And the operator sees "removed <binary>"

    Examples:
      | prefix     | binary                                        |
      | /usr/local | /usr/local/bin/expiry-radar                   |
      | (none)     | the Go toolchain's bin directory/expiry-radar |

  @id:developer-tooling.build-and-install.004 @rule:tooling.editor-install.missing-vscode-still-installs-neovim @inferred @source:Makefile#ext-install
  Scenario: Installing the editor integrations without VS Code still installs the Neovim plugin
    Given the VS Code command-line launcher is not on PATH
    And Neovim is on PATH
    When the operator installs the editor integrations
    Then the operator is told the 'code' command is missing and how VS Code installs it
    And the VS Code extension is skipped
    And the Neovim plugin is linked to the checkout so pulling updates it
    And the operator is pointed to :ExpiryRadarReport and :help expiry-radar

  @id:developer-tooling.build-and-install.005 @rule:tooling.editor-test.runs-against-fresh-binary @inferred @source:Makefile#ext-test
  Scenario: Editor integration tests always run against a freshly built binary
    Given no binary has been built in the checkout
    When the operator runs the editor integration tests
    Then the expiry-radar binary is built first
    And the VS Code and Neovim tests drive that binary rather than skipping
