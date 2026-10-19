# cutship

One Go tool for the release cycle of ambrdk services: from a merge in a service to a deployed app and a Telegram post about it. Every rule of that cycle (which tag is allowed, what a bump branch is called, when a pull request can take the fast lane, when a deploy can skip CI) lives in one Go package here, and the workflows in `ci-templates` and `ansible` only call `cutship`.

This repository used to be `deploy-notify`; `cutship notify` is that tool.

## Commands

| Command | What it does |
|---|---|
| `cutship validate [--config cutship.yaml]` | checks `cutship.yaml`, prints `::error file=,line=::` per problem |
| `cutship schema` | prints the JSON Schema of `cutship.yaml` |
| `cutship version next [--config cutship.yaml] --app <app> [--dir .]` | prints the next release tag from conventional commits since the last one, nothing when there is no release; needs the full history |
| `cutship notify` | keeps one Telegram post per release in sync with GitHub, see below |
| `cutship pin read [--root dir] <app>` | prints the image pinned in `apps/<app>/app.yml` |
| `cutship pin check [--root dir] <app> <tag>` | prints `bump` or `same`, fails when the tag breaks the rule |
| `cutship pin set [--root dir] <app> <tag>` | changes only the `image` line to the app's image and tag, keeps every other byte |
| `cutship pin verify [--root dir]` | checks every app pin against the tag rule |
| `cutship lane < files.json` | prints `fast` or `full` for the files of a pull request |
| `cutship bump name <app> <tag>` | prints `branch=` and `title=` of the bump pull request |
| `cutship bump parse <ref>` | prints `<app> <tag>` when the ref is a bump branch |
| `cutship bump superseded <app> <tag> < branches` | prints the open bump branches of the app with a lower version |
| `cutship gate head --pulls <file>` | prints the head sha of the merged bump pull request |
| `cutship gate skip --pulls <file> --checks <file> --pending <json\|unknown>` | prints `true` when the deploy can skip its CI run |

Every command except `schema` reads `cutship.yaml` from the current directory, or the file given with `--config`. Its own images, tag format, bump branch prefix and required check come from there.

Output is plain values on stdout, one per line, so a step can write it to `$GITHUB_OUTPUT`. A broken rule prints `::error::<reason>` on stderr and exits 1; wrong arguments exit 2. The commands, their output and the exit codes are a versioned interface: an incompatible change needs a new major version.

The rules are written down in `CONTEXT.md` here and in the deploy repository.

## Using cutship in a workflow

```yaml
- uses: ambrdk/cutship/setup@<sha> # vX.Y.Z
- run: cutship pin check alert-relay "$TAG"
```

`setup` puts the binary on `PATH`. Every `vX.Y.Z` tag gets a GitHub Release with a prebuilt `cutship_linux_amd64`; setup finds the newest version tag on the commit it is pinned to (a SHA, a version tag or `v1`) and downloads that binary in a few seconds. Pinned to a branch or a commit without a release, it builds from source. The `version` input installs a given release instead of the pinned one and fails the job when that release cannot be downloaded, so a pipeline never runs a different cutship than it asked for. The `version` output says what was installed.

## cutship notify

Keeps one Telegram post per release in sync with what GitHub does to it. A release is a bump pull request in the deploy repository (`bump/<app>-<tag>`, opened by `ci-templates` image-release). The post in the channel goes from 📦 waiting for merge to 🔀 merged, 🚀 deploying, and then ✅, ❌ or ⚪. Under it, in the channel's discussion group, one comment holds the details and a timeline. Both messages are edited in place, the same way alert-relay handles alerts.

It runs as a step in a workflow of the deploy repository and exits. There is no server. Between runs, the state of each release lives in a hidden comment of the bot on the bump pull request: a link to the post followed by `<!-- deploy-notify:v1 {json} -->`. If that comment is deleted or edited past recognition, the next event starts a new post.

### Events

| GitHub event | What it does |
|---|---|
| `pull_request` opened / reopened on `bump/*` | creates the post or brings a closed one back |
| `workflow_run` of `ci` completed for a `bump/*` pull request | adds the check result |
| `pull_request` closed | merged by …, or ⚪ not merged |
| `workflow_run` of `deploy` requested | 🚀 for the pull request merged by that push |
| `workflow_run` of `deploy` completed | result per app, hosts from the `deploy-report` artifact |

A deploy started with `workflow_dispatch`, or a push without a pull request, gets its own post when it finishes and is not tracked further. A pushed change that is not a bump (an edited `env.j2`, say) opens a thread on its pull request when the deploy starts.

Repeated events change nothing: every event has an id that includes the run attempt, and seen ids are kept in the state. A re-run after a failure turns ❌ into ✅ and adds `[RETRY]` to the timeline.

### Telegram setup

1. Create a separate bot for this. A bot that already long-polls updates somewhere else (alert-relay) cannot be shared, because Telegram gives updates of one token to one reader at a time.
2. Make the bot an admin of the channel, allowed to post and edit messages.
3. Make it an admin of the channel's discussion group too, so it sees the copies of its posts there and can reply to them. Without a discussion group the post goes out without a comment. If a copy has not arrived within 30 seconds, the run logs a warning and the next event of that release looks for it again. If it is still missing, the comment goes to the group as a plain message, not a reply.

### Workflow

In the deploy repository, with repository secrets `TELEGRAM_DEPLOY_BOT_TOKEN` and `TELEGRAM_DEPLOY_CHANNEL_ID`:

```yaml
name: notify

on:
  pull_request:
    types: [opened, reopened, closed]
  workflow_run:
    workflows: [ci, deploy]
    types: [requested, completed]

jobs:
  update:
    runs-on: ubuntu-24.04
    concurrency:
      group: deploy-notify
      cancel-in-progress: false
      queue: max
    permissions:
      contents: read
      pull-requests: write
      actions: read
    steps:
      - uses: ambrdk/cutship@<sha> # vX.Y.Z
        with:
          telegram-token: ${{ secrets.TELEGRAM_DEPLOY_BOT_TOKEN }}
          telegram-channel-id: ${{ secrets.TELEGRAM_DEPLOY_CHANNEL_ID }}
```

All runs share one concurrency group with `queue: max`, so events are handled one at a time. That keeps the bot to one `getUpdates` reader and stops two events of the same release from overwriting each other's state.

`workflow_run` only fires when the workflow file is on the default branch.

### Environment

The action sets these for `cutship notify`:

| Variable | Meaning |
|---|---|
| `TELEGRAM_BOT_TOKEN` | bot token |
| `TELEGRAM_CHANNEL_ID` | channel id, `-100...` |
| `GITHUB_TOKEN` | reads the repository, runs and artifacts, writes pull request comments |
| `TELEGRAM_API_URL` | optional Bot API base URL |

`GITHUB_REPOSITORY`, `GITHUB_EVENT_NAME`, `GITHUB_EVENT_PATH` and `GITHUB_API_URL` come from Actions.

## Development

```bash
go test -race ./...
bash scripts/pick_tag_test.sh
bash scripts/install_test.sh
```

## License

Apache-2.0, see `LICENSE` and `NOTICE`.
