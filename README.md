# hottub

A CI bridge from GitHub to SourceHut.

A [public instance] is available.

## Building

    go build

## Installation

1. Follow the [GitHub guide] to register an app suitable for the Checks API:
   - Open the [Register a new app](https://github.com/settings/apps/new) page
   - Set a name and homepage URL
   - Leave the callback URL empty
   - Set the setup URL to `https://<domain>/post-install`
   - Set the webhook URL to `https://<domain>/webhook`
   - In *Repository permissions*, select:
     - Checks: Read and write
     - Commit statuses: Read and write
     - Contents: Read-only
     - Metadata: Read-only
     - Pull requests: Read-only
   - In *Subscribe to events*, check:
     - Check run
     - Check suite
     - Pull request
2. Grab the GitHub app ID and webhook secret (optional for local development).
   Download a new PEM private key.
3. Start hottub:

       hottub -gh-app-id <id> -gh-private-key <path> -gh-webhook-secret <secret>

Optionally, to improve the authorization flow, you can [register an sr.ht
OAuth2 client] (setting the Redirection URI to
`https://<domain>/authorize-srht`) and pass its credentials with
`-metasrht-client-id` and `-metasrht-client-secret`.

## License

AGPLv3, see LICENSE.

Copyright (C) 2022 Simon Ser

[GitHub guide]: https://docs.github.com/en/developers/apps/guides/creating-ci-tests-with-the-checks-api
[register an sr.ht OAuth2 client]: https://meta.sr.ht/oauth2/client-registration
[public instance]: https://hottub.emersion.fr/
