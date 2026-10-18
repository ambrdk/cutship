# deploy-notify

Keeps one Telegram post per release in sync with what GitHub does to it. A release is a bump pull request in the deploy repository (`bump/<app>-<tag>`, opened by `ci-templates` image-release). The post in the channel goes from 📦 waiting for merge to 🔀 merged, 🚀 deploying, and then ✅, ❌ or ⚪. Under it, in the channel's discussion group, one comment holds the details and a timeline. Both messages are edited in place, the same way alert-relay handles alerts.

It runs as a step in a workflow of the deploy repository and exits. There is no server. Between runs, the state of each release lives in a hidden comment of the bot on the bump pull request: a link to the post followed by `<!-- deploy-notify:v1 {json} -->`. If that comment is deleted or edited past recognition, the next event starts a new post.

## Events

| GitHub event | What it does |
|---|---|
| `pull_request` opened / reopened on `bump/*` | creates the post or brings a closed one back |
| `workflow_run` of `ci` completed for a `bump/*` pull request | adds the check result |
| `pull_request` closed | merged by …, or ⚪ not merged |
| `workflow_run` of `deploy` requested | 🚀 for the pull request merged by that push |
| `workflow_run` of `deploy` completed | result per app, hosts from the `deploy-report` artifact |

A deploy started with `workflow_dispatch`, or a push without a pull request, gets its own post when it finishes and is not tracked further. A pushed change that is not a bump (an edited `env.j2`, say) opens a thread on its pull request when the deploy starts.

Repeated events change nothing: every event has an id that includes the run attempt, and seen ids are kept in the state. A re-run after a failure turns ❌ into ✅ and adds `[RETRY]` to the timeline.

## Telegram setup

1. Create a separate bot for this. A bot that already long-polls updates somewhere else (alert-relay) cannot be shared, because Telegram gives updates of one token to one reader at a time.
2. Make the bot an admin of the channel, allowed to post and edit messages.
3. Make it an admin of the channel's discussion group too, so it sees the copies of its posts there and can reply to them. Without a discussion group the post goes out without a comment.

## Workflow

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
      - uses: ambrdk/deploy-notify@v0.1.0
        with:
          telegram-token: ${{ secrets.TELEGRAM_DEPLOY_BOT_TOKEN }}
          telegram-channel-id: ${{ secrets.TELEGRAM_DEPLOY_CHANNEL_ID }}
```

All runs share one concurrency group with `queue: max`, so events are handled one at a time. That keeps the bot to one `getUpdates` reader and stops two events of the same release from overwriting each other's state.

`workflow_run` only fires when the workflow file is on the default branch.

## Environment

The action sets these for `deploy-notify handle`:

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
```
