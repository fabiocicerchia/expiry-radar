@domain:tracking-items @area:config-writing @from:src/splice
Feature: Writing tracked items into the config file
  Operators who add or stop tracking an item from the editor get a minimal, valid edit to their expiry-radar config rather than a rewritten file, so nothing they already declared is lost.

  Background:
    Given the operator records or removes items for the workspace's expiry-radar config

  @id:vscode.tracking-items.config-writing.002 @rule:tracking.config-writing.add-preserves-layout @verified @source:extensions/vscode/src/splice.ts#addToArray @test:extensions/vscode/src/test/edit.test.ts#an entry is appended to a multi-line array, indented like its siblings @test:extensions/vscode/src/test/edit.test.ts#a one-line array stays a one-line array @test:extensions/vscode/src/test/edit.test.ts#an empty array takes the first entry without a stray comma @test:extensions/vscode/src/test/edit.test.ts#four-space and tab indentation are preserved @test:extensions/vscode/src/test/edit.test.ts#a bracket inside a value does not confuse the append
  Scenario Outline: A new entry is added in the layout the array already uses
    Given the config's <array> array is written as <layout>
    When the operator adds <entry> to the <array> array
    Then the <array> array reads <result>
    And no other entry of the config is changed
    And the config still loads as valid JSON

    Examples:
      | array     | layout                                   | entry                      | result                                                                                |
      | domains   | one line: ["example.com", "example.net"] | "example.org"              | ["example.com", "example.net", "example.org"] on the same line                        |
      | domains   | one line, empty: []                      | "a.example"                | ["a.example"] with no stray comma                                                     |
      | endpoints | one host per line, indented 4 spaces     | {"host":"api.example.com"} | the new host on its own line, indented 4 spaces, with a comma after the previous host |
      | domains   | one domain per line, indented with tabs  | "b.example"                | the new domain on its own line, indented with the same tabs                           |
      | domains   | several lines, empty                     | "a.example"                | "a.example" on a line of its own, inside the brackets                                 |

  @id:vscode.tracking-items.config-writing.003 @rule:tracking.config-writing.add-keeps-existing-config @verified @source:extensions/vscode/src/splice.ts#addKey @test:extensions/vscode/src/test/edit.test.ts#a missing key is added to the object rather than replacing it @test:extensions/vscode/src/test/edit.test.ts#an empty or absent config becomes a config
  Scenario Outline: Adding the first item of a kind keeps everything already in the config
    Given the config <state>
    When the operator adds the domain "a.example"
    Then the config has a "domains" list containing "a.example"
    And every entry the config already had is still there
    And the config still loads as valid JSON

    Examples:
      | state                                                        | note                                       |
      | declares one endpoint "shop.example.com" and no domains list | the endpoint survives next to the new list |
      | is an empty object {}                                        | the list is added inside the object        |
      | file does not exist yet                                      | a new config is written around the entry   |
      | is blank                                                     | a new config is written around the entry   |

  @id:vscode.tracking-items.config-writing.004 @rule:tracking.config-writing.non-object-config-replaced @inferred @source:extensions/vscode/src/splice.ts#addKey @questionable
  Scenario: A config that does not start with an object is replaced by a new one
    Given the config file has content that does not begin with "{", such as a leading comment or a bare list
    And the file has no "domains" list
    When the operator adds the domain "a.example"
    Then the whole file is overwritten with a new config holding only the "domains" list with "a.example"
    And everything the file held before is gone

  @id:vscode.tracking-items.config-writing.005 @rule:tracking.config-writing.cursor-on-new-entry @verified @source:extensions/vscode/src/splice.ts#addToArray @test:extensions/vscode/src/test/edit.test.ts#the reported line is where the entry actually landed @test:extensions/vscode/src/test/edit.test.ts#an entry is appended to a multi-line array, indented like its siblings
  Scenario: After adding an item, the config opens with the cursor on the new entry
    Given the config lists the domain "a.example" on its own line
    When the operator adds the domain "b.example"
    Then the config opens in the editor
    And the cursor is on the line that reads "b.example"

  @id:vscode.tracking-items.config-writing.006 @rule:tracking.config-writing.remove-leaves-valid-config @verified @source:extensions/vscode/src/splice.ts#removeEntry @test:extensions/vscode/src/test/edit.test.ts#removing the middle of a multi-line array leaves valid JSON @test:extensions/vscode/src/test/edit.test.ts#removing the last element takes the comma before it, not after @test:extensions/vscode/src/test/edit.test.ts#removing the only element leaves an empty array, not a broken one @test:extensions/vscode/src/test/edit.test.ts#an entry spanning several lines is removed whole @test:extensions/vscode/src/test/edit.test.ts#a brace inside a value does not end the element early
  Scenario Outline: Removing a recorded item leaves a valid config with the other entries intact
    Given the config's <array> list holds <entries>, one per line
    When the operator stops tracking <target>
    Then the <array> list holds <remaining>
    And, unless the list is left empty, no blank line is left where the entry was
    And the config still loads as valid JSON

    Examples:
      | array     | entries                                            | target       | remaining               |
      | endpoints | a.example, b.example, c.example                    | b.example    | a.example, c.example    |
      | endpoints | a.example, b.example                               | b.example    | a.example               |
      | domains   | a.example                                          | a.example    | nothing (an empty list) |
      | manual    | code-signing (written across several lines), other | code-signing | other                   |
      | endpoints | a.example with a label note "}]", b.example        | a.example    | b.example               |

  @id:vscode.tracking-items.config-writing.007 @rule:tracking.config-writing.remove-bounded-to-entry @verified @source:extensions/vscode/src/splice.ts#removeEntry @test:extensions/vscode/src/test/edit.test.ts#a one-line config loses one entry, not the whole document @test:extensions/vscode/src/test/edit.test.ts#removal never reaches outside the array it was given
  Scenario Outline: Removing an item never removes anything outside that one entry
    Given the config is written on <layout>
    When the operator stops tracking "a.example"
    Then only "a.example" is gone
    And every other entry is still in the config

    Examples:
      | layout                                                                                          |
      | a single line, { "domains": ["a.example", "b.example"] } (removing domain a.example)            |
      | one line per list, with endpoints, domains and manual entries (removing endpoint a.example)     |

  @id:vscode.tracking-items.config-writing.008 @rule:tracking.config-writing.remove-refuses-unknown-position @verified @source:extensions/vscode/src/splice.ts#removeEntry @test:extensions/vscode/src/test/edit.test.ts#a position naming no entry is refused rather than guessed at
  Scenario Outline: Removal is refused when the recorded position no longer names an entry
    Given the item "a.example" was recorded at a position in the config that now <mismatch>
    When the operator stops tracking "a.example"
    Then the config file is left untouched
    And a warning reads either "expiry-radar: expiry-radar.json has changed since the last collection — refresh and try again." or "expiry-radar: could not find the entry for a.example to remove."

    Examples:
      | mismatch                                   |
      | lies past the end of the file              |
      | lies outside the list the item belongs to  |
      | belongs to a list the config no longer has |
