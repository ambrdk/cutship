package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ambrdk/ship/event"
	"github.com/ambrdk/ship/github"
)

type Anchored struct {
	PR    int
	Event event.Event
}

type prPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number         int          `json:"number"`
		Title          string       `json:"title"`
		HTMLURL        string       `json:"html_url"`
		CreatedAt      time.Time    `json:"created_at"`
		UpdatedAt      time.Time    `json:"updated_at"`
		ClosedAt       *time.Time   `json:"closed_at"`
		Merged         bool         `json:"merged"`
		MergeCommitSHA string       `json:"merge_commit_sha"`
		MergedBy       *github.User `json:"merged_by"`
		Head           github.Ref   `json:"head"`
		Base           github.Ref   `json:"base"`
	} `json:"pull_request"`
}

type workflowRun struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Event        string    `json:"event"`
	Conclusion   string    `json:"conclusion"`
	HeadSHA      string    `json:"head_sha"`
	RunAttempt   int       `json:"run_attempt"`
	HTMLURL      string    `json:"html_url"`
	RunStartedAt time.Time `json:"run_started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	PullRequests []struct {
		Number int        `json:"number"`
		Head   github.Ref `json:"head"`
	} `json:"pull_requests"`
}

type runPayload struct {
	Action      string      `json:"action"`
	WorkflowRun workflowRun `json:"workflow_run"`
}

type parser struct {
	api  *github.API
	repo string
}

func Parse(ctx context.Context, api *github.API, repo, eventName string, payload []byte) ([]Anchored, error) {
	p := parser{api: api, repo: repo}
	switch eventName {
	case "pull_request":
		var pl prPayload
		if err := json.Unmarshal(payload, &pl); err != nil {
			return nil, fmt.Errorf("decode pull_request payload: %w", err)
		}
		return p.pullRequest(ctx, pl)
	case "workflow_run":
		var pl runPayload
		if err := json.Unmarshal(payload, &pl); err != nil {
			return nil, fmt.Errorf("decode workflow_run payload: %w", err)
		}
		switch pl.WorkflowRun.Name {
		case "ci":
			return p.ci(pl), nil
		case "deploy":
			return p.deploy(ctx, pl)
		}
	}
	return nil, nil
}

func (p parser) pullRequest(ctx context.Context, pl prPayload) ([]Anchored, error) {
	pr := pl.PullRequest
	app, _, ok := bumpBranch(pr.Head.Ref)
	if !ok {
		return nil, nil
	}
	e := event.Event{App: app, Title: pr.Title, PR: pr.Number, PRURL: pr.HTMLURL, ID: fmt.Sprintf("pull_request:%s:%d:%s", pl.Action, pr.Number, pr.Head.SHA)}
	switch {
	case pl.Action == "opened":
		e.Kind, e.At = event.ReleaseOpened, pr.CreatedAt
	case pl.Action == "reopened":
		e.Kind, e.At = event.Reopened, pr.UpdatedAt
	case pl.Action == "closed" && pr.Merged:
		e.Kind, e.At, e.Commit = event.Merged, closedAt(pr.ClosedAt, pr.UpdatedAt), pr.MergeCommitSHA
		if pr.MergedBy != nil {
			e.Actor = pr.MergedBy.Login
		}
	case pl.Action == "closed":
		e.Kind, e.At = event.ClosedUnmerged, closedAt(pr.ClosedAt, pr.UpdatedAt)
	default:
		return nil, nil
	}
	var err error
	if e.Image, e.To, err = p.image(ctx, app, pr.Head.SHA); err != nil {
		return nil, err
	}
	if _, e.From, err = p.image(ctx, app, pr.Base.SHA); err != nil {
		return nil, err
	}
	return []Anchored{{PR: pr.Number, Event: e}}, nil
}

func (p parser) ci(pl runPayload) []Anchored {
	run := pl.WorkflowRun
	if pl.Action != "completed" || run.Event != "pull_request" || len(run.PullRequests) == 0 {
		return nil
	}
	pr := run.PullRequests[0]
	if _, _, ok := bumpBranch(pr.Head.Ref); !ok {
		return nil
	}
	result := event.Failure
	if run.Conclusion == "success" {
		result = event.Success
	}
	return []Anchored{{PR: pr.Number, Event: event.Event{Kind: event.ChecksFinished, ID: fmt.Sprintf("ci:%d:%d", run.ID, run.RunAttempt), At: run.UpdatedAt, Result: result}}}
}

func (p parser) deploy(ctx context.Context, pl runPayload) ([]Anchored, error) {
	run := pl.WorkflowRun
	base := event.Event{RunURL: run.HTMLURL, Attempt: run.RunAttempt}
	if pl.Action == "requested" {
		anchor, err := p.commitAnchor(ctx, run.Event, run.HeadSHA)
		if err != nil || anchor.PR == 0 {
			return nil, err
		}
		e := anchor.Event
		e.Kind, e.ID, e.At, e.RunURL, e.Attempt = event.DeployStarted, fmt.Sprintf("deploy:%d:%d:start", run.ID, run.RunAttempt), run.RunStartedAt, run.HTMLURL, run.RunAttempt
		return []Anchored{{PR: anchor.PR, Event: e}}, nil
	}
	if pl.Action != "completed" {
		return nil, nil
	}
	jobs, err := p.api.RunJobs(ctx, p.repo, run.ID)
	if err != nil {
		return nil, err
	}
	result := map[string]event.Result{"success": event.Success, "cancelled": event.Cancelled}[run.Conclusion]
	if result == "" {
		result = event.Failure
	}
	if failedJob := failedBeforeDeploy(jobs); failedJob != "" {
		return p.single(ctx, run, event.Failure, failedJob+" job", false)
	}
	if !deployRan(jobs) {
		if result == event.Success {
			return p.single(ctx, run, event.Success, "", true)
		}
		return p.single(ctx, run, result, stoppedJob(jobs)+" job", false)
	}
	files, err := p.api.RunArtifact(ctx, p.repo, run.ID, "deploy-report")
	if errors.Is(err, github.ErrNotFound) {
		return p.single(ctx, run, result, "deploy job", false)
	}
	if err != nil {
		return nil, err
	}
	var extra extraVars
	if json.Unmarshal(files["extra.json"], &extra) != nil || len(extra.DeployApps) == 0 {
		return p.single(ctx, run, result, "deploy job", false)
	}
	var rep Report
	if raw, ok := files["deploy-report.json"]; ok {
		if err := json.Unmarshal(raw, &rep); err != nil {
			return nil, fmt.Errorf("decode deploy-report.json: %w", err)
		}
	}
	commit, err := p.commitAnchor(ctx, run.Event, run.HeadSHA)
	if err != nil {
		return nil, err
	}
	var out []Anchored
	for _, app := range extra.DeployApps {
		e := base
		e.Kind, e.ID, e.At, e.StartedAt, e.App, e.Result, e.Hosts = event.DeployFinished, fmt.Sprintf("deploy:%d:%d:%s", run.ID, run.RunAttempt, app), run.UpdatedAt, run.RunStartedAt, app, result, rep.hosts()
		if e.Image, e.To, err = p.image(ctx, app, run.HeadSHA); err != nil {
			return nil, err
		}
		if result == event.Failure {
			e.FailedTask = "deploy job"
			if rep.Failure != nil {
				e.FailedTask, e.Error = rep.Failure.Task, rep.Failure.Message
			}
		}
		e.NoChanges = result == event.Success && noChanges(e.Hosts)
		pr, err := p.appAnchor(ctx, run.Event, commit, app, e.To)
		if err != nil {
			return nil, err
		}
		out = append(out, Anchored{PR: pr, Event: e})
	}
	return out, nil
}

func (p parser) single(ctx context.Context, run workflowRun, result event.Result, task string, emptyPlan bool) ([]Anchored, error) {
	anchor, err := p.commitAnchor(ctx, run.Event, run.HeadSHA)
	if err != nil {
		return nil, err
	}
	if emptyPlan && anchor.PR == 0 {
		return nil, nil
	}
	e := anchor.Event
	e.Kind, e.ID, e.At, e.StartedAt, e.RunURL, e.Attempt, e.Result = event.DeployFinished, fmt.Sprintf("deploy:%d:%d:run", run.ID, run.RunAttempt), run.UpdatedAt, run.RunStartedAt, run.HTMLURL, run.RunAttempt, result
	e.NoChanges = emptyPlan
	if result == event.Failure {
		e.FailedTask = task
	}
	return []Anchored{{PR: anchor.PR, Event: e}}, nil
}

func (p parser) commitAnchor(ctx context.Context, trigger, sha string) (Anchored, error) {
	if trigger != "push" {
		return Anchored{}, nil
	}
	pulls, err := p.api.PullsForCommit(ctx, p.repo, sha)
	if err != nil {
		return Anchored{}, err
	}
	for _, pr := range pulls {
		if pr.MergedAt == nil {
			continue
		}
		e := event.Event{PR: pr.Number, PRURL: pr.HTMLURL, Title: pr.Title}
		if app, _, ok := bumpBranch(pr.Head.Ref); ok {
			e.App = app
			if e.Image, e.To, err = p.image(ctx, app, sha); err != nil {
				return Anchored{}, err
			}
		}
		return Anchored{PR: pr.Number, Event: e}, nil
	}
	return Anchored{}, nil
}

func (p parser) appAnchor(ctx context.Context, trigger string, commit Anchored, app, tag string) (int, error) {
	if trigger != "push" {
		return 0, nil
	}
	if commit.PR != 0 {
		if commit.Event.App == app {
			return commit.PR, nil
		}
		if commit.Event.App == "" {
			files, err := p.api.PullFiles(ctx, p.repo, commit.PR)
			if err != nil {
				return 0, err
			}
			for _, f := range files {
				if strings.HasPrefix(f, "apps/"+app+"/") {
					return commit.PR, nil
				}
			}
		}
	}
	if tag != "" {
		owner, _, _ := strings.Cut(p.repo, "/")
		pulls, err := p.api.PullsByHead(ctx, p.repo, owner+":bump/"+app+"-"+tag)
		if err != nil {
			return 0, err
		}
		for _, pr := range pulls {
			if pr.MergedAt != nil {
				return pr.Number, nil
			}
		}
	}
	return 0, nil
}

func (p parser) image(ctx context.Context, app, ref string) (string, string, error) {
	body, err := p.api.FileAt(ctx, p.repo, "apps/"+app+"/app.yml", ref)
	if errors.Is(err, github.ErrNotFound) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	repository, tag := imageOf(body)
	return repository, tag, nil
}

func failedBeforeDeploy(jobs []github.Job) string {
	for _, j := range jobs {
		stage, _, _ := strings.Cut(j.Name, " / ")
		if (stage == "ci" || stage == "plan") && (j.Conclusion == "failure" || j.Conclusion == "timed_out") {
			return stage
		}
	}
	return ""
}

func stoppedJob(jobs []github.Job) string {
	for _, j := range jobs {
		if j.Conclusion != "success" && j.Conclusion != "skipped" {
			stage, _, _ := strings.Cut(j.Name, " / ")
			return stage
		}
	}
	return "deploy run"
}

func deployRan(jobs []github.Job) bool {
	for _, j := range jobs {
		stage, _, _ := strings.Cut(j.Name, " / ")
		if stage == "deploy" && j.Conclusion != "skipped" && j.Conclusion != "" {
			return true
		}
	}
	return false
}

func noChanges(hosts []event.Host) bool {
	for _, h := range hosts {
		if h.Changed > 0 {
			return false
		}
	}
	return true
}

func closedAt(closed *time.Time, fallback time.Time) time.Time {
	if closed != nil {
		return *closed
	}
	return fallback
}
