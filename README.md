# hottub

A CI bridge from GitHub to SourceHut.

## Building

    go build

## Installation

1. Follow the [GitHub guide] to register an app suitable for the Checks API.
2. Export the GitHub app ID as `GITHUB_APP_IDENTIFIER`.
3. Export the path to the PEM private key as `GITHUB_PRIVATE_KEY`.
4. Export the webhook secret as `GITHUB_WEBHOOK_SECRET` (optional for local
   development).
5. Start hottub.

## License

AGPLv3, see LICENSE.

Copyright (C) 2022 Simon Ser

[GitHub guide]: https://docs.github.com/en/developers/apps/guides/creating-ci-tests-with-the-checks-api
