package fake

import (
	"context"
	"fmt"
	"iter"
	"time"

	"github.com/ambrdk/cutship/forge"
)

var _ forge.Deployments = (*Forge)(nil)

type deploymentRecord struct {
	forge.Deployment
	repo    string
	payload []byte
	logURL  string
}

func (f *Forge) CreateDeployment(_ context.Context, name, environment, sha string, payload []byte) (forge.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("CreateDeployment"); err != nil {
		return forge.Deployment{}, err
	}
	f.seq++
	d := &deploymentRecord{
		Deployment: forge.Deployment{ID: int64(f.seq), Environment: environment, SHA: sha, State: forge.DeployInProgress, CreatedAt: time.Unix(int64(f.seq), 0).UTC(), Payload: payload},
		repo:       name,
		payload:    payload,
	}
	f.deployments = append(f.deployments, d)
	return d.Deployment, nil
}

func (f *Forge) SetDeploymentState(_ context.Context, name string, id int64, state forge.DeployState, logURL string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("SetDeploymentState"); err != nil {
		return err
	}
	for _, d := range f.deployments {
		if d.repo == name && d.ID == id {
			d.State, d.logURL = state, logURL
			return nil
		}
	}
	return fmt.Errorf("deployment %d: %w", id, forge.ErrNotFound)
}

func (f *Forge) ListDeployments(_ context.Context, name, environment string) iter.Seq2[forge.Deployment, error] {
	f.mu.Lock()
	err := f.enter("ListDeployments")
	var list []forge.Deployment
	for i := len(f.deployments) - 1; i >= 0; i-- {
		d := f.deployments[i]
		if d.repo == name && d.Environment == environment {
			list = append(list, d.Deployment)
		}
	}
	f.mu.Unlock()
	return func(yield func(forge.Deployment, error) bool) {
		if err != nil {
			yield(forge.Deployment{}, err)
			return
		}
		for _, d := range list {
			if !yield(d, nil) {
				return
			}
		}
	}
}

func (f *Forge) Deployed(name, environment string) []forge.Deployment {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []forge.Deployment
	for _, d := range f.deployments {
		if d.repo == name && d.Environment == environment {
			out = append(out, d.Deployment)
		}
	}
	return out
}

func (f *Forge) DeploymentLog(name string, id int64) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.deployments {
		if d.repo == name && d.ID == id {
			return d.logURL
		}
	}
	return ""
}
