package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ambrdk/cutship/bump"
	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/event"
	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/pin"
	"github.com/ambrdk/cutship/post"
)

type Item struct {
	Target post.Target
	Event  event.Event
}

type user struct {
	Login string `json:"login"`
}

type ref struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type pullPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number         int        `json:"number"`
		Title          string     `json:"title"`
		HTMLURL        string     `json:"html_url"`
		CreatedAt      time.Time  `json:"created_at"`
		UpdatedAt      time.Time  `json:"updated_at"`
		ClosedAt       *time.Time `json:"closed_at"`
		Merged         bool       `json:"merged"`
		MergeCommitSHA string     `json:"merge_commit_sha"`
		MergedBy       *user      `json:"merged_by"`
		User           user       `json:"user"`
		Head           ref        `json:"head"`
		Base           ref        `json:"base"`
	} `json:"pull_request"`
}

type runPayload struct {
	Action      string `json:"action"`
	WorkflowRun struct {
		ID           int64     `json:"id"`
		Name         string    `json:"name"`
		Event        string    `json:"event"`
		Conclusion   string    `json:"conclusion"`
		RunAttempt   int       `json:"run_attempt"`
		UpdatedAt    time.Time `json:"updated_at"`
		PullRequests []struct {
			Number int `json:"number"`
			Head   ref `json:"head"`
		} `json:"pull_requests"`
	} `json:"workflow_run"`
}

func Parse(ctx context.Context, f forge.Reader, cfg config.Config, repo, eventName string, payload []byte) ([]Item, error) {
	switch eventName {
	case "pull_request":
		var pl pullPayload
		if err := json.Unmarshal(payload, &pl); err != nil {
			return nil, fmt.Errorf("decode pull_request payload: %w", err)
		}
		return pullRequest(ctx, f, cfg, repo, pl)
	case "workflow_run":
		var pl runPayload
		if err := json.Unmarshal(payload, &pl); err != nil {
			return nil, fmt.Errorf("decode workflow_run payload: %w", err)
		}
		return workflowRun(ctx, f, cfg, repo, pl)
	}
	return nil, nil
}

func pinned(ctx context.Context, f forge.Reader, repo string, app config.App, sha string) (string, string, error) {
	content, err := f.FileAt(ctx, repo, pin.Path(app), sha)
	if errors.Is(err, forge.ErrNotFound) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	line, ok := pin.Line(content)
	if !ok {
		return "", "", nil
	}
	image, tag := pin.ParseRef(line)
	return image, tag, nil
}

func pullRequest(ctx context.Context, f forge.Reader, cfg config.Config, repo string, pl pullPayload) ([]Item, error) {
	pr := pl.PullRequest
	app, tag, ok := bump.Parse(cfg.Release, pr.Head.Ref)
	if !ok {
		return nil, nil
	}
	ch := forge.Change{Number: pr.Number, Title: pr.Title, URL: pr.HTMLURL, HeadRef: pr.Head.Ref, HeadSHA: pr.Head.SHA, MergeSHA: pr.MergeCommitSHA, Author: pr.User.Login}
	e := event.Event{App: app, To: tag, Title: pr.Title, PR: pr.Number, PRURL: pr.HTMLURL}
	closed := pr.UpdatedAt
	if pr.ClosedAt != nil {
		closed = *pr.ClosedAt
	}
	switch {
	case pl.Action == "opened":
		e.Kind, e.At, e.ID = event.ReleaseOpened, pr.CreatedAt, fmt.Sprintf("release:%d:%s", pr.Number, pr.Head.SHA)
	case pl.Action == "reopened":
		e.Kind, e.At, e.ID = event.Reopened, pr.UpdatedAt, fmt.Sprintf("pull_request:reopened:%d:%s", pr.Number, pr.Head.SHA)
	case pl.Action == "closed" && pr.Merged:
		e.Kind, e.At, e.ID, e.Commit = event.Merged, closed, fmt.Sprintf("pull_request:closed:%d:%s", pr.Number, pr.Head.SHA), pr.MergeCommitSHA
		if pr.MergedBy != nil {
			e.Actor = pr.MergedBy.Login
		}
	case pl.Action == "closed":
		e.Kind, e.At, e.ID = event.ClosedUnmerged, closed, fmt.Sprintf("pull_request:closed:%d:%s", pr.Number, pr.Head.SHA)
	default:
		return nil, nil
	}
	if a, ok := cfg.App(app); ok {
		image, _, err := pinned(ctx, f, repo, a, pr.Head.SHA)
		if err != nil {
			return nil, err
		}
		_, from, err := pinned(ctx, f, repo, a, pr.Base.SHA)
		if err != nil {
			return nil, err
		}
		e.Image, e.From = image, from
	}
	return []Item{{Target: post.Target{Change: ch, App: app, Env: cfg.Environments[0], Tag: tag}, Event: e}}, nil
}

func workflowRun(ctx context.Context, f forge.Reader, cfg config.Config, repo string, pl runPayload) ([]Item, error) {
	run := pl.WorkflowRun
	if pl.Action != "completed" || run.Name != cfg.Lanes.CIWorkflow || run.Event != "pull_request" || len(run.PullRequests) == 0 {
		return nil, nil
	}
	pr := run.PullRequests[0]
	app, tag, ok := bump.Parse(cfg.Release, pr.Head.Ref)
	if !ok {
		return nil, nil
	}
	ch, found, err := f.ChangeByHead(ctx, repo, pr.Head.Ref)
	if err != nil {
		return nil, err
	}
	if !found || ch.Number != pr.Number {
		ch = forge.Change{Number: pr.Number, HeadRef: pr.Head.Ref, HeadSHA: pr.Head.SHA}
	}
	result := event.Failure
	if run.Conclusion == "success" {
		result = event.Success
	}
	e := event.Event{Kind: event.ChecksFinished, ID: fmt.Sprintf("ci:%d:%d", run.ID, run.RunAttempt), At: run.UpdatedAt, App: app, Result: result, PR: pr.Number}
	return []Item{{Target: post.Target{Change: ch, App: app, Env: cfg.Environments[0], Tag: tag}, Event: e}}, nil
}

func Handle(ctx context.Context, items []Item, p *post.Publisher) error {
	var errs []error
	for _, it := range items {
		if _, err := p.Apply(ctx, it.Target, it.Event); err != nil {
			errs = append(errs, fmt.Errorf("event %s: %w", it.Event.ID, err))
		}
	}
	return errors.Join(errs...)
}
