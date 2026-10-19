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
	Data  json.RawMessage
}

type payload struct {
	App  string          `json:"app"`
	Env  string          `json:"env"`
	Data json.RawMessage `json:"data,omitempty"`
}

func Environment(env, app string) string {
	return env + "/" + app
}

func entry(app string, d forge.Deployment) Entry {
	e := Entry{App: app, SHA: d.SHA, State: d.State, ID: d.ID}
	var p payload
	if len(d.Payload) > 0 && json.Unmarshal(d.Payload, &p) == nil {
		e.Data = p.Data
	}
	return e
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

func History(ctx context.Context, d forge.Deployments, repo, env, app string) (latest, success *Entry, err error) {
	for dep, err := range d.ListDeployments(ctx, repo, Environment(env, app)) {
		if err != nil {
			return nil, nil, err
		}
		e := entry(app, dep)
		if latest == nil {
			latest = &e
		}
		if dep.State == forge.DeploySuccess {
			return latest, &e, nil
		}
	}
	return latest, nil, nil
}

func LastSuccess(ctx context.Context, d forge.Deployments, repo, env, app string) (Entry, bool, error) {
	return first(ctx, d, repo, env, app, func(dep forge.Deployment) bool { return dep.State == forge.DeploySuccess })
}

func Latest(ctx context.Context, d forge.Deployments, repo, env, app string) (Entry, bool, error) {
	return first(ctx, d, repo, env, app, func(forge.Deployment) bool { return true })
}

func Running(ctx context.Context, d forge.Deployments, repo, env, app, sha string) (Entry, bool, error) {
	latest, ok, err := Latest(ctx, d, repo, env, app)
	if err != nil || !ok || latest.SHA != sha || latest.State != forge.DeployInProgress {
		return Entry{}, false, err
	}
	return latest, true, nil
}

func create(ctx context.Context, d forge.Deployments, repo, env, app, sha string, data json.RawMessage) (Entry, error) {
	raw, err := json.Marshal(payload{App: app, Env: env, Data: data})
	if err != nil {
		return Entry{}, err
	}
	dep, err := d.CreateDeployment(ctx, repo, Environment(env, app), sha, raw)
	if err != nil {
		return Entry{}, err
	}
	e := entry(app, dep)
	e.Data = data
	return e, nil
}

func set(ctx context.Context, d forge.Deployments, repo, env, app, sha string, state forge.DeployState, logURL string, data json.RawMessage) (Entry, error) {
	e, ok, err := Running(ctx, d, repo, env, app, sha)
	if err != nil {
		return Entry{}, err
	}
	if !ok {
		if e, err = create(ctx, d, repo, env, app, sha, data); err != nil {
			return Entry{}, err
		}
	}
	if err := d.SetDeploymentState(ctx, repo, e.ID, state, logURL); err != nil {
		return Entry{}, err
	}
	e.State = state
	return e, nil
}

func Start(ctx context.Context, d forge.Deployments, repo, env, app, sha, logURL string) (Entry, error) {
	return StartWith(ctx, d, repo, env, app, sha, logURL, nil)
}

func StartWith(ctx context.Context, d forge.Deployments, repo, env, app, sha, logURL string, data json.RawMessage) (Entry, error) {
	return set(ctx, d, repo, env, app, sha, forge.DeployInProgress, logURL, data)
}

func Finish(ctx context.Context, d forge.Deployments, repo, env, app, sha string, succeeded bool, logURL string) (Entry, error) {
	return FinishWith(ctx, d, repo, env, app, sha, succeeded, logURL, nil)
}

func FinishWith(ctx context.Context, d forge.Deployments, repo, env, app, sha string, succeeded bool, logURL string, data json.RawMessage) (Entry, error) {
	state := forge.DeployFailure
	if succeeded {
		state = forge.DeploySuccess
	}
	return set(ctx, d, repo, env, app, sha, state, logURL, data)
}
