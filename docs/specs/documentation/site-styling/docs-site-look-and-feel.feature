@domain:documentation @area:site-styling @from:stylesheets/extra
Feature: Branding the documentation site
  Gives readers of the published expiry-radar docs and splash page one consistent, self-hosted look that marks the project as part of the security family of tools.

  Background:
    Given the published documentation site and its splash page

  @id:docs.documentation.site-styling.002 @rule:docs.styling.no-external-requests @inferred @source:docs/stylesheets/extra.css#:root
  Scenario: The docs site loads no web fonts or third-party assets
    Given the reader has no network access beyond the docs host
    When the reader opens any docs page
    Then text renders in the reader's system font stack
    And code renders in the system monospace stack
    And no request goes to a font CDN or analytics service
