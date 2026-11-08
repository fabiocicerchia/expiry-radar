@domain:scanning @area:collection @from:source/source
Feature: Collecting every source under one deadline
  Operators give a scan a single timeout and need every source to get a fair share of it, with a report whose order does not change between identical runs.

  Background:
    Given the operator has configured several sources

  @id:scanning.collection.001 @rule:scanning.collection.sources-share-one-deadline @verified @source:internal/source/source.go#CollectAll @test:internal/source/collect_test.go#TestCollectAllRunsSourcesConcurrently
  Scenario: All sources run at the same time, so a slow source cannot use up the others' time
    Given four sources each wait until all four have started before answering
    And the run has a single timeout
    When the operator runs a scan
    Then all four sources start before any finishes
    And the inventory contains one item from each of the four sources

  @id:scanning.collection.002 @rule:scanning.collection.configured-order-is-stable @verified @source:internal/source/source.go#CollectAll @test:internal/source/collect_test.go#TestCollectAllKeepsTheConfiguredOrderUnderConcurrency
  Scenario: Findings come out in the configured source order, whichever source answers first
    Given source "slow" is configured before source "fast"
    And "slow" takes longer to answer than "fast"
    When the operator runs a scan
    Then the item from "slow" is listed before the item from "fast"
    And two runs of the same configuration list tied items in the same order
