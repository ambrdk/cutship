# Troubleshooting

Each entry starts with the text cutship prints.

## Errors

**`the next version needs the full history: check out with fetch-depth: 0`**
`version next` ran on a shallow clone. Set `fetch-depth: 0` on `actions/checkout`.

**`no tag matches release.tag_format "...", but v1.4.0 exists`**
The service has release tags in another format than the config says. Fix `release.tag_format` (for example `{version}` for tags without `v`) or tag the last release in the configured format.

**`image ghcr.io/acme/api:v1.4.1 not found: push the image first`**
`release` ran before the image was pushed, or with another tag. Run it after the push step, with the same tag.

**`image ... was built from <sha>, not <sha>: release a new tag`**
An image with this tag already exists and came from another commit. Tags are never moved: merge a new `fix:` commit to get the next version.

**`... points at another commit`**
The git tag already exists on another commit of the service. Same cause and fix as above.

**`tag ... is lower than current ...`**
The deploy repository already pins a higher version of the app. A late release of an older commit stops here on purpose.

**`CUTSHIP_DEPLOY_TOKEN is required`**
Add the secret to the service repository and pass it to the `release` step. See [tokens.md](tokens.md).

**`auto-merge of ...: turn on Allow auto-merge in the repository settings`**
Turn on "Allow auto-merge" in the deploy repository. The bump stays open until then.

**`app "x" is not in ...`**
The `--app` name is not under `apps` in `cutship.yaml`.

**`environment "staging" is not in cutship.yaml`**
`--env` must be one of `environments`.

**`list apps/ at <sha>: ...`**
`deploy plan` could not read the `apps/` directory of the deploy repository. Check that `GITHUB_TOKEN` has `contents: read` and that `--base` is a commit of this repository.

**`... is not supported yet, planned`**
The config uses something planned for a later version: GitLab, Slack, Telegram topics, Helm or other pin presets, `tag_prefix`. The [README](../README.md#status) lists what is planned.

## Warnings

**`image ... has no org.opencontainers.image.revision label, its commit is not checked`**
Add the label to the image build, for example `labels: org.opencontainers.image.revision=${{ github.sha }}` in `docker/build-push-action`.

**`the release goes on without a post: TELEGRAM_BOT_TOKEN is required`** (also for deploy and notify)
`notify` in `cutship.yaml` has a Telegram entry but the step has no `TELEGRAM_BOT_TOKEN`.

**`telegram:<chat>: the release post was not updated: ...`**
Telegram failed. The command still did its job; the next stage of the release publishes the post with the whole history.

**`the post went out without a comment: its copy did not show up in the discussion group in time`**
The bot is not an admin of the discussion group, or another cutship run kept reading updates with the same bot for the whole 30 seconds the bot waits for the copy. `cutship validate --online` tells which. The next stage looks for the copy again.

**`the channel has no discussion group or it could not be read`**
Link a discussion group to the channel, or live with posts without the comment.

**`the token is a GITHUB_TOKEN`** (from `validate --online`)
A bump opened with a `GITHUB_TOKEN` does not start CI. Use a personal access token or an app token as `CUTSHIP_DEPLOY_TOKEN`.

**`not a conventional commit subject, no release from it: ...`**
A commit in the release range has a subject like `Merge branch ...`. Use squash merges with a conventional pull request title.

## Outcomes that are not errors

- `outcome=same` from `release`: the pin already has this tag, nothing to bump. The tag was still set on the service.
- `outcome=superseded`: a bump of a newer version of the app is open; this release is skipped and its own bump, if any, is closed.
- Empty output from `version next`: the commits since the last release are all `chore:`, `docs:`, `ci:` and similar.
