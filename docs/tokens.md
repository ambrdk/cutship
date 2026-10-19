# Tokens and permissions

cutship reads every secret from the environment and never prints it.

| Variable | Used by | What it needs |
|---|---|---|
| `GITHUB_TOKEN` | every command that talks to GitHub | the workflow's own token, with the permissions below |
| `CUTSHIP_DEPLOY_TOKEN` | `release`, `version next --deploy-repo`, `validate --online` | a token of a user or a GitHub App with access to the deploy repository |
| `TELEGRAM_BOT_TOKEN` | `release`, `deploy start/finish`, `notify`, `validate --online` | the token of the bot from @BotFather |
| `TELEGRAM_API_URL` | same | optional, a Bot API server other than api.telegram.org |
| `CUTSHIP_REGISTRY_USERNAME`, `CUTSHIP_REGISTRY_PASSWORD` | `release`, `validate --online` | optional; by default cutship uses the Docker credentials of the runner (`docker/login-action`) and then anonymous access |

## CUTSHIP_DEPLOY_TOKEN

`cutship release` runs in the service repository, but it reads `cutship.yaml`, pushes the bump branch, opens the pull request, enables auto-merge and writes the state comment in the deploy repository. The service's `GITHUB_TOKEN` cannot do that, and a pull request opened with any `GITHUB_TOKEN` would not start the deploy repository's CI.

Use one of:

- A fine-grained personal access token limited to the deploy repository, with **Contents: read and write** and **Pull requests: read and write**.
- A GitHub App installed on the deploy repository with the same two permissions. Installation tokens expire after an hour, so do not store one in a secret: store the app id and private key, and mint the token in the service workflow right before cutship runs:

  ```yaml
  - id: app
    uses: actions/create-github-app-token@bcd2ba49218906704ab6c1aa796996da409d3eb1 # v3.2.0
    with:
      app-id: ${{ vars.CUTSHIP_APP_ID }}
      private-key: ${{ secrets.CUTSHIP_APP_PRIVATE_KEY }}
      owner: acme
      repositories: deploy
  ```

  and pass `CUTSHIP_DEPLOY_TOKEN: ${{ steps.app.outputs.token }}` to `version next` and `release`.

## GITHUB_TOKEN permissions

| Workflow | Permissions |
|---|---|
| service release | `contents: write` (tag the service), `packages: write` if the image goes to ghcr |
| deploy repository CI (`cutship lane --pr`) | `contents: read`, `pull-requests: read` |
| deploy plan | `contents: read`, `pull-requests: read`, `checks: read`, `statuses: read`, `deployments: read` |
| deploy start and finish | `contents: read`, `deployments: write`, `pull-requests: write` |
| notify | `contents: read`, `pull-requests: write` |

GitHub creates the Deployment environments `prod/<app>` by itself on the first deploy of each app. They are separate from any environment your deploy job uses for its secrets.

## Repository settings

In the deploy repository:

- **Allow auto-merge**, so bumps merge themselves once CI is green.
- **Automatically delete head branches**, so merged bump branches go away.
- A branch protection rule on the default branch that requires the check named in `lanes.required_check` (`lint` by default).

`cutship validate --online` warns when auto-merge or branch deletion is off and when the token is a `GITHUB_TOKEN`.

## The Telegram bot

- One bot for cutship alone: Telegram hands the updates of a token to one reader, and cutship reads them to find the copy of its post in the discussion group.
- Admin of the channel with the rights to post and edit messages.
- Admin of the channel's discussion group.

## Verifying a release

Every release has `checksums.txt` signed with cosign keyless signing from the release workflow. The `ambrdk/cutship` Action checks the archive against `checksums.txt`. To check the signature too:

```bash
cosign verify-blob \
  --certificate-identity 'https://github.com/ambrdk/cutship/.github/workflows/release.yml@refs/tags/v0.3.0' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  --bundle checksums.txt.sigstore.json checksums.txt
```

```bash
sha256sum --check --ignore-missing checksums.txt
```
