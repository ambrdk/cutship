# GitHub and Telegram

This guide sets up the whole cycle for one service and one deploy repository on GitHub, with posts in a Telegram channel. The working files are in [examples/github](../examples/github).

## What you need

- A service repository that builds a container image in GitHub Actions and pushes it to a registry (ghcr, Docker Hub or any OCI registry).
- A deploy repository where each app's image is pinned in the `image` line of `apps/<app>/app.yml` (the `ansible` preset) and a workflow deploys the apps.
- Conventional commit subjects in the service (`feat:`, `fix:`, `perf:`, `!` for breaking changes). Squash merges use the pull request title, so the title must follow the format too.
- A Telegram channel with a discussion group.

## 1. cutship.yaml

Put `cutship.yaml` in the root of the deploy repository. The smallest useful file:

```yaml
version: 1
forge:
  type: github
deploy_repo: acme/deploy
apps:
  - name: api
    image: ghcr.io/acme/api
    pin: { preset: ansible }
notify:
  - type: telegram
    chat_id: "-1001234567890"
```

- `apps` lists your own images. Their tags must follow `release.tag_format` (`v{version}` by default, so `v1.4.0`). Other images in the deploy repository (redis, grafana) need no entry: cutship only checks that they are pinned to an exact tag other than `latest`.
- Bump branches are `cutship/<app>-<tag>` unless you set `release.bump_branch_prefix`.
- The check that has to be green before a bump's deploy may skip CI is `lint` unless you set `lanes.required_check`.

Run `cutship validate` in the deploy repository. It prints every problem with its line, for example `::error file=cutship.yaml,line=5::environment: unknown field "environment"`. Editors that understand JSON Schema can use the output of `cutship schema`.

## 2. Tokens and settings

[tokens.md](tokens.md) lists every token and permission. In short:

- In the service repository: the secret `CUTSHIP_DEPLOY_TOKEN`, a fine-grained token that can write contents and pull requests of the deploy repository, and `TELEGRAM_BOT_TOKEN`.
- In the deploy repository: the secret `TELEGRAM_BOT_TOKEN`. The workflows use their own `GITHUB_TOKEN` for everything else.
- In the deploy repository settings: turn on "Allow auto-merge" and "Automatically delete head branches", and add a branch protection rule on the default branch that requires the `lint` check. Without the required check, auto-merge merges a bump before CI finishes.

## 3. The Telegram bot

1. Create a bot with @BotFather for cutship alone. A bot that already reads updates somewhere else cannot be shared, because Telegram gives the updates of one token to one reader.
2. Add it to the channel as an admin that may post and edit messages.
3. Add it to the channel's discussion group as an admin, so it sees the copies of its posts there and can reply to them. Without a discussion group the post goes out without the comment.

`cutship validate --online` checks all three.

## 4. The service workflow

[examples/github/service/.github/workflows/release.yml](../examples/github/service/.github/workflows/release.yml) runs on every push to the default branch:

1. `cutship version next --deploy-repo acme/deploy --app api` prints the next tag, or nothing when the new commits do not make a release. It needs the full history (`fetch-depth: 0`) and reads `cutship.yaml` from the deploy repository.
2. The image is built and pushed with that tag and the label `org.opencontainers.image.revision=${{ github.sha }}`.
3. `cutship release --app api --tag <tag> --deploy-repo acme/deploy` checks that the image exists and came from this commit, tags the service, closes older bumps of the app, opens the bump with auto-merge and creates the post. It prints `outcome=bump`, `same` (the pin is already there) or `superseded` (a newer bump of the app is open).

Re-running a failed release is safe: every step checks what is already done.

## 5. The deploy repository workflows

- [ci.yml](../examples/github/deploy/.github/workflows/ci.yml) runs on pull requests and as a reusable workflow. `cutship lane --pr <n>` prints `fast` when the only change is one pin moving to a higher version of the same image; the fast job then runs only `cutship pin verify`, the full job runs your checks. The `lint` job joins both and is the required check.
- [deploy.yml](../examples/github/deploy/.github/workflows/deploy.yml) runs on pushes that change `apps/`. `cutship deploy plan --base <before>` prints `apps=[...]`, `skip_ci=` and `change=`. An app is in the plan when its directory changed since its last successful deploy, or when its last deploy failed. CI is skipped only for a merged bump whose `lint` check is green and when nothing older is waiting. `cutship deploy start` and `finish` wrap your deploy step, record each app as a GitHub Deployment `prod/<app>` and move the post. Pass `--report deploy-report.json` to finish to list the hosts; the format is in [commands.md](commands.md#deploy-finish).
- [notify.yml](../examples/github/deploy/.github/workflows/notify.yml) is optional. It adds the result of the bump's CI to the post and marks a bump closed without a merge with ⚪.

## 6. Check and try

From a checkout of the deploy repository:

```bash
CUTSHIP_DEPLOY_TOKEN=... TELEGRAM_BOT_TOKEN=... cutship validate --online
```

It reads the deploy repository with the token, checks every pin and its image in the registry, the token's rights, the auto-merge and branch settings and the bot's rights in the channel and its discussion group. Errors exit with 1, warnings only print.

Then merge a `fix:` commit into the service. Within a few minutes the post shows 📦, the bump merges by itself, the deploy starts and the post ends with ✅.
