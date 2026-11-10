@domain:prioritisation @area:risk-scoring @from:rank/rank
Feature: Ranking expiring items by blast radius
  Gives operators one list ordered by what an expiry would actually break, so the certificate on the payment path is dealt with before the one on a staging dashboard.

  Background:
    Given the inventory is ranked as of 2026-08-01 12:00 UTC
    And no blast-radius overrides are configured unless a scenario says so

  @id:prioritisation.risk-scoring.001 @rule:prioritisation.ranking.time-left-weighed-against-blast-radius @verified @source:internal/rank/rank.go#Rank @test:internal/rank/rank_test.go#TestRankingBeatsSortingByDate @test:internal/rank/rank_test.go#TestTwoDaysLeftOutranksAModestlyBiggerBlastRadiusEightDaysOut
  Scenario Outline: Time left and blast radius are weighed together, not sorted by date
    Given a <first_kind> "<first>" with <first_evidence> expiring in <first_days> days
    And a <second_kind> "<second>" with <second_evidence> expiring in <second_days> days
    When the operator ranks the inventory
    Then "<leader>" is listed first

    Examples:
      | first_kind      | first             | first_evidence         | first_days | second_kind     | second          | second_evidence                            | second_days | leader          |
      | TLS certificate | staging-dashboard | environment staging    | 20         | TLS certificate | checkout        | public exposure and 2000 rps of traffic    | 45          | checkout        |
      | IAM access key  | production-worker | environment production | 8          | IAM access key  | mentor-platform | no labels                                  | 2           | mentor-platform |
      | TLS certificate | staging-dashboard | environment staging    | 4          | TLS certificate | checkout        | environment production and public exposure | 6           | checkout        |

  @id:prioritisation.risk-scoring.002 @rule:prioritisation.ranking.expired-items-lead @verified @source:internal/rank/rank.go#Rank @test:internal/rank/rank_test.go#TestExpiredSortsFirst
  Scenario: An already-expired item outranks a far-off item with a bigger blast radius
    Given a domain "example.com" expiring in 200 days
    And a secret "staging/old-token" in environment staging that expired 3 days ago
    When the operator ranks the inventory
    Then "staging/old-token" is listed first
    And "staging/old-token" shows a negative number of days left

  @id:prioritisation.risk-scoring.003 @rule:prioritisation.ranking.explicit-blast-radius-beats-inference @verified @source:internal/rank/rank.go#blastRadius @test:internal/rank/rank_test.go#TestOverrideWins @test:internal/rank/rank_test.go#TestResourceLabelBeatsInference
  Scenario Outline: An operator override, then a blast-radius label on the resource, replaces inferred blast radius
    Given a TLS certificate "<name>" in namespace "<namespace>" expiring in 30 days
    And <explicit_setting>
    When the operator ranks the inventory
    Then its blast radius is <blast>
    And its explanation reads "<why>"

    Examples:
      | name           | namespace | explicit_setting                                                                                                    | blast | why                      |
      | internal-thing | payments  | an override matching "payments*" with blast radius 1                                                                | 1.00  | override payments*       |
      | thing          | dev       | the resource label expiry-radar/blast-radius set to 0.95                                                            | 0.95  | labelled on the resource |
      | thing          | payments  | an override matching "payments*" with blast radius 0.3 and the resource label expiry-radar/blast-radius set to 0.95 | 0.30  | override payments*       |

  @id:prioritisation.risk-scoring.004 @rule:prioritisation.overrides.invalid-override-rejected-at-load @verified @source:internal/rank/rank.go#ValidateOverrides @test:internal/rank/rank_test.go#TestValidateOverridesRejectsPatternsThatCouldNeverMatch @test:internal/rank/rank_test.go#TestAMalformedGlobSilentlyMatchesNothing
  Scenario Outline: A blast-radius override that could never apply is refused when the configuration loads
    Given the configuration has an override <override>
    When the operator runs a scan with that configuration
    Then the configuration is refused with "<message>"

    Examples:
      | override                                 | message                                           |
      | with no match pattern                    | override 0 has no match pattern                   |
      | matching "[unclosed" with blast radius 1 | override 0 ("[unclosed"): syntax error in pattern |
      | matching "ok" with blast radius 2        | override 0 ("ok"): blastRadius 2 is outside 0..1  |
      | matching "ok" with blast radius -1       | override 0 ("ok"): blastRadius -1 is outside 0..1 |

  @id:prioritisation.risk-scoring.005 @rule:prioritisation.inference.evidence-moves-blast-radius-and-is-explained @verified @source:internal/rank/rank.go#blastRadius @test:internal/rank/rank_test.go#TestInference
  Scenario Outline: Each piece of evidence about a certificate moves its blast radius and is named in the explanation
    Given a TLS certificate "<name>" expiring in 30 days with <evidence>
    When the operator ranks the inventory
    Then its blast radius is <blast>
    And its explanation reads "<why>"

    Examples:
      | name            | evidence                                                                        | blast | why                                                  |
      | app             | no labels                                                                       | 0.50  | base tls_cert                                        |
      | app             | public exposure                                                                 | 0.70  | base tls_cert, internet-facing                       |
      | app             | ingress class nginx-internal                                                    | 0.35  | base tls_cert, internal ingress class                |
      | app             | environment staging                                                             | 0.20  | base tls_cert, non-production                        |
      | app             | environment production                                                          | 0.70  | base tls_cert, production                            |
      | wild            | hosts *.example.com, a.example.com, b.example.com, c.example.com, d.example.com | 0.65  | base tls_cert, wildcard certificate, 5 hosts covered |
      | app             | 1000 rps of traffic                                                             | 0.80  | base tls_cert, traffic 1000 rps                      |
      | old.example.com | in-use false                                                                    | 0.15  | base tls_cert, not in use                            |

  @id:prioritisation.risk-scoring.006 @rule:prioritisation.inference.trust-anchor-outranks-leaf @verified @source:internal/rank/rank.go#baseByKind @test:internal/rank/rank_test.go#TestATrustAnchorOutranksALeafAtTheSameDeadline
  Scenario: A cluster trust anchor outranks a public production certificate expiring the same day
    Given a public TLS certificate "prod/shop-tls" expiring in 20 days
    And a trust anchor "validatingwebhookconfiguration/cert-manager" expiring in 20 days
    When the operator ranks the inventory
    Then the trust anchor is listed first with blast radius 0.95
    And "prod/shop-tls" has blast radius 0.90

  @id:prioritisation.risk-scoring.007 @rule:prioritisation.inference.environment-read-from-whole-words @verified @source:internal/rank/rank.go#environment @test:internal/rank/rank_test.go#TestEnvironmentMatchesTokensNotSubstrings @test:internal/rank/rank_test.go#TestATrustAnchorIsStillMovedByEnvironment
  Scenario Outline: Environment is inferred from whole words in names, and non-production wins a tie
    Given a TLS certificate named "<name>" in namespace "<namespace>" expiring in 30 days
    When the operator ranks the inventory
    Then its blast radius is <blast>
    And its explanation reads "<why>"

    Examples:
      | name                 | namespace    | blast | why                           |
      | device-registry      | iot          | 0.50  | base tls_cert                 |
      | reproduction-service | apps         | 0.50  | base tls_cert                 |
      | api                  | prod         | 0.70  | base tls_cert, production     |
      | api                  | team-staging | 0.20  | base tls_cert, non-production |
      | prod-mirror          | staging      | 0.20  | base tls_cert, non-production |

  @id:prioritisation.risk-scoring.008 @rule:prioritisation.ranking.ties-list-sooner-expiry-first @verified @source:internal/rank/rank.go#Rank @test:internal/rank/rank_test.go#TestTiesBreakOnTheSoonerExpiry
  Scenario: Items with equal priority are listed soonest expiry first
    Given a TLS certificate "later" with no labels expiring in 120 days
    And a TLS certificate "sooner" with no labels expiring in 100 days
    When the operator ranks the inventory
    Then both have the same priority
    And "sooner" is listed before "later"
