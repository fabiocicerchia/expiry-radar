# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.11.0](https://github.com/fabiocicerchia/expiry-radar/compare/v1.10.0...v1.11.0) (2026-11-13)


### Features

* **aws:** -verify-aws, and make the degradation rule testable at last ([#38](https://github.com/fabiocicerchia/expiry-radar/issues/38)) ([8bce5d8](https://github.com/fabiocicerchia/expiry-radar/commit/8bce5d8507834141f0d9febf344b13abf17f92c5))
* **ci:** let the release publish the extension ([#34](https://github.com/fabiocicerchia/expiry-radar/issues/34)) ([1dd70e6](https://github.com/fabiocicerchia/expiry-radar/commit/1dd70e663cfcdf6876629b79cc08b2937d207349))
* **cloudflare:** read the account's own API tokens, and the ones that never expire ([#76](https://github.com/fabiocicerchia/expiry-radar/issues/76)) ([18cd42a](https://github.com/fabiocicerchia/expiry-radar/commit/18cd42aa2017b4d507dd449467fa76b3e11ea206))
* **docs:** build the docs site in Actions and drop Read the Docs ([#24](https://github.com/fabiocicerchia/expiry-radar/issues/24)) ([9e32012](https://github.com/fabiocicerchia/expiry-radar/commit/9e3201228d683fe6df895316b0bea6bcf552b8c0))
* **domain:** fall back to WHOIS when a TLD has no RDAP service ([9e0834a](https://github.com/fabiocicerchia/expiry-radar/commit/9e0834ae9c54706ee56bfa906b9ddb3d08625036))
* **k8s:** report cluster trust anchors and cert-manager renewal health ([#66](https://github.com/fabiocicerchia/expiry-radar/issues/66)) ([af42ec6](https://github.com/fabiocicerchia/expiry-radar/commit/af42ec6697e81241dd3831df6b39ebaeb6716727))
* **manual:** record a credential that cannot expire without inventing a date ([#78](https://github.com/fabiocicerchia/expiry-radar/issues/78)) ([28f814e](https://github.com/fabiocicerchia/expiry-radar/commit/28f814ed28db1dcc65702801a65b60bb35b150c7))
* **output:** add an HTML report with stats, grouping and filtering ([8f36956](https://github.com/fabiocicerchia/expiry-radar/commit/8f3695607b42d12d56903e1b1f496c623595a1f7))
* **packaging:** man page, OS packages and a staged install ([#51](https://github.com/fabiocicerchia/expiry-radar/issues/51)) ([97a53a3](https://github.com/fabiocicerchia/expiry-radar/commit/97a53a3d46a7a06b368ec91792afdddd1f10c08b))
* **vscode:** swap the marketplace icon for an hourglass ([#36](https://github.com/fabiocicerchia/expiry-radar/issues/36)) ([adf79e2](https://github.com/fabiocicerchia/expiry-radar/commit/adf79e2cd166cf94f7b1a2ce7f2529586f5ea024))


### Bug Fixes

* 2 issues found by code-atlas (end-of-life runtimes) ([#105](https://github.com/fabiocicerchia/expiry-radar/issues/105)) ([0806c81](https://github.com/fabiocicerchia/expiry-radar/commit/0806c8114df1b269911d7edf0c0a6efd4d4091b9))
* 2 issues found by code-atlas (silenced checks) ([#114](https://github.com/fabiocicerchia/expiry-radar/issues/114)) ([3166ab7](https://github.com/fabiocicerchia/expiry-radar/commit/3166ab7ec54f8de59c0cde3101fbfba67ea22044))
* 3 issues found by code-atlas (ci that cannot fail) ([#107](https://github.com/fabiocicerchia/expiry-radar/issues/107)) ([4fda036](https://github.com/fabiocicerchia/expiry-radar/commit/4fda036cff196469392e72c94b67cae376290b67))
* 5 issues found by code-atlas (vulnerabilities) ([#101](https://github.com/fabiocicerchia/expiry-radar/issues/101)) ([e1772c0](https://github.com/fabiocicerchia/expiry-radar/commit/e1772c0ff658bc35c3041397f5518359fc77ec1c))
* 6 issues found by code-atlas (code design) ([#109](https://github.com/fabiocicerchia/expiry-radar/issues/109)) ([8f5b367](https://github.com/fabiocicerchia/expiry-radar/commit/8f5b367cd5b0369b2d188365893ea89cf9a8fb09))
* 6 issues found by code-atlas (quality gates) ([#104](https://github.com/fabiocicerchia/expiry-radar/issues/104)) ([1e12320](https://github.com/fabiocicerchia/expiry-radar/commit/1e123205c1a224d8e056d2a2227a2e65e49270d2))
* 8 issues found by code-atlas (quality gates) ([#102](https://github.com/fabiocicerchia/expiry-radar/issues/102)) ([6bfca03](https://github.com/fabiocicerchia/expiry-radar/commit/6bfca035b25776a2da479df9d1de74fc954d5a54))
* 9 issues found by code-atlas (test suites) ([#116](https://github.com/fabiocicerchia/expiry-radar/issues/116)) ([c974233](https://github.com/fabiocicerchia/expiry-radar/commit/c974233b7bc9d74462e2a344a8c567bb199d5f7b))
* **ci:** keep actions: read on the job that uploads sarif ([#72](https://github.com/fabiocicerchia/expiry-radar/issues/72)) ([ce11a6c](https://github.com/fabiocicerchia/expiry-radar/commit/ce11a6cb7068693561fe488668f886f738aad8c7))
* **ci:** pin the editorconfig-checker binary version ([#40](https://github.com/fabiocicerchia/expiry-radar/issues/40)) ([cb741f4](https://github.com/fabiocicerchia/expiry-radar/commit/cb741f43e990c6683b03fcea2c54d8bea1baf8d9))
* **ci:** stop security workflows failing on private repos ([#3](https://github.com/fabiocicerchia/expiry-radar/issues/3)) ([59942f0](https://github.com/fabiocicerchia/expiry-radar/commit/59942f02b45ad172222713505675324ae69a6967))
* **deps:** bump Go toolchain to go1.26.10 ([#91](https://github.com/fabiocicerchia/expiry-radar/issues/91)) ([ff9e352](https://github.com/fabiocicerchia/expiry-radar/commit/ff9e3521c3402b2a44e3c3a3021139ee69597082))
* kics warns in the repository ([#111](https://github.com/fabiocicerchia/expiry-radar/issues/111)) ([fe13b42](https://github.com/fabiocicerchia/expiry-radar/commit/fe13b42dd4c0ee532117e195d6080e4b21c30d6d))
* **output:** stop html/template turning a CSS comment into a space ([#73](https://github.com/fabiocicerchia/expiry-radar/issues/73)) ([3ed06e1](https://github.com/fabiocicerchia/expiry-radar/commit/3ed06e10bd8ab7739c346833bf90a712e18afba3))
* **pre-commit:** stop check-yaml failing on Helm templates and multi-doc manifests ([4a634bd](https://github.com/fabiocicerchia/expiry-radar/commit/4a634bdbe63a2075d6fa8e0c28137d027f065ec7))
* **rank:** make urgency steep near the deadline, not linear over 90 days ([#81](https://github.com/fabiocicerchia/expiry-radar/issues/81)) ([d421a2c](https://github.com/fabiocicerchia/expiry-radar/commit/d421a2c59de698fcd565b9a78472bbbb654d07b2))
* **release:** actually publish the Homebrew cask ([#56](https://github.com/fabiocicerchia/expiry-radar/issues/56)) ([d739d26](https://github.com/fabiocicerchia/expiry-radar/commit/d739d26ec04782086f72d6c5f409e6d187dff904))
* **release:** let the release PR carry a token that isn't GITHUB_TOKEN ([#58](https://github.com/fabiocicerchia/expiry-radar/issues/58)) ([786b743](https://github.com/fabiocicerchia/expiry-radar/commit/786b7431dc974c5b743fe4a619b07fa681814ead))
* **release:** sign checksums with a Sigstore bundle ([#53](https://github.com/fabiocicerchia/expiry-radar/issues/53)) ([298bb15](https://github.com/fabiocicerchia/expiry-radar/commit/298bb15aed7f8372a36f7f28075e6f2408b32daf))
* security and code-quality findings ([#14](https://github.com/fabiocicerchia/expiry-radar/issues/14)) ([83da568](https://github.com/fabiocicerchia/expiry-radar/commit/83da56859049cd9054cf81217694b307ee98e682))
* spell "unparsable" the way the typos linter expects ([35f1bcd](https://github.com/fabiocicerchia/expiry-radar/commit/35f1bcddb5b9faeaf105ff1f7bd851005378ec75))
* unblock quality and clear the Scorecard pinned-dependencies finding ([#26](https://github.com/fabiocicerchia/expiry-radar/issues/26)) ([fb67097](https://github.com/fabiocicerchia/expiry-radar/commit/fb67097d3dd8981bd1ce647dfdd7c706e19195ca))

## [1.10.0](https://github.com/fabiocicerchia/expiry-radar/compare/v1.9.0...v1.10.0) (2026-10-31)


### Features

* **aws:** -verify-aws, and make the degradation rule testable at last ([#38](https://github.com/fabiocicerchia/expiry-radar/issues/38)) ([8bce5d8](https://github.com/fabiocicerchia/expiry-radar/commit/8bce5d8507834141f0d9febf344b13abf17f92c5))
* **ci:** let the release publish the extension ([#34](https://github.com/fabiocicerchia/expiry-radar/issues/34)) ([1dd70e6](https://github.com/fabiocicerchia/expiry-radar/commit/1dd70e663cfcdf6876629b79cc08b2937d207349))
* **cloudflare:** read the account's own API tokens, and the ones that never expire ([#76](https://github.com/fabiocicerchia/expiry-radar/issues/76)) ([18cd42a](https://github.com/fabiocicerchia/expiry-radar/commit/18cd42aa2017b4d507dd449467fa76b3e11ea206))
* **docs:** build the docs site in Actions and drop Read the Docs ([#24](https://github.com/fabiocicerchia/expiry-radar/issues/24)) ([9e32012](https://github.com/fabiocicerchia/expiry-radar/commit/9e3201228d683fe6df895316b0bea6bcf552b8c0))
* **domain:** fall back to WHOIS when a TLD has no RDAP service ([9e0834a](https://github.com/fabiocicerchia/expiry-radar/commit/9e0834ae9c54706ee56bfa906b9ddb3d08625036))
* **k8s:** report cluster trust anchors and cert-manager renewal health ([#66](https://github.com/fabiocicerchia/expiry-radar/issues/66)) ([af42ec6](https://github.com/fabiocicerchia/expiry-radar/commit/af42ec6697e81241dd3831df6b39ebaeb6716727))
* **manual:** record a credential that cannot expire without inventing a date ([#78](https://github.com/fabiocicerchia/expiry-radar/issues/78)) ([28f814e](https://github.com/fabiocicerchia/expiry-radar/commit/28f814ed28db1dcc65702801a65b60bb35b150c7))
* **output:** add an HTML report with stats, grouping and filtering ([8f36956](https://github.com/fabiocicerchia/expiry-radar/commit/8f3695607b42d12d56903e1b1f496c623595a1f7))
* **packaging:** man page, OS packages and a staged install ([#51](https://github.com/fabiocicerchia/expiry-radar/issues/51)) ([97a53a3](https://github.com/fabiocicerchia/expiry-radar/commit/97a53a3d46a7a06b368ec91792afdddd1f10c08b))
* **vscode:** swap the marketplace icon for an hourglass ([#36](https://github.com/fabiocicerchia/expiry-radar/issues/36)) ([adf79e2](https://github.com/fabiocicerchia/expiry-radar/commit/adf79e2cd166cf94f7b1a2ce7f2529586f5ea024))


### Bug Fixes

* **ci:** keep actions: read on the job that uploads sarif ([#72](https://github.com/fabiocicerchia/expiry-radar/issues/72)) ([ce11a6c](https://github.com/fabiocicerchia/expiry-radar/commit/ce11a6cb7068693561fe488668f886f738aad8c7))
* **ci:** pin the editorconfig-checker binary version ([#40](https://github.com/fabiocicerchia/expiry-radar/issues/40)) ([cb741f4](https://github.com/fabiocicerchia/expiry-radar/commit/cb741f43e990c6683b03fcea2c54d8bea1baf8d9))
* **ci:** stop security workflows failing on private repos ([#3](https://github.com/fabiocicerchia/expiry-radar/issues/3)) ([59942f0](https://github.com/fabiocicerchia/expiry-radar/commit/59942f02b45ad172222713505675324ae69a6967))
* **deps:** bump Go toolchain to go1.26.10 ([#91](https://github.com/fabiocicerchia/expiry-radar/issues/91)) ([ff9e352](https://github.com/fabiocicerchia/expiry-radar/commit/ff9e3521c3402b2a44e3c3a3021139ee69597082))
* **output:** stop html/template turning a CSS comment into a space ([#73](https://github.com/fabiocicerchia/expiry-radar/issues/73)) ([3ed06e1](https://github.com/fabiocicerchia/expiry-radar/commit/3ed06e10bd8ab7739c346833bf90a712e18afba3))
* **pre-commit:** stop check-yaml failing on Helm templates and multi-doc manifests ([4a634bd](https://github.com/fabiocicerchia/expiry-radar/commit/4a634bdbe63a2075d6fa8e0c28137d027f065ec7))
* **rank:** make urgency steep near the deadline, not linear over 90 days ([#81](https://github.com/fabiocicerchia/expiry-radar/issues/81)) ([d421a2c](https://github.com/fabiocicerchia/expiry-radar/commit/d421a2c59de698fcd565b9a78472bbbb654d07b2))
* **release:** actually publish the Homebrew cask ([#56](https://github.com/fabiocicerchia/expiry-radar/issues/56)) ([d739d26](https://github.com/fabiocicerchia/expiry-radar/commit/d739d26ec04782086f72d6c5f409e6d187dff904))
* **release:** let the release PR carry a token that isn't GITHUB_TOKEN ([#58](https://github.com/fabiocicerchia/expiry-radar/issues/58)) ([786b743](https://github.com/fabiocicerchia/expiry-radar/commit/786b7431dc974c5b743fe4a619b07fa681814ead))
* **release:** sign checksums with a Sigstore bundle ([#53](https://github.com/fabiocicerchia/expiry-radar/issues/53)) ([298bb15](https://github.com/fabiocicerchia/expiry-radar/commit/298bb15aed7f8372a36f7f28075e6f2408b32daf))
* security and code-quality findings ([#14](https://github.com/fabiocicerchia/expiry-radar/issues/14)) ([83da568](https://github.com/fabiocicerchia/expiry-radar/commit/83da56859049cd9054cf81217694b307ee98e682))
* spell "unparsable" the way the typos linter expects ([35f1bcd](https://github.com/fabiocicerchia/expiry-radar/commit/35f1bcddb5b9faeaf105ff1f7bd851005378ec75))
* unblock quality and clear the Scorecard pinned-dependencies finding ([#26](https://github.com/fabiocicerchia/expiry-radar/issues/26)) ([fb67097](https://github.com/fabiocicerchia/expiry-radar/commit/fb67097d3dd8981bd1ce647dfdd7c706e19195ca))

## [1.9.0](https://github.com/fabiocicerchia/expiry-radar/compare/v1.8.2...v1.9.0) (2026-10-23)


### Features

* **aws:** -verify-aws, and make the degradation rule testable at last ([#38](https://github.com/fabiocicerchia/expiry-radar/issues/38)) ([8bce5d8](https://github.com/fabiocicerchia/expiry-radar/commit/8bce5d8507834141f0d9febf344b13abf17f92c5))
* **ci:** let the release publish the extension ([#34](https://github.com/fabiocicerchia/expiry-radar/issues/34)) ([1dd70e6](https://github.com/fabiocicerchia/expiry-radar/commit/1dd70e663cfcdf6876629b79cc08b2937d207349))
* **cloudflare:** read the account's own API tokens, and the ones that never expire ([#76](https://github.com/fabiocicerchia/expiry-radar/issues/76)) ([18cd42a](https://github.com/fabiocicerchia/expiry-radar/commit/18cd42aa2017b4d507dd449467fa76b3e11ea206))
* **docs:** build the docs site in Actions and drop Read the Docs ([#24](https://github.com/fabiocicerchia/expiry-radar/issues/24)) ([9e32012](https://github.com/fabiocicerchia/expiry-radar/commit/9e3201228d683fe6df895316b0bea6bcf552b8c0))
* **domain:** fall back to WHOIS when a TLD has no RDAP service ([9e0834a](https://github.com/fabiocicerchia/expiry-radar/commit/9e0834ae9c54706ee56bfa906b9ddb3d08625036))
* **k8s:** report cluster trust anchors and cert-manager renewal health ([#66](https://github.com/fabiocicerchia/expiry-radar/issues/66)) ([af42ec6](https://github.com/fabiocicerchia/expiry-radar/commit/af42ec6697e81241dd3831df6b39ebaeb6716727))
* **manual:** record a credential that cannot expire without inventing a date ([#78](https://github.com/fabiocicerchia/expiry-radar/issues/78)) ([28f814e](https://github.com/fabiocicerchia/expiry-radar/commit/28f814ed28db1dcc65702801a65b60bb35b150c7))
* **output:** add an HTML report with stats, grouping and filtering ([8f36956](https://github.com/fabiocicerchia/expiry-radar/commit/8f3695607b42d12d56903e1b1f496c623595a1f7))
* **packaging:** man page, OS packages and a staged install ([#51](https://github.com/fabiocicerchia/expiry-radar/issues/51)) ([97a53a3](https://github.com/fabiocicerchia/expiry-radar/commit/97a53a3d46a7a06b368ec91792afdddd1f10c08b))
* **vscode:** swap the marketplace icon for an hourglass ([#36](https://github.com/fabiocicerchia/expiry-radar/issues/36)) ([adf79e2](https://github.com/fabiocicerchia/expiry-radar/commit/adf79e2cd166cf94f7b1a2ce7f2529586f5ea024))


### Bug Fixes

* **ci:** keep actions: read on the job that uploads sarif ([#72](https://github.com/fabiocicerchia/expiry-radar/issues/72)) ([ce11a6c](https://github.com/fabiocicerchia/expiry-radar/commit/ce11a6cb7068693561fe488668f886f738aad8c7))
* **ci:** pin the editorconfig-checker binary version ([#40](https://github.com/fabiocicerchia/expiry-radar/issues/40)) ([cb741f4](https://github.com/fabiocicerchia/expiry-radar/commit/cb741f43e990c6683b03fcea2c54d8bea1baf8d9))
* **ci:** stop security workflows failing on private repos ([#3](https://github.com/fabiocicerchia/expiry-radar/issues/3)) ([59942f0](https://github.com/fabiocicerchia/expiry-radar/commit/59942f02b45ad172222713505675324ae69a6967))
* **deps:** bump Go toolchain to go1.26.10 ([#91](https://github.com/fabiocicerchia/expiry-radar/issues/91)) ([ff9e352](https://github.com/fabiocicerchia/expiry-radar/commit/ff9e3521c3402b2a44e3c3a3021139ee69597082))
* **output:** stop html/template turning a CSS comment into a space ([#73](https://github.com/fabiocicerchia/expiry-radar/issues/73)) ([3ed06e1](https://github.com/fabiocicerchia/expiry-radar/commit/3ed06e10bd8ab7739c346833bf90a712e18afba3))
* **pre-commit:** stop check-yaml failing on Helm templates and multi-doc manifests ([4a634bd](https://github.com/fabiocicerchia/expiry-radar/commit/4a634bdbe63a2075d6fa8e0c28137d027f065ec7))
* **rank:** make urgency steep near the deadline, not linear over 90 days ([#81](https://github.com/fabiocicerchia/expiry-radar/issues/81)) ([d421a2c](https://github.com/fabiocicerchia/expiry-radar/commit/d421a2c59de698fcd565b9a78472bbbb654d07b2))
* **release:** actually publish the Homebrew cask ([#56](https://github.com/fabiocicerchia/expiry-radar/issues/56)) ([d739d26](https://github.com/fabiocicerchia/expiry-radar/commit/d739d26ec04782086f72d6c5f409e6d187dff904))
* **release:** let the release PR carry a token that isn't GITHUB_TOKEN ([#58](https://github.com/fabiocicerchia/expiry-radar/issues/58)) ([786b743](https://github.com/fabiocicerchia/expiry-radar/commit/786b7431dc974c5b743fe4a619b07fa681814ead))
* **release:** sign checksums with a Sigstore bundle ([#53](https://github.com/fabiocicerchia/expiry-radar/issues/53)) ([298bb15](https://github.com/fabiocicerchia/expiry-radar/commit/298bb15aed7f8372a36f7f28075e6f2408b32daf))
* security and code-quality findings ([#14](https://github.com/fabiocicerchia/expiry-radar/issues/14)) ([83da568](https://github.com/fabiocicerchia/expiry-radar/commit/83da56859049cd9054cf81217694b307ee98e682))
* spell "unparsable" the way the typos linter expects ([35f1bcd](https://github.com/fabiocicerchia/expiry-radar/commit/35f1bcddb5b9faeaf105ff1f7bd851005378ec75))
* unblock quality and clear the Scorecard pinned-dependencies finding ([#26](https://github.com/fabiocicerchia/expiry-radar/issues/26)) ([fb67097](https://github.com/fabiocicerchia/expiry-radar/commit/fb67097d3dd8981bd1ce647dfdd7c706e19195ca))

## [1.8.2](https://github.com/fabiocicerchia/expiry-radar/compare/v1.8.1...v1.8.2) (2026-10-21)


### Bug Fixes

* **deps:** bump Go toolchain to go1.26.10 ([#91](https://github.com/fabiocicerchia/expiry-radar/issues/91)) ([ff9e352](https://github.com/fabiocicerchia/expiry-radar/commit/ff9e3521c3402b2a44e3c3a3021139ee69597082))

## [1.8.1](https://github.com/fabiocicerchia/expiry-radar/compare/v1.8.0...v1.8.1) (2026-09-27)


### Bug Fixes

* **rank:** make urgency steep near the deadline, not linear over 90 days ([#81](https://github.com/fabiocicerchia/expiry-radar/issues/81)) ([d421a2c](https://github.com/fabiocicerchia/expiry-radar/commit/d421a2c59de698fcd565b9a78472bbbb654d07b2))

## [1.8.0](https://github.com/fabiocicerchia/expiry-radar/compare/v1.7.0...v1.8.0) (2026-09-22)


### Features

* **manual:** record a credential that cannot expire without inventing a date ([#78](https://github.com/fabiocicerchia/expiry-radar/issues/78)) ([28f814e](https://github.com/fabiocicerchia/expiry-radar/commit/28f814ed28db1dcc65702801a65b60bb35b150c7))

## [1.7.0](https://github.com/fabiocicerchia/expiry-radar/compare/v1.6.0...v1.7.0) (2026-09-22)


### Features

* **cloudflare:** read the account's own API tokens, and the ones that never expire ([#76](https://github.com/fabiocicerchia/expiry-radar/issues/76)) ([18cd42a](https://github.com/fabiocicerchia/expiry-radar/commit/18cd42aa2017b4d507dd449467fa76b3e11ea206))

## [1.6.0](https://github.com/fabiocicerchia/expiry-radar/compare/v1.5.2...v1.6.0) (2026-09-20)


### Features

* **k8s:** report cluster trust anchors and cert-manager renewal health ([#66](https://github.com/fabiocicerchia/expiry-radar/issues/66)) ([af42ec6](https://github.com/fabiocicerchia/expiry-radar/commit/af42ec6697e81241dd3831df6b39ebaeb6716727))

## [1.5.2](https://github.com/fabiocicerchia/expiry-radar/compare/v1.5.1...v1.5.2) (2026-09-20)


### Bug Fixes

* **ci:** keep actions: read on the job that uploads sarif ([#72](https://github.com/fabiocicerchia/expiry-radar/issues/72)) ([ce11a6c](https://github.com/fabiocicerchia/expiry-radar/commit/ce11a6cb7068693561fe488668f886f738aad8c7))
* **output:** stop html/template turning a CSS comment into a space ([#73](https://github.com/fabiocicerchia/expiry-radar/issues/73)) ([3ed06e1](https://github.com/fabiocicerchia/expiry-radar/commit/3ed06e10bd8ab7739c346833bf90a712e18afba3))

## [1.5.1](https://github.com/fabiocicerchia/expiry-radar/compare/v1.5.0...v1.5.1) (2026-09-11)


### Bug Fixes

* **release:** let the release PR carry a token that isn't GITHUB_TOKEN ([#58](https://github.com/fabiocicerchia/expiry-radar/issues/58)) ([786b743](https://github.com/fabiocicerchia/expiry-radar/commit/786b7431dc974c5b743fe4a619b07fa681814ead))

## [1.5.0](https://github.com/fabiocicerchia/expiry-radar/compare/v1.4.0...v1.5.0) (2026-09-09)


### Features

* **aws:** -verify-aws, and make the degradation rule testable at last ([#38](https://github.com/fabiocicerchia/expiry-radar/issues/38)) ([8bce5d8](https://github.com/fabiocicerchia/expiry-radar/commit/8bce5d8507834141f0d9febf344b13abf17f92c5))
* **ci:** let the release publish the extension ([#34](https://github.com/fabiocicerchia/expiry-radar/issues/34)) ([1dd70e6](https://github.com/fabiocicerchia/expiry-radar/commit/1dd70e663cfcdf6876629b79cc08b2937d207349))
* **docs:** build the docs site in Actions and drop Read the Docs ([#24](https://github.com/fabiocicerchia/expiry-radar/issues/24)) ([9e32012](https://github.com/fabiocicerchia/expiry-radar/commit/9e3201228d683fe6df895316b0bea6bcf552b8c0))
* **domain:** fall back to WHOIS when a TLD has no RDAP service ([9e0834a](https://github.com/fabiocicerchia/expiry-radar/commit/9e0834ae9c54706ee56bfa906b9ddb3d08625036))
* **output:** add an HTML report with stats, grouping and filtering ([8f36956](https://github.com/fabiocicerchia/expiry-radar/commit/8f3695607b42d12d56903e1b1f496c623595a1f7))
* **packaging:** man page, OS packages and a staged install ([#51](https://github.com/fabiocicerchia/expiry-radar/issues/51)) ([97a53a3](https://github.com/fabiocicerchia/expiry-radar/commit/97a53a3d46a7a06b368ec91792afdddd1f10c08b))
* **vscode:** swap the marketplace icon for an hourglass ([#36](https://github.com/fabiocicerchia/expiry-radar/issues/36)) ([adf79e2](https://github.com/fabiocicerchia/expiry-radar/commit/adf79e2cd166cf94f7b1a2ce7f2529586f5ea024))


### Bug Fixes

* **ci:** pin the editorconfig-checker binary version ([#40](https://github.com/fabiocicerchia/expiry-radar/issues/40)) ([cb741f4](https://github.com/fabiocicerchia/expiry-radar/commit/cb741f43e990c6683b03fcea2c54d8bea1baf8d9))
* **ci:** stop security workflows failing on private repos ([#3](https://github.com/fabiocicerchia/expiry-radar/issues/3)) ([59942f0](https://github.com/fabiocicerchia/expiry-radar/commit/59942f02b45ad172222713505675324ae69a6967))
* **pre-commit:** stop check-yaml failing on Helm templates and multi-doc manifests ([4a634bd](https://github.com/fabiocicerchia/expiry-radar/commit/4a634bdbe63a2075d6fa8e0c28137d027f065ec7))
* **release:** actually publish the Homebrew cask ([#56](https://github.com/fabiocicerchia/expiry-radar/issues/56)) ([d739d26](https://github.com/fabiocicerchia/expiry-radar/commit/d739d26ec04782086f72d6c5f409e6d187dff904))
* **release:** sign checksums with a Sigstore bundle ([#53](https://github.com/fabiocicerchia/expiry-radar/issues/53)) ([298bb15](https://github.com/fabiocicerchia/expiry-radar/commit/298bb15aed7f8372a36f7f28075e6f2408b32daf))
* security and code-quality findings ([#14](https://github.com/fabiocicerchia/expiry-radar/issues/14)) ([83da568](https://github.com/fabiocicerchia/expiry-radar/commit/83da56859049cd9054cf81217694b307ee98e682))
* spell "unparsable" the way the typos linter expects ([35f1bcd](https://github.com/fabiocicerchia/expiry-radar/commit/35f1bcddb5b9faeaf105ff1f7bd851005378ec75))
* unblock quality and clear the Scorecard pinned-dependencies finding ([#26](https://github.com/fabiocicerchia/expiry-radar/issues/26)) ([fb67097](https://github.com/fabiocicerchia/expiry-radar/commit/fb67097d3dd8981bd1ce647dfdd7c706e19195ca))

## [1.4.0](https://github.com/fabiocicerchia/expiry-radar/compare/v1.3.1...v1.4.0) (2026-09-09)


### Features

* **packaging:** man page, OS packages and a staged install ([#51](https://github.com/fabiocicerchia/expiry-radar/issues/51)) ([97a53a3](https://github.com/fabiocicerchia/expiry-radar/commit/97a53a3d46a7a06b368ec91792afdddd1f10c08b))

## [1.3.1](https://github.com/fabiocicerchia/expiry-radar/compare/v1.3.0...v1.3.1) (2026-09-04)


### Bug Fixes

* **ci:** pin the editorconfig-checker binary version ([#40](https://github.com/fabiocicerchia/expiry-radar/issues/40)) ([cb741f4](https://github.com/fabiocicerchia/expiry-radar/commit/cb741f43e990c6683b03fcea2c54d8bea1baf8d9))

## [1.3.0](https://github.com/fabiocicerchia/expiry-radar/compare/v1.2.0...v1.3.0) (2026-09-03)


### Features

* **aws:** -verify-aws, and make the degradation rule testable at last ([#38](https://github.com/fabiocicerchia/expiry-radar/issues/38)) ([8bce5d8](https://github.com/fabiocicerchia/expiry-radar/commit/8bce5d8507834141f0d9febf344b13abf17f92c5))
* **vscode:** swap the marketplace icon for an hourglass ([#36](https://github.com/fabiocicerchia/expiry-radar/issues/36)) ([adf79e2](https://github.com/fabiocicerchia/expiry-radar/commit/adf79e2cd166cf94f7b1a2ce7f2529586f5ea024))

## [1.2.0](https://github.com/fabiocicerchia/expiry-radar/compare/v1.1.1...v1.2.0) (2026-09-01)


### Features

* **ci:** let the release publish the extension ([#34](https://github.com/fabiocicerchia/expiry-radar/issues/34)) ([1dd70e6](https://github.com/fabiocicerchia/expiry-radar/commit/1dd70e663cfcdf6876629b79cc08b2937d207349))

## [1.1.1](https://github.com/fabiocicerchia/expiry-radar/compare/v1.1.0...v1.1.1) (2026-08-29)


### Bug Fixes

* unblock quality and clear the Scorecard pinned-dependencies finding ([#26](https://github.com/fabiocicerchia/expiry-radar/issues/26)) ([fb67097](https://github.com/fabiocicerchia/expiry-radar/commit/fb67097d3dd8981bd1ce647dfdd7c706e19195ca))

## [1.1.0](https://github.com/fabiocicerchia/expiry-radar/compare/v1.0.1...v1.1.0) (2026-08-25)


### Features

* **docs:** build the docs site in Actions and drop Read the Docs ([#24](https://github.com/fabiocicerchia/expiry-radar/issues/24)) ([9e32012](https://github.com/fabiocicerchia/expiry-radar/commit/9e3201228d683fe6df895316b0bea6bcf552b8c0))

## [1.0.1](https://github.com/fabiocicerchia/expiry-radar/compare/v1.0.0...v1.0.1) (2026-08-13)


### Bug Fixes

* security and code-quality findings ([#14](https://github.com/fabiocicerchia/expiry-radar/issues/14)) ([83da568](https://github.com/fabiocicerchia/expiry-radar/commit/83da56859049cd9054cf81217694b307ee98e682))

## 1.0.0 (2026-08-06)


### Features

* **domain:** fall back to WHOIS when a TLD has no RDAP service ([9e0834a](https://github.com/fabiocicerchia/expiry-radar/commit/9e0834ae9c54706ee56bfa906b9ddb3d08625036))
* **output:** add an HTML report with stats, grouping and filtering ([8f36956](https://github.com/fabiocicerchia/expiry-radar/commit/8f3695607b42d12d56903e1b1f496c623595a1f7))


### Bug Fixes

* **ci:** stop security workflows failing on private repos ([#3](https://github.com/fabiocicerchia/expiry-radar/issues/3)) ([59942f0](https://github.com/fabiocicerchia/expiry-radar/commit/59942f02b45ad172222713505675324ae69a6967))
* **pre-commit:** stop check-yaml failing on Helm templates and multi-doc manifests ([4a634bd](https://github.com/fabiocicerchia/expiry-radar/commit/4a634bdbe63a2075d6fa8e0c28137d027f065ec7))
* spell "unparsable" the way the typos linter expects ([35f1bcd](https://github.com/fabiocicerchia/expiry-radar/commit/35f1bcddb5b9faeaf105ff1f7bd851005378ec75))

## [Unreleased]

### Added
### Changed
### Deprecated
### Removed
### Fixed
### Security

## [0.1.0] - 2026-08-01

### Added
- Initial release.

[Unreleased]: https://github.com/fabiocicerchia/expiry-radar/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/fabiocicerchia/expiry-radar/releases/tag/v0.1.0
