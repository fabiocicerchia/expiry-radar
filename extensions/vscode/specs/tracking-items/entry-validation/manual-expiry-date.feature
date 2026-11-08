@domain:tracking-items @area:entry-validation @from:src/prompts
Feature: Validating a recorded expiry date
  Keeps the operator from recording an expiry date the CLI would refuse to load.

  @id:vscode.tracking-items.entry-validation.006 @rule:tracking-items.entry-validation.expiry-cli-accepts @inferred @source:extensions/vscode/src/edit.ts#invalidExpires
  Scenario Outline: Only a real date in YYYY-MM-DD or RFC 3339 form is accepted as an expiry
    Given the operator is recording something nothing can discover
    When the operator enters "<expires>" as its expiry date
    Then the date prompt shows "<message>"

    Examples:
      | expires              | message                                       |
      |                      | A date is required.                           |
      | 03/01/2027           | Use YYYY-MM-DD, or a full RFC 3339 timestamp. |
      | 2027-02-31           | 2027-02-31 is not a real date.                |
      | 2027-03-01           | (accepted, no message)                        |
      | 2027-03-01T09:00:00Z | (accepted, no message)                        |
