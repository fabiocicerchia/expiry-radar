@domain:reporting @area:calendar-feed @from:output/ical
Feature: Publishing expiries to the team calendar
  Gives operators an iCalendar file of every expiring item so renewals show up in the team calendar, with earlier reminders for whatever has the largest blast radius.

  Background:
    Given the inventory has been scanned and ranked by blast radius

  @id:reporting.calendar-feed.001 @rule:reporting.calendar-feed.one-all-day-event-per-item @verified @source:internal/output/ical.go#renderICal @test:internal/output/output_test.go#TestICalIsWellFormed
  Scenario Outline: Every expiring item becomes an all-day event on its expiry date
    Given a <kind> "<name>" from source "<source>" expires at <expires>
    And it has blast radius <blast> because "<why>" and priority <priority>
    When the operator exports the inventory as an iCalendar feed
    Then the feed is a published calendar named "Expiry radar" with one event per item
    And the event is an all-day event on <day> ending on <next_day>
    And the event title reads "<kind> expires: <name>" and its category is "<category>"
    And the event description shows "blast radius <blast> (<why>)", "source <source>" and "priority <priority>" on separate lines

    Examples:
      | kind     | name                          | source      | expires           | blast | why                                        | priority | day        | next_day   | category |
      | tls_cert | payments/checkout.example.com | k8s:secret  | 2026-08-13T12:00Z | 0.90  | base tls_cert, internet-facing, production | 0.88     | 2026-08-13 | 2026-08-14 | TLS_CERT |
      | domain   | example.com                   | domain:rdap | 2027-02-17T12:00Z | 0.85  | base domain                                | 0.38     | 2027-02-17 | 2027-02-18 | DOMAIN   |

  @id:reporting.calendar-feed.002 @rule:reporting.calendar-feed.alarm-lead-scales-with-blast-radius @verified @source:internal/output/ical.go#alarm @test:internal/output/output_test.go#TestICalIsWellFormed
  Scenario Outline: Items with a larger blast radius are reminded about earlier
    Given an item "payments/checkout.example.com" has blast radius <blast>
    When the operator exports the inventory as an iCalendar feed
    Then its event carries a reminder <lead> before the expiry day
    And the reminder reads "Renew payments/checkout.example.com"

    Examples:
      | blast | lead    |
      | 0.90  | 30 days |
      | 0.80  | 30 days |
      | 0.70  | 14 days |
      | 0.60  | 14 days |
      | 0.50  | 7 days  |

  @id:reporting.calendar-feed.003 @rule:reporting.calendar-feed.event-identity-stable-across-refreshes @verified @source:internal/output/ical.go#uid @test:internal/output/output_test.go#TestICalUIDIsStableAcrossRuns
  Scenario: Refreshing the feed updates existing events instead of duplicating them
    Given the team calendar is subscribed to a feed exported yesterday
    And the tls_cert "payments/checkout.example.com" from source "k8s:secret" still expires on 2026-08-13
    When the operator exports the inventory as an iCalendar feed again
    Then the item's event keeps the identifier "tls-cert-k8s-secret-payments-checkout-example-com-20260813@expiry-radar"
    And the calendar shows one event for the item, not two

  @id:reporting.calendar-feed.004 @rule:reporting.calendar-feed.distinct-items-get-distinct-events @inferred @source:internal/output/ical.go#sanitiseUID @questionable
  Scenario Outline: Items whose names differ only in punctuation collapse into one calendar event
    Given a tls_cert "<first>" and a tls_cert "<second>" both come from source "tls:endpoint"
    And both expire on 2026-08-13
    When the operator exports the inventory as an iCalendar feed
    Then both events carry the identifier "<uid>"
    And a subscribed calendar shows only one of the two items

    Examples:
      | first           | second          | uid                                                         |
      | api.example.com | api-example.com | tls-cert-tls-endpoint-api-example-com-20260813@expiry-radar |
      | payments/db     | payments_db     | tls-cert-tls-endpoint-payments-db-20260813@expiry-radar     |

  @id:reporting.calendar-feed.005 @rule:reporting.calendar-feed.text-separators-escaped @verified @source:internal/output/ical.go#escapeText @test:internal/output/output_test.go#TestICalEscapesSeparators
  Scenario Outline: Commas, semicolons, backslashes and line breaks in names do not break the event text
    Given a tls_cert is named "<name>"
    When the operator exports the inventory as an iCalendar feed
    Then the event title and reminder show the name as "<escaped>"

    Examples:
      | name         | escaped        |
      | a,b;c        | a\,b\;c        |
      | C:\certs\api | C:\\certs\\api |

  @id:reporting.calendar-feed.006 @rule:reporting.calendar-feed.lines-within-rfc-limit @verified @source:internal/output/ical.go#fold @test:internal/output/output_test.go#TestICalIsWellFormed @questionable
  Scenario: Long lines are folded so strict calendar clients accept the feed
    Given an item's event description is longer than 75 characters
    When the operator exports the inventory as an iCalendar feed
    Then no line of the feed is longer than 75 octets
    And the long text continues on lines that start with a single space
