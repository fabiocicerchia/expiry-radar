@domain:editor-integrations @area:editing-entries @from:extensions/nvim
Feature: Recording and removing entries from Neovim
  Lets an operator add or stop tracking hosts, domains and hand-declared expiries without hand-editing JSON or damaging the rest of the config.

  Background:
    Given the operator uses the expiry-radar plugin in Neovim

  @id:editor-integrations.editing-entries.001 @rule:editor.entries.add-appends-without-rewriting @verified @source:extensions/nvim/lua/expiry-radar/record.lua#M.write_entry @test:extensions/nvim/tests/edit_spec.lua#appends to a multi-line array, indented like its siblings @test:extensions/nvim/tests/edit_spec.lua#keeps a one-line array on one line @test:extensions/nvim/tests/edit_spec.lua#adds a missing key rather than replacing the object @test:extensions/nvim/tests/edit_spec.lua#turns an empty or absent config into a config @test:extensions/nvim/tests/edit_spec.lua#reports the line the entry actually landed on @test:extensions/nvim/tests/edit_spec.lua#trims, so a pasted hostname does not become a name with a space
  Scenario Outline: Recording an entry appends it and leaves the rest of the config as written
    Given expiry-radar.json already lists shop.example.com under endpoints
    When the operator records <entry> <value>
    Then <written> is appended to the "<array>" list
    And every existing entry and its layout is unchanged and the file is still valid JSON
    And the config is opened at the new entry's line and a collection starts

    Examples:
      | entry                          | value                                             | written                                                           | array     |
      | an endpoint                    | api.example.com                                   | {"host":"api.example.com"}                                        | endpoints |
      | a domain                       | example.org                                       | "example.org"                                                     | domains   |
      | something nothing can discover | acme-corp.co.uk, kind Domain, expiring 2027-03-01 | {"name":"acme-corp.co.uk","kind":"domain","expires":"2027-03-01"} | manual    |

  @id:editor-integrations.editing-entries.002 @rule:editor.entries.manual-expiry-must-be-a-real-date @verified @source:extensions/nvim/lua/expiry-radar/edit.lua#M.invalid_expires @test:extensions/nvim/tests/edit_spec.lua#accepts what the CLI accepts @test:extensions/nvim/tests/edit_spec.lua#rejects what the CLI would reject at load
  Scenario Outline: A hand-declared expiry must be a real calendar date
    Given the operator is recording something nothing can discover named code-signing of kind TLS certificate
    When the operator gives the expiry date "<expires>"
    Then <outcome>

    Examples:
      | expires                   | outcome                                                                                            |
      | 2027-03-01                | the entry is recorded                                                                              |
      | 2027-03-01T15:04:05+02:00 | the entry is recorded                                                                              |
      |                           | the operator sees the error "a date is required" and nothing is recorded                           |
      | 03/01/2027                | the operator sees the error "use YYYY-MM-DD, or a full RFC 3339 timestamp" and nothing is recorded |
      | 2027-02-31                | the operator sees the error "2027-02-31 is not a real date" and nothing is recorded                |

  @id:editor-integrations.editing-entries.003 @rule:editor.entries.only-recorded-items-removable @verified @source:extensions/nvim/lua/expiry-radar/record.lua#M.remove_item @test:extensions/nvim/tests/edit_spec.lua#maps a recorded source to its array, and discovery to none
  Scenario: Only entries recorded in the config can be removed
    Given the last collection returned only items discovered in Kubernetes and AWS
    When the operator asks to stop tracking an item
    Then the operator sees "nothing recorded to remove — every item here was discovered."
    And the config file is unchanged

  @id:editor-integrations.editing-entries.004 @rule:editor.entries.remove-takes-one-entry-only @verified @source:extensions/nvim/lua/expiry-radar/edit.lua#M.remove_entry @test:extensions/nvim/tests/edit_spec.lua#never reaches outside the array it was given @test:extensions/nvim/tests/edit_spec.lua#removes the middle of a multi-line array and leaves valid JSON @test:extensions/nvim/tests/edit_spec.lua#takes the comma before the last element, not after @test:extensions/nvim/tests/edit_spec.lua#leaves an empty array rather than a broken one @test:extensions/nvim/tests/edit_spec.lua#removes a multi-line entry whole @test:extensions/nvim/tests/edit_spec.lua#takes one entry from a one-line config, not the whole document
  Scenario: Removing an entry takes exactly that entry and nothing else
    Given the config lists endpoints a.example, domains keep.example and a manual entry keep
    When the operator stops tracking a.example
    Then a.example is gone from endpoints
    And keep.example and the manual entry keep are still there and the file is valid JSON
    And the operator sees "stopped tracking a.example" and a collection starts

  @id:editor-integrations.editing-entries.005 @rule:editor.entries.remove-refused-when-config-changed @inferred @source:extensions/nvim/lua/expiry-radar/record.lua#M.remove_item
  Scenario: Removal is refused when the config has changed since the last collection
    Given the last collection placed shop.example.com on line 3 of expiry-radar.json
    And the operator has since edited the file so line 3 no longer names shop.example.com
    When the operator stops tracking shop.example.com
    Then the operator sees the warning "expiry-radar.json has changed since the last collection — refresh and try again."
    And the config file is unchanged

  @id:editor-integrations.editing-entries.006 @rule:editor.entries.open-or-create-config @inferred @source:extensions/nvim/lua/expiry-radar/record.lua#M.open_config
  Scenario Outline: Opening the config creates a starter file when there is none
    Given the project has no expiry-radar.json
    And <example>
    When the operator opens the config
    Then expiry-radar.json is created with <content> and opened
    And the operator sees "created" followed by its path

    Examples:
      | example                                   | content                                               |
      | the project has expiry-radar.example.json | the contents of expiry-radar.example.json             |
      | the project has no example file           | an endpoint shop.example.com and a domain example.com |
