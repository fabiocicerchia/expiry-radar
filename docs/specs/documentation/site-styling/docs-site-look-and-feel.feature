@domain:documentation @area:site-styling @from:stylesheets/extra
Feature: Branding the documentation site
  Gives readers of the published expiry-radar docs and splash page one consistent, self-hosted look that marks the project as part of the security family of tools.

  Background:
    Given the published documentation site and its splash page

  @id:docs.documentation.site-styling.001 @rule:docs.styling.security-palette-shared @inferred @source:docs/stylesheets/extra.css#:root @questionable
  Scenario Outline: Docs and splash page share the orange-red security accent palette
    Given the reader views <page> in the <scheme> colour scheme
    When the page renders
    Then primary buttons and the home hero title show an orange-to-red gradient from #fb923c to #ef4444
    And links appear in <link_colour>

    Examples:
      | page              | scheme | link_colour    |
      | the rendered docs | light  | red #ef4444    |
      | the rendered docs | dark   | orange #fb923c |
      | the splash page   | light  | red #ef4444    |

  @id:docs.documentation.site-styling.002 @rule:docs.styling.no-external-requests @inferred @source:docs/stylesheets/extra.css#:root
  Scenario: The docs site loads no web fonts or third-party assets
    Given the reader has no network access beyond the docs host
    When the reader opens any docs page
    Then text renders in the reader's system font stack
    And code renders in the system monospace stack
    And no request goes to a font CDN or analytics service

  @id:docs.documentation.site-styling.003 @rule:docs.styling.no-placeholder-logo @inferred @source:docs/stylesheets/extra.css#.md-header__button.md-logo
  Scenario: The docs header shows no stand-in logo
    Given the project has no logo configured
    When the reader views the header or the navigation title
    Then no logo icon appears next to the site title
