# Commands

Values go to stdout, one `key=value` or one value per line, so a step can append them to `$GITHUB_OUTPUT`. Problems go to stderr as `::error::` or `::warning::` lines that GitHub shows as annotations. Exit codes: 0 for success, 1 for a broken rule, a config problem or a failed call, 2 for wrong arguments.

Most commands read `cutship.yaml` from the current directory or from `--config`. `release`, `notify` and `version next --deploy-repo` read it from the deploy repository through the API.

## validate

```
cutship validate [--config cutship.yaml] [--online]
```

Checks the config and prints `::error file=<config>,line=<n>::<field>: <problem>` for each problem. With `--online` it also reads the deploy repository with `CUTSHIP_DEPLOY_TOKEN` (or `GITHUB_TOKEN`), compares the config there with the local one, checks every pin and its image in the registry, whether the token's user may push, whether auto-merge and head branch deletion are on (branch protection is not checked), and for each Telegram entry the bot's rights in the channel and its discussion group. Warnings do not change the exit code.

## schema

```
cutship schema
```

Prints the JSON Schema of `cutship.yaml`. Each release also carries it as `cutship.schema.json`.

## version next

```
cutship version next --app <app> [--dir .] [--config cutship.yaml | --deploy-repo owner/name]
```

Prints the next release tag of the service checked out in `--dir`, or nothing when there is no release. The last release is the highest tag on the history of HEAD that matches `release.tag_format`; the commit subjects after it decide the bump: `feat` gives a minor, `fix` and `perf` a patch, `!` a major (a minor while the major is 0). Other conventional types make no release; subjects that are not conventional make no release and print a warning. When HEAD already carries a release tag, that tag is printed again, so a re-run finishes the same release. The last line on stderr says `last release X, next Y`, or `HEAD is already X`.

Fails on a shallow clone (check out with `fetch-depth: 0`) and when only tags of another format exist.

## release

```
cutship release --app <app> --tag <tag> --deploy-repo owner/name [--sha <commit>] [--repo owner/name]
```

Run after the image is pushed. In order: reads `cutship.yaml` from the deploy repository, checks that `<image>:<tag>` exists and that its `org.opencontainers.image.revision` label is the commit (`GITHUB_SHA` by default), tags the service repository (`GITHUB_REPOSITORY` by default), compares the tag with the pin, closes open bumps of the app with a lower version, pushes the bump branch and opens the pull request, saves the release state with the post, and enables auto-merge.

Prints `outcome=bump`, `outcome=same` (the pin already has this tag) or `outcome=superseded` (a bump of a higher version is open), then `tag=` and, when there is a pull request, `pr=` and `url=`. For `superseded` they belong to the newer bump.

Needs `GITHUB_TOKEN` (the service) and `CUTSHIP_DEPLOY_TOKEN` (the deploy repository).

## lane

```
cutship lane [--config cutship.yaml] --pr <n> [--repo owner/name]
cutship lane [--config cutship.yaml] < files.json
```

Prints `fast` when the pull request changes one pin file and only its `image` line, to a strictly higher version of the same image (in `release.tag_format` for own images, an exact tag other than `latest` with a higher numeric prefix for others). Prints `full` otherwise. Without `--pr` it reads the GitHub list of pull request files as JSON from stdin.

## deploy plan

```
cutship deploy plan --base <sha> [--head <sha>] [--env <env>] [--config cutship.yaml] [--repo owner/name]
```

Prints `apps=<json array>`, `skip_ci=true|false` and `change=<pull request number or 0>`. `--base` is the commit before the push (`github.event.before`; all zeros on a new branch is treated as head), `--head` defaults to `GITHUB_SHA`, `--env` to the first entry of `environments`.

An app is in the plan when its directory under `apps/` has a different tree at head than at its last successful deploy (or at `--base` when it was never deployed), or when its last deploy failed, and its `app.yml` still exists. `skip_ci` is true only when head comes from a merged bump, the latest `lanes.required_check` on the bump is green, and no app was left undeployed or failed before this push.

## deploy start

```
cutship deploy start --apps <json array> [--sha <sha>] [--env <env>] [--config cutship.yaml] [--repo owner/name]
```

Marks each app as deploying in the ledger (a GitHub Deployment in environment `<env>/<app>`) and moves its post to 🔀 merged and 🚀 deploying.

## deploy finish

```
cutship deploy finish --apps <json array> --result success|failure [--report deploy-report.json] [--sha <sha>] [--env <env>] [--config cutship.yaml] [--repo owner/name]
```

Records the result in the ledger and ends the post with ✅ or ❌. The optional report lists the hosts:

```json
{
  "version": 1,
  "started_at": "2026-10-19T12:00:00Z",
  "finished_at": "2026-10-19T12:01:30Z",
  "hosts": { "mon-1": { "ok": 12, "changed": 2, "failed": 0, "unreachable": 0, "skipped": 3 } },
  "failure": { "host": "mon-1", "task": "restart", "message": "..." }
}
```

Hosts with at most one `ok` task and nothing changed, failed or unreachable are left out. When no host changed anything, the post says "no changes".

## notify

```
cutship notify [--repo owner/name]
```

Reads the event from `GITHUB_EVENT_NAME` and `GITHUB_EVENT_PATH`. Handles `pull_request` opened, reopened and closed on bump branches, and `workflow_run` of `lanes.ci_workflow` completed for a bump. Other events do nothing.

## pin, bump, gate

Low-level helpers that the commands above use; they stay for scripts and debugging.

```
cutship pin read [--root .] <app>
cutship pin check [--root .] <app> <tag>
cutship pin set [--root .] <app> <tag>
cutship pin verify [--root .]
cutship bump name <app> <tag>
cutship bump parse <ref>
cutship bump superseded <app> <tag> < branches
cutship gate head --pulls <file>
cutship gate skip --pulls <file> --checks <file> --pending <json|unknown>
```

`pin check` prints `bump` or `same` and fails when the tag is lower or not in the release format. `pin set` changes only the `image` line and keeps every other byte, including line endings. `pin verify` checks every `apps/*/app.yml`.
