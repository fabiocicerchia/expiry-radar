@domain:editor-integrations @area:inventory-view @from:extensions/nvim
Feature: Browsing the ranked inventory in Neovim
  Lets an operator see what is expiring, how soon and how incomplete the picture is, without leaving the editor.

  Background:
    Giventhe operator uses the expiry-radar plugin in Neovim
    Andthe project has a config file expiry-radar.json

  @id:editor-integrations.inventory-view.001 @rule:editor.inventory.statusline-shows-soonest-deadline @verified @source:extensions/nvim/lua/expiry-radar/init.lua#M.statusline @test:extensions/nvim/tests/init_spec.lua#shows the soonest deadline, and says when the run lost a source @test:extensions/nvim/tests/init_spec.lua#marks a deadline inside the warning window without calling it expired @test:extensions/nvim/tests/init_spec.lua#stays neutral for a deadline outside the warning window @test:extensions/nvim/tests/init_spec.lua#says nothing came back rather than showing an empty count as fine
  Scenario Outline: The statusline shows the soonest deadline and says when the inventory is incomplete
    Giventhe last collection returned <items>
    And<failed> source(s) failed in that collection
    Whenthe operator looks at the statusline
    Thenthe statusline reads "<statusline>"

    Examples:
      | items                            | failed | statusline                          |
      | one item that expired 3 days ago | 1      | ✗ radar expired 3d ago (incomplete) |
      | one item expiring in 5 days      | 0      | ! radar 5d                          |
      | one item expiring in 40 days     | 0      | · radar 40d                         |
      | no items                         | 1      | ! radar 0 (incomplete)              |
      | no items                         | 0      | · radar 0                           |

  @id:editor-integrations.inventory-view.002 @rule:editor.inventory.failed-sources-shown-first @verified @source:extensions/nvim/lua/expiry-radar/ui.lua#M.report_lines @test:extensions/nvim/tests/ui_spec.lua#puts the failed sources above the rows that did come back @test:extensions/nvim/tests/init_spec.lua#keeps the sources that failed, so an incomplete run cannot look clean
  Scenario: Sources that failed are shown above the rows that did come back
    Giventhe last collection read shop.example.com
    Andthe aws source failed with "aws: AccessDenied"
    Whenthe operator opens the inventory report
    Thenthe report shows "⚠ 1 source(s) failed — this inventory is incomplete:" followed by "aws: AccessDenied"
    Andthat warning appears above the row for shop.example.com
    Andthe operator was notified "1 source(s) failed — this inventory is incomplete. :ExpiryRadarLog for the details."

  @id:editor-integrations.inventory-view.003 @rule:editor.inventory.order-worst-deadline-then-priority @verified @source:extensions/nvim/lua/expiry-radar/core.lua#M.compare_items @test:extensions/nvim/tests/core_spec.lua#orders worst deadline first, then by priority @test:extensions/nvim/tests/init_spec.lua#turns a partial report into rows, worst deadline first @test:extensions/nvim/tests/ui_spec.lua#groups rows under the deadline that decides what to do today
  Scenario: The inventory lists the worst deadline window first, then the highest priority
    Giventhe last collection returned calm (80 days, priority 0.9), broken (expired, priority 0.1), urgent-low (3 days, priority 0.2) and urgent-high (9 days, priority 0.8)
    Whenthe operator opens the inventory
    Thenthe items appear in the order broken, urgent-high, urgent-low, calm
    Andthey are grouped under "Expired", "Within 14 days" and "Further out"

  @id:editor-integrations.inventory-view.004 @rule:editor.inventory.diagnostics-only-on-declared-entries @verified @source:extensions/nvim/lua/expiry-radar/ui.lua#M.publish @test:extensions/nvim/tests/ui_spec.lua#maps the deadline onto a severity, and publishes nothing beyond the window @test:extensions/nvim/tests/ui_spec.lua#gives no squiggle to an item nothing in the repository declared @test:extensions/nvim/tests/ui_spec.lua#places the squiggle on the declared line and column @test:extensions/nvim/tests/ui_spec.lua#says what expires, when, and why it is ranked where it is @test:extensions/nvim/tests/ui_spec.lua#says how long an expired item has been broken
  Scenario Outline: Only entries written in the config file get a squiggle, and only inside the deadline windows
    Giventhe config file is open in the editor
    Andan item expiring in <days> days <declared> in the config file
    Whena collection finishes
    Thenthat config line shows <squiggle>
    Anda squiggle's message says what expires, when, and why it is ranked there

    Examples:
      | days | declared                                                   | squiggle            |
      | -1   | is declared                                                | an error            |
      | 5    | is declared                                                | a warning           |
      | 20   | is declared                                                | an information mark |
      | 90   | is declared                                                | nothing             |
      | -1   | was discovered on a Kubernetes Ingress and is not declared | nothing             |

  @id:editor-integrations.inventory-view.005 @rule:editor.inventory.quickfix-jumps-to-declared-entries @verified @source:extensions/nvim/lua/expiry-radar/ui.lua#M.to_quickfix @test:extensions/nvim/tests/ui_spec.lua#sends declared items to their line, and keeps the rest as text @test:extensions/nvim/tests/ui_spec.lua#marks what is expired or nearly so as an error
  Scenario: The quickfix list jumps to declared entries and keeps discovered ones as text
    Giventhe last collection returned shop.example.com expiring in 5 days, declared on line 3 of the config
    Andprod/web-tls discovered on a Kubernetes Ingress, expiring in 90 days
    Whenthe operator lists every item
    Thenthe quickfix list has 2 entries
    Andthe shop.example.com entry jumps to line 3 of the config and is marked as an error
    Andthe prod/web-tls entry has no location, carries its name, source and reason as text, and is marked as a warning

  @id:editor-integrations.inventory-view.006 @rule:editor.inventory.filter-offers-only-present-windows-and-kinds @verified @source:extensions/nvim/lua/expiry-radar/view.lua#M.filter @test:extensions/nvim/tests/init_spec.lua#offers only the severities and kinds the inventory actually has @test:extensions/nvim/tests/init_spec.lua#sends only the chosen severity to the quickfix list
  Scenario: Filtering offers only the deadline windows and kinds the inventory actually has
    Giventhe last collection returned one expired TLS certificate and one secret expiring in 200 days
    Whenthe operator filters the inventory and picks "Expired"
    Thenthe choices offered are Expired (1), Further out (1), TLS certificate (1) and Secret (1)
    Andthe quickfix list titled "expiry-radar: expired" holds only the expired certificate

  @id:editor-integrations.inventory-view.007 @rule:editor.inventory.hover-no-match-is-not-proof @verified @source:extensions/nvim/lua/expiry-radar/ui.lua#M.hover_lines @test:extensions/nvim/tests/ui_spec.lua#says whether anything actually looked, when nothing matched
  Scenario: Looking up a host with no match says it is not proof when sources failed
    Giventhe cursor is on a line naming shop.example.com
    Andnothing in the last collection matches shop.example.com
    Andthe aws source failed with "aws: AccessDenied"
    Whenthe operator asks what the inventory knows about the host under the cursor
    Thena window titled shop.example.com says "Nothing in the inventory matches this."
    Andit says "⚠ 1 source(s) failed, so this is not proof of anything:" followed by "aws: AccessDenied"
