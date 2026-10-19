package ledger

import (
	"context"
	"encoding/json"

	"github.com/ambrdk/cutship/forge"
)

type Entry struct {
	App   string
	SHA   string
	State forge.DeployState
	ID    int64
}

func Environment(env, app string) string {
	return env + "/" + app
}

func entry(app string, d forge.Deployment) Entry {
	return Entry{App: app, SHA: d.SHA, State: d.State, ID: d.ID}
}

func first(ctx context.Context, d forge.Deployments, repo, env, app string, match func(forge.Deployment) bool) (Entry, bool, error) {
	for dep, err := range d.ListDeployments(ctx, repo, Environment(env, app)) {
		if err != nil {
			return Entry{}, false, err
		}
		if match(dep) {
			return entry(app, dep), true, nil
		}
	}
	return Entry{}, false, nil
}

func LastSuccess(ctx context.Context, d forge.Deployments, repo, env, app string) (Entry, bool, error) {
	return first(ctx, d, repo, env, app, func(dep forge.Deployment) bool { return dep.State == forge.DeploySuccess })
}

func Latest(ctx context.Context, d forge.Deployments, repo, env, app string) (Entry, bool, error) {
	return first(ctx, d, repo, env, app, func(forge.Deployment) bool { return true })
}

func running(ctx context.Context, d forge.Deployments, repo, env, app, sha string) (Entry, bool, error) {
	latest, ok, err := Latest(ctx, d, repo, env, app)
	if err != nil || !ok || latest.SHA != sha || latest.State != forge.DeployInProgress {
		return Entry{}, false, err
	}
	return latest, true, nil
}

func create(ctx context.Context, d forge.Deployments, repo, env, app, sha string) (Entry, error) {
	payload, err := json.Marshal(map[string]string{"app": app, "env": env})
	if err != nil {
		return Entry{}, err
	}
	dep, err := d.CreateDeployment(ctx, repo, Environment(env, app), sha, payload)
	if err != nil {
		return Entry{}, err
	}
	return entry(app, dep), nil
}

func Start(ctx context.Context, d forge.Deployments, repo, env, app, sha, logURL string) (Entry, error) {
	e, ok, err := running(ctx, d, repo, env, app, sha)
	if err != nil {
		return Entry{}, err
	}
	if !ok {
		if e, err = create(ctx, d, repo, env, app, sha); err != nil {
			return Entry{}, err
		}
	}
	if err := d.SetDeploymentState(ctx, repo, e.ID, forge.DeployInProgress, logURL); err != nil {
		return Entry{}, err
	}
	e.State = forge.DeployInProgress
	return e, nil
}

func Finish(ctx context.Context, d forge.Deployments, repo, env, app, sha string, succeeded bool, logURL string) (Entry, error) {
	e, ok, err := running(ctx, d, repo, env, app, sha)
	if err != nil {
		return Entry{}, err
	}
	if !ok {
		if e, err = create(ctx, d, repo, env, app, sha); err != nil {
			return Entry{}, err
		}
	}
	state := forge.DeployFailure
	if succeeded {
		state = forge.DeploySuccess
	}
	if err := d.SetDeploymentState(ctx, repo, e.ID, state, logURL); err != nil {
		return Entry{}, err
	}
	e.State = state
	return e, nil
}
