@domain:credentials @area:code-hosting-tokens @from:source/gitlab
Feature: Tracking GitLab tokens and Pages certificates
  Gives operators one ranked view of GitLab personal, project, group and deploy tokens and Pages certificates, so the CI token that can write the API outranks a read-only registry token before a pipeline starts failing with 401.

  Background:
    Given the GitLab source is enabled

  @id:credentials.code-hosting-tokens.002 @rule:credentials.gitlab.blast-radius-from-scope @verified @source:internal/source/gitlab.go#scopeBlastRadius @test:internal/source/gitlab_test.go#TestGitLabRanksTokensByScope
  Scenario Outline: A GitLab token's blast radius comes from the broadest scope it holds
    Given a GitLab personal access token "ci-bot" expires in 20 days
    And it has the scopes <scopes>
    When the operator runs a scan
    Then the item "pat/ci-bot" is listed with blast radius <blast radius>

    Examples:
      | scopes                     | blast radius                   |
      | api                        | 0.95                           |
      | read_api, api              | 0.95                           |
      | write_repository           | 0.85                           |
      | sudo                       | 0.85                           |
      | admin_mode                 | 0.85                           |
      | read_api                   | 0.55                           |
      | read_api, write_repository | 0.85                           |
      | read_repository            | 0.40                           |
      | read_registry              | 0.40                           |
      | write_registry             | 0.40                           |
      | read_user                  | not stated (left to inference) |

  @id:credentials.code-hosting-tokens.003 @rule:credentials.gitlab.pages-auto-ssl-is-managed @verified @source:internal/source/gitlab.go#pagesItem @test:internal/source/gitlab_test.go#TestGitLabPagesAutoSSLIsDeRanked
  Scenario Outline: A GitLab Pages certificate renewed by GitLab is marked as managed, an uploaded one is not
    Given the project "acme/site" is configured
    And its Pages domain <domain> has a certificate expiring in 20 days
    And automatic certificates are <auto ssl> for that domain
    When the operator runs a scan
    Then a public TLS certificate item "<domain>" is listed for "acme/site"
    And its renewal is <renewal>

    Examples:
      | domain             | auto ssl | renewal               |
      | auto.example.com   | enabled  | managed               |
      | byhand.example.com | disabled | not marked as managed |

  @id:credentials.code-hosting-tokens.004 @rule:credentials.gitlab.denial-explains-fix @verified @source:internal/source/gitlab.go#glGet @test:internal/source/gitlab_test.go#TestGitLabOneUnreadableProjectKeepsTheOthers
  Scenario Outline: A GitLab denial tells the operator what access the token is missing
    Given the project "acme/payments" is configured
    And GitLab answers <response> when its access tokens are read
    When the operator runs a scan
    Then the scan warns "<warning>" for "acme/payments"

    Examples:
      | response         | warning                                                           |
      | 401 Unauthorized | the token needs read_api, and owner or maintainer on the resource |
      | 403 Forbidden    | the token needs read_api, and owner or maintainer on the resource |
      | 404 Not Found    | not found, or not visible to this token                           |
