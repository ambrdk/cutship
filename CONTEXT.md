# cutship context

**Release cycle.** A merge in a service releases a version, a bump pull request in the deploy repository pins it, its merge deploys it, and one post per release follows the whole way. cutship holds the rules of that cycle; the workflows only call it.

**Config.** `cutship.yaml` in the root of the deploy repository (package `config`). Unknown fields, typos and values planned for later versions are errors with the field's path and line. `cutship schema` prints its JSON Schema.

**App.** An app of the deploy repository: a name, its own image and where the image is pinned. Images listed under `apps` are the project's own; every other image is foreign.

**Pin.** Where the deploy repository fixes an image tag (package `pin`). The `ansible` preset is the `image` line of `apps/<app>/app.yml`. Own images are pinned to tags in `release.tag_format`; foreign images to any exact tag except `latest`, compared by their numeric prefix. Changing a pin keeps every other byte of the file.

**Release.** A version of an app: the git tag in the service, the image with that tag, the bump and the deploy (package `cut`, command `cutship release`). The image must carry the service commit in `org.opencontainers.image.revision`.

**Bump.** A pull request in the deploy repository that changes one pin and nothing else (package `bump`). Branch `<bump_branch_prefix><app>-<tag>`, title from `release.bump_title`. A newer bump of the same app supersedes older open ones.

**Lane.** The CI path of a deploy repository pull request: `fast` when the only change is one pin moving to a strictly higher version of the same image, `full` otherwise (`pin.ImageOnly`, command `cutship lane`).

**Gate.** Whether a deploy can skip its own CI run: only for a merged bump whose latest `lanes.required_check` is green and when no app was left undeployed or failed before it (package `gate`).

**Ledger.** What is deployed where: one GitHub Deployment environment `<env>/<app>` per app (package `ledger`). `cutship deploy plan` reads it to choose the apps to deploy; `deploy start` and `deploy finish` write it.

**State.** The data of a release between pipelines: stage, timeline, ids of the messages. It lives in a hidden `<!-- cutship:v1 -->` comment on the bump, on the merged pull request of a non-bump deploy, or in the payload of the Deployment of a direct push (package `state`).

**Post.** The message about a release in each configured messenger, built from a neutral view (package `render`) and published by a notifier (packages `notifier`, `post`). Every command that moves a release publishes its post.

**Interface.** The commands, their output and their exit codes are versioned: until 1.0 they may change in a minor version, always with an error that says what to do. The `ambrdk/cutship` Action installs a verified release of the version it is pinned to and never builds from source.
