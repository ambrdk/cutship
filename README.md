# cutship

One Go tool for the release cycle of ambrdk services: from a merge in a service to a deployed app and a Telegram post about it. Every rule of that cycle (which tag is allowed, what a bump branch is called, when a pull request can take the fast lane, when a deploy can skip CI) lives in one Go package here, and the workflows in `ci-templates` and `ansible` only call `cutship`.

This repository used to be `deploy-notify`; `cutship notify` is that tool.

## Commands

| Command | What it does |
|---|---|
| `cutship validate [--config cutship.yaml] [--online]` | checks `cutship.yaml`, prints `::error file=,line=::` per problem; `--online` also reads the deploy repository with `CUTSHIP_DEPLOY_TOKEN` (or `GITHUB_TOKEN`), checks every pin and its image, the token's rights and the auto-merge and branch settings |
| `cutship schema` | prints the JSON Schema of `cutship.yaml` |
| `cutship version next [--config cutship.yaml] --app <app> [--dir .]` | prints the next release tag from conventional commits since the last one, nothing when there is no release; needs the full history |
| `cutship release --app <app> --tag <tag> --deploy-repo <owner/name> [--sha sha]` | after the image is pushed: checks its revision label, tags the service, closes older bumps and opens the bump with auto-merge; prints `outcome=bump\|same\|superseded`, `tag=`, `pr=`, `url=` |
| `cutship deploy plan --base <sha> [--head sha] [--env env]` | prints `apps=<json>`, `skip_ci=` and `change=` from the ledger of GitHub Deployments, one environment `<env>/<app>` per app |
| `cutship deploy start --apps <json>` / `cutship deploy finish --apps <json> --result success\|failure [--report deploy-report.json]` | record the deploy in the ledger and in the bump's state |
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

## Release posts

Each release gets one Telegram post that the commands themselves keep up to date. `cutship release` creates it (📦 waiting for merge), `cutship deploy start` moves it to 🔀 merged and 🚀 deploying, and `cutship deploy finish` ends it with ✅ or ❌ and the hosts from the deploy report. Under the post, in the channel's discussion group, one comment holds the details and a timeline. Both messages are edited in place. The chat is `notify[].chat_id` in `cutship.yaml`, the bot token comes from `TELEGRAM_BOT_TOKEN`.

There is no server. Between runs, the state of each release lives in a hidden comment on the bump pull request: a link to the post followed by `<!-- cutship:v1 {json} -->`. Comments of the older `deploy-notify:v1` format are read and rewritten in the new one, so a post started before the move keeps being edited. A deploy that is not a bump keeps its state on the merged pull request it came from, or, for a direct push, in the payload of its GitHub Deployment.

If Telegram fails, the command still does its job and prints a `::warning::` with the reason; the next stage publishes the post with the whole history.

Repeated events change nothing: every event has an id, and seen ids are kept in the state. A re-run after a failure turns ❌ into ✅ and adds `[RETRY]` to the timeline.

## cutship notify

An optional step for two things the commands cannot see: the result of the bump's CI and a bump closed without a merge.

| GitHub event | What it does |
|---|---|
| `pull_request` opened / reopened on a bump branch | creates the post or brings a closed one back |
| `workflow_run` of `lanes.ci_workflow` (default `ci`) completed for a bump | adds the check result |
| `pull_request` closed | merged by …, or ⚪ not merged |

`notify` reads `cutship.yaml` from the repository through the API, so the workflow needs no checkout.

### Telegram setup

1. Create a separate bot for this. A bot that already long-polls updates somewhere else (alert-relay) cannot be shared, because Telegram gives updates of one token to one reader at a time.
2. Make the bot an admin of the channel, allowed to post and edit messages.
3. Make it an admin of the channel's discussion group too, so it sees the copies of its posts there and can reply to them. Without a discussion group the post goes out without a comment. If a copy has not arrived within 30 seconds, the run logs a warning and the next event of that release looks for it again. If it is still missing, the comment goes to the group as a plain message, not a reply.

### Workflow

In the deploy repository, with the repository secret `TELEGRAM_DEPLOY_BOT_TOKEN`:

```yaml
name: notify

on:
  pull_request:
    types: [opened, reopened, closed]
  workflow_run:
    workflows: [ci]
    types: [completed]

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
    steps:
      - uses: ambrdk/cutship@<sha> # vX.Y.Z
        with:
          telegram-token: ${{ secrets.TELEGRAM_DEPLOY_BOT_TOKEN }}
```

The concurrency group keeps notify runs in order, but it does not cover `cutship release` in service repositories or `cutship deploy` in the deploy workflow, and those publish with the same bot. Two things follow from that, and both are known limits of this version:

- The bot reads `getUpdates` to find the copy of its post in the discussion group. Telegram allows one reader per token, so when two commands publish at the same moment one of them may get no copy, with a warning in the log; the next stage of that release looks for the copy again.
- Each command re-reads the state right before it writes and publishes the newest version, so a post does not go back a stage. Two writes to the same state comment within the same second can still lose one timeline line.

`workflow_run` only fires when the workflow file is on the default branch.

### Environment

`release`, `deploy` and `notify` read these:

| Variable | Meaning |
|---|---|
| `TELEGRAM_BOT_TOKEN` | bot token, needed when `notify` in `cutship.yaml` has a telegram entry |
| `GITHUB_TOKEN` | reads the repository, writes pull request comments and deployments |
| `CUTSHIP_DEPLOY_TOKEN` | `release` only: opens the bump in the deploy repository |
| `TELEGRAM_API_URL` | optional Bot API base URL |

`GITHUB_REPOSITORY`, `GITHUB_EVENT_NAME`, `GITHUB_EVENT_PATH`, `GITHUB_SHA` and `GITHUB_API_URL` come from Actions.

## Development

```bash
go test -race ./...
bash scripts/pick_tag_test.sh
bash scripts/install_test.sh
```

## License

Apache-2.0, see `LICENSE` and `NOTICE`.
