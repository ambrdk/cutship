# cutship context

**Release cycle.** A merge in a service releases a version, a bump pull request in the deploy repository pins it, its merge deploys it, and a Telegram post follows the whole way. cutship holds the rules of that cycle; the workflows only call it.

**Pin.** The `image` line of `apps/<app>/app.yml` in the deploy repository (package `pin`). Own images (`ghcr.io/ambrdk/*`) are pinned to `vMAJOR.MINOR.PATCH`; other images to any exact tag except `latest`, compared by their numeric prefix. `pin set` changes that line and nothing else.

**Bump.** A pull request in the deploy repository that changes one pin and nothing else (package `bump`). Branch `bump/<app>-vX.Y.Z`, title `chore(apps): bump <app> to vX.Y.Z`. A newer bump of the same app supersedes the older open ones.

**Lane.** The CI path of a deploy repository pull request: `fast` when the only change is one pin moving to a strictly higher version in the same image repository, `full` otherwise (package `pin`, `ImageOnly`).

**Gate.** Whether a deploy can skip its own CI run: only for a merged bump whose latest `lint` check is green and when no app was left undeployed before it (package `gate`).

**Notify.** The Telegram post of a release and its comment, kept in sync with GitHub events (packages `source`, `release`, `render`, `store`, `notify`).

**Interface.** The commands, their output and their exit codes are versioned: an incompatible change needs a new major version. `ci-templates` calls `ambrdk/cutship/setup@v1` with an exact `version`, which fails the job when that release cannot be installed. The deploy repository pins `setup` and the notify action by SHA; without a `version` input, setup falls back to building the pinned ref from source.

The deploy repository's own `CONTEXT.md` describes the same Pin, Bump and Lane from its side.
