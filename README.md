# hottub

A CI bridge from GitHub to SourceHut.

A [public instance] is available.

## Building

    go build

## Installation

1. Follow the [GitHub guide] to register an app suitable for the Checks API.
2. Set the GitHub app setup URL to `https://<domain>/post-install` and the
   webhook URL to `https://<domain>/webhook`.
3. Grab the GitHub app ID and webhook secret (optional for local development).
   Download a new PEM private key.
4. Start hottub:

       hottub -gh-app-id <id> -gh-private-key <path> -gh-webhook-secret <secret>

## License

AGPLv3, see LICENSE.

Copyright (C) 2022 Simon Ser

[GitHub guide]: https://docs.github.com/en/developers/apps/guides/creating-ci-tests-with-the-checks-api
[public instance]: https://hottub.emersion.fr/
