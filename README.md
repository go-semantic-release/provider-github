# :octocat: provider-github
[![CI](https://github.com/go-semantic-release/provider-github/workflows/CI/badge.svg?branch=master)](https://github.com/go-semantic-release/provider-github/actions?query=workflow%3ACI+branch%3Amaster)
[![Go Report Card](https://goreportcard.com/badge/github.com/go-semantic-release/provider-github)](https://goreportcard.com/report/github.com/go-semantic-release/provider-github)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/go-semantic-release/provider-github)](https://pkg.go.dev/github.com/go-semantic-release/provider-github)

The GitHub provider for [go-semantic-release](https://github.com/go-semantic-release/semantic-release).

### Provider Option

The provider options can be configured via the `--provider-opt` CLI flag.

| Name | Description | Example |
|---|---|---|
| github_enterprise_host | This configures the provider to use a GitHub Enterprise host endpoint | `--provider-opt github_enterprise_host=github.mycorp.com` |
| github_use_compare_commits | This enables the [compare commits API](https://docs.github.com/en/rest/reference/repos#compare-two-commits) for fetching the commits  | `--provider-opt github_use_compare_commits=true` |
| slug | The owner and repository name  | `--provider-opt slug=go-semantic-release/provider-github` |
| token | GitHub token  | `--provider-opt token=xx` |

## Licence

The [MIT License (MIT)](http://opensource.org/licenses/MIT)

Copyright © 2020 [Christoph Witzko](https://twitter.com/christophwitzko)
