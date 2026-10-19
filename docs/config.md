# cutship.yaml reference

`cutship.yaml` lives in the root of the deploy repository. This page is generated from the JSON Schema (`go run ./internal/docgen > docs/config.md`); `cutship schema` prints the same schema for editors, and `cutship validate` checks a file against the same rules with line numbers.

| Field | Type | Required | Description |
|---|---|---|---|
| `version` | integer | yes | Version of this file format. Only 1 exists. Allowed now: `1`. |
| `forge` | object | yes | Where the deploy repository and the service repositories live. |
| `forge.type` | string | yes | Forge type. gitlab is planned. Tokens come from GITHUB_TOKEN and, for release from a service, CUTSHIP_DEPLOY_TOKEN. Allowed now: `github`. |
| `forge.url` | string | no | API base URL for a self-hosted forge. Empty means the public one. |
| `deploy_repo` | string | yes | Deploy repository as owner/name. Bump pull requests go there. |
| `environments` | list of strings | no | Environments the apps are deployed to. Default [prod]. Promotion between environments is planned. |
| `apps` | list of objects | yes | Apps whose images this deploy repository pins. |
| `apps[].name` | string | yes | App name, also the name of the bump branch and of the pin preset path. |
| `apps[].image` | string | yes | Image repository without a tag or digest. Tags of this image must follow release.tag_format. |
| `apps[].tag_prefix` | string | no | Tag prefix of the app in a monorepo. Not supported yet, planned; must be empty. |
| `apps[].pin` | object | yes | Where the deploy repository pins the image tag. |
| `apps[].pin.preset` | string | yes | Known layout. ansible: the image key of apps/&lt;app&gt;/app.yml. helm, kustomize and compose are planned. Allowed now: `ansible`. |
| `apps[].pin.file` | string | no | File that holds the pin, relative to the deploy repository. Not supported yet, planned; must be empty. |
| `apps[].pin.path` | string | no | YAML path of the pin inside file. Not supported yet, planned; must be empty. |
| `release` | object | no | How releases are tagged and how bump pull requests are named. |
| `release.versioning` | string | no | How the next version is chosen. conventional: from conventional commit subjects since the last release tag. Allowed now: `conventional`. |
| `release.tag_format` | string | no | Release tag of git and of the image, with {version} exactly once. Default v{version}. |
| `release.bump_branch_prefix` | string | no | Prefix of bump branches, ending with /. Default cutship/. |
| `release.bump_title` | string | no | Title of bump pull requests with {app} and {tag}. Default chore(apps): bump {app} to {tag}. |
| `lanes` | object | no | When a bump pull request may take the fast lane. |
| `lanes.required_check` | string | no | Check that must be green on the bump pull request before its deploy skips CI. Default lint. |
| `lanes.ci_workflow` | string | no | Name of the workflow whose result cutship notify adds to the release post. Default ci. |
| `notify` | list of objects | no | Messengers that get one post per release. |
| `notify[].type` | string | yes | Messenger. telegram uses the bot token from TELEGRAM_BOT_TOKEN. slack is planned. Allowed now: `telegram`. |
| `notify[].mode` | string | no | Telegram mode. channel (the default): a post in a channel and its comment in the discussion group. topics is planned. Allowed now: `channel`. |
| `notify[].chat_id` | string or integer | yes | Telegram chat id, -100... for a channel. |
| `notify[].topic_id` | integer | no | Telegram topic id for mode topics, planned. Allowed now: `0`. |
| `notify[].channel` | string | no | Slack channel id, planned. |
