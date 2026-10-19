package github

import (
	"context"
	"encoding/json"
	"iter"
	"strings"

	"github.com/ambrdk/cutship/forge"
	gh "github.com/google/go-github/v92/github"
)

func production(environment string) bool {
	first, _, _ := strings.Cut(environment, "/")
	return first == "prod" || first == "production"
}

func deployState(s string) forge.DeployState {
	switch s {
	case "success":
		return forge.DeploySuccess
	case "failure", "error":
		return forge.DeployFailure
	case "inactive":
		return forge.DeployInactive
	}
	return forge.DeployInProgress
}

func deployment(d *gh.Deployment, state forge.DeployState) forge.Deployment {
	return forge.Deployment{
		ID:          d.GetID(),
		Environment: d.GetEnvironment(),
		SHA:         d.GetSHA(),
		State:       state,
		CreatedAt:   d.GetCreatedAt().Time,
		Payload:     d.Payload,
	}
}

func (c *Client) CreateDeployment(ctx context.Context, full, environment, sha string, payload []byte) (forge.Deployment, error) {
	owner, name, err := repo(full)
	if err != nil {
		return forge.Deployment{}, err
	}
	req := gh.DeploymentRequest{
		Ref:                   sha,
		AutoMerge:             new(false),
		RequiredContexts:      []string{},
		Environment:           new(environment),
		TransientEnvironment:  new(false),
		ProductionEnvironment: new(production(environment)),
	}
	if len(payload) > 0 {
		req.Payload = json.RawMessage(payload)
	}
	d, _, err := c.gh.Repositories.CreateDeployment(ctx, owner, name, req)
	if err != nil {
		return forge.Deployment{}, wrap(err)
	}
	return deployment(d, forge.DeployInProgress), nil
}

func (c *Client) SetDeploymentState(ctx context.Context, full string, id int64, state forge.DeployState, logURL string) error {
	owner, name, err := repo(full)
	if err != nil {
		return err
	}
	req := gh.DeploymentStatusRequest{State: string(state), AutoInactive: new(false)}
	if logURL != "" {
		req.LogURL = new(logURL)
	}
	_, _, err = c.gh.Repositories.CreateDeploymentStatus(ctx, owner, name, id, req)
	return wrap(err)
}

func (c *Client) latestState(ctx context.Context, owner, name string, id int64) (forge.DeployState, error) {
	statuses, _, err := c.gh.Repositories.ListDeploymentStatuses(ctx, owner, name, id, &gh.ListOptions{PerPage: 1})
	if err != nil || len(statuses) == 0 {
		return forge.DeployInProgress, wrap(err)
	}
	return deployState(statuses[0].GetState()), nil
}

func (c *Client) ListDeployments(ctx context.Context, full, environment string) iter.Seq2[forge.Deployment, error] {
	return func(yield func(forge.Deployment, error) bool) {
		owner, name, err := repo(full)
		if err != nil {
			yield(forge.Deployment{}, err)
			return
		}
		opts := &gh.DeploymentsListOptions{Environment: environment, ListOptions: gh.ListOptions{PerPage: perPage}}
		for {
			page, resp, err := c.gh.Repositories.ListDeployments(ctx, owner, name, opts)
			if err != nil {
				yield(forge.Deployment{}, wrap(err))
				return
			}
			for _, d := range page {
				state, err := c.latestState(ctx, owner, name, d.GetID())
				if !yield(deployment(d, state), err) || err != nil {
					return
				}
			}
			if resp.NextPage == 0 {
				return
			}
			opts.Page = resp.NextPage
		}
	}
}
