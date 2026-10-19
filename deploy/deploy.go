package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"sort"
	"time"

	"github.com/ambrdk/cutship/bump"
	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/event"
	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/gate"
	"github.com/ambrdk/cutship/ledger"
	"github.com/ambrdk/cutship/notifier"
	"github.com/ambrdk/cutship/pin"
	"github.com/ambrdk/cutship/post"
	"github.com/ambrdk/cutship/release"
	"github.com/ambrdk/cutship/render"
)

type Plan struct {
	Apps   []string
	SkipCI bool
	Change int
}

type Run struct {
	Repo      string
	Env       string
	SHA       string
	ID        string
	Attempt   int
	LogURL    string
	Now       time.Time
	Notifiers []notifier.Notifier
	Warn      func(string)
}

func (r Run) warn(s string) {
	if r.Warn != nil {
		r.Warn(s)
	}
}

func checkEnv(cfg config.Config, env string) error {
	if !slices.Contains(cfg.Environments, env) {
		return fmt.Errorf("environment %q is not in cutship.yaml, want one of %v", env, cfg.Environments)
	}
	return nil
}

var appsDir = path.Dir(path.Dir(pin.Path(config.App{Name: "app"})))

type trees struct {
	ctx   context.Context
	f     forge.Reader
	repo  string
	cache map[string]map[string]string
}

func (t *trees) at(ref string) (map[string]string, error) {
	if got, ok := t.cache[ref]; ok {
		return got, nil
	}
	got, err := t.f.Subtrees(t.ctx, t.repo, appsDir, ref)
	if err != nil {
		return nil, fmt.Errorf("list %s/ at %s: %w", appsDir, ref, err)
	}
	t.cache[ref] = got
	return got, nil
}

func (t *trees) differ(app, from, to string) (bool, error) {
	if from == to {
		return false, nil
	}
	a, err := t.at(from)
	if err != nil {
		return false, err
	}
	b, err := t.at(to)
	if err != nil {
		return false, err
	}
	return a[app] != b[app], nil
}

func MakePlan(ctx context.Context, f forge.Forge, d forge.Deployments, cfg config.Config, repo, env, base, head string) (Plan, error) {
	if err := checkEnv(cfg, env); err != nil {
		return Plan{}, err
	}
	t := &trees{ctx: ctx, f: f, repo: repo, cache: map[string]map[string]string{}}
	atHead, err := t.at(head)
	if err != nil {
		return Plan{}, err
	}
	names := make([]string, 0, len(atHead))
	for name := range atHead {
		if pin.ValidApp(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var plan Plan
	pending := []string{}
	for _, name := range names {
		if _, err := f.FileAt(ctx, repo, pin.Path(config.App{Name: name}), head); errors.Is(err, forge.ErrNotFound) {
			continue
		} else if err != nil {
			return Plan{}, err
		}
		latest, success, err := ledger.History(ctx, d, repo, env, name)
		if err != nil {
			return Plan{}, err
		}
		from := base
		if success != nil {
			from = success.SHA
		}
		failed := latest != nil && latest.State == forge.DeployFailure
		changed, err := t.differ(name, from, head)
		if err != nil {
			return Plan{}, err
		}
		before, err := t.differ(name, from, base)
		if err != nil {
			return Plan{}, err
		}
		if failed || before {
			pending = append(pending, name)
		}
		if changed || failed {
			plan.Apps = append(plan.Apps, name)
		}
	}
	changes, err := f.ChangesForCommit(ctx, repo, head)
	if err != nil {
		return Plan{}, err
	}
	merged, ok := gate.Bump(cfg.Release, changes)
	if !ok {
		return plan, nil
	}
	plan.Change = merged.Number
	checks, err := f.Checks(ctx, repo, merged.HeadSHA)
	if err != nil {
		return Plan{}, err
	}
	plan.SkipCI = gate.Skip(cfg.Release, cfg.Lanes, changes, checks, pending)
	return plan, nil
}

type home struct {
	change forge.Change
	tag    string
	found  bool
}

func homeOf(ctx context.Context, f forge.Reader, cfg config.Config, repo, sha, app string) (home, error) {
	changes, err := f.ChangesForCommit(ctx, repo, sha)
	if err != nil {
		return home{}, err
	}
	var plain *forge.Change
	for i, c := range changes {
		if c.MergedAt == nil {
			continue
		}
		other, tag, isBump := bump.Parse(cfg.Release, c.HeadRef)
		if isBump && other == app {
			return home{change: c, tag: tag, found: true}, nil
		}
		if !isBump && plain == nil {
			plain = &changes[i]
		}
	}
	if plain != nil {
		return home{change: *plain, found: true}, nil
	}
	return home{}, nil
}

func merged(ch forge.Change, app string) event.Event {
	return event.Event{
		Kind:   event.Merged,
		ID:     fmt.Sprintf("pull_request:closed:%d:%s", ch.Number, ch.HeadSHA),
		At:     *ch.MergedAt,
		App:    app,
		Actor:  ch.MergedBy,
		Commit: ch.MergeSHA,
		Title:  ch.Title,
		PR:     ch.Number,
		PRURL:  ch.URL,
	}
}

type thread struct {
	State    release.State              `json:"state"`
	Messages map[string]json.RawMessage `json:"messages,omitempty"`
}

func (run Run) unanchored(ctx context.Context, d forge.Deployments, app string, e event.Event) json.RawMessage {
	var t thread
	if running, ok, err := ledger.Running(ctx, d, run.Repo, run.Env, app, run.SHA); err == nil && ok && len(running.Data) > 0 {
		_ = json.Unmarshal(running.Data, &t)
	}
	var changed bool
	t.State, changed = release.Apply(t.State, e)
	refs := post.Send(ctx, run.Notifiers, run.warn, render.Build(t.State), t.Messages, changed)
	if len(refs) > 0 {
		if t.Messages == nil {
			t.Messages = map[string]json.RawMessage{}
		}
		maps.Copy(t.Messages, refs)
	}
	raw, _ := json.Marshal(t)
	return raw
}

func Start(ctx context.Context, f forge.Forge, d forge.Deployments, cfg config.Config, run Run, apps []string) error {
	if err := checkEnv(cfg, run.Env); err != nil {
		return err
	}
	p := post.New(f, run.Repo, run.Notifiers, run.warn)
	for _, app := range apps {
		h, err := homeOf(ctx, f, cfg, run.Repo, run.SHA, app)
		if err != nil {
			return err
		}
		e := event.Event{Kind: event.DeployStarted, ID: "deploy:" + run.ID + ":" + app + ":started", At: run.Now, App: app, Commit: run.SHA, RunURL: run.LogURL, Attempt: run.Attempt}
		if !h.found {
			if _, err := ledger.StartWith(ctx, d, run.Repo, run.Env, app, run.SHA, run.LogURL, run.unanchored(ctx, d, app, e)); err != nil {
				return fmt.Errorf("ledger %s: %w", app, err)
			}
			continue
		}
		if _, err := ledger.Start(ctx, d, run.Repo, run.Env, app, run.SHA, run.LogURL); err != nil {
			return fmt.Errorf("ledger %s: %w", app, err)
		}
		e.PR, e.PRURL = h.change.Number, h.change.URL
		target := post.Target{Change: h.change, App: app, Env: run.Env, Tag: h.tag}
		if _, err := p.Apply(ctx, target, merged(h.change, app), e); err != nil {
			return fmt.Errorf("state of %s: %w", app, err)
		}
	}
	return nil
}

func Finish(ctx context.Context, f forge.Forge, d forge.Deployments, cfg config.Config, run Run, apps []string, succeeded bool, report *Report) error {
	if err := checkEnv(cfg, run.Env); err != nil {
		return err
	}
	result := event.Failure
	if succeeded {
		result = event.Success
	}
	p := post.New(f, run.Repo, run.Notifiers, run.warn)
	for _, app := range apps {
		h, err := homeOf(ctx, f, cfg, run.Repo, run.SHA, app)
		if err != nil {
			return err
		}
		e := event.Event{Kind: event.DeployFinished, ID: "deploy:" + run.ID + ":" + app + ":finished", At: run.Now, App: app, Commit: run.SHA, RunURL: run.LogURL, Attempt: run.Attempt, Result: result}
		if report != nil {
			e.StartedAt, e.Hosts = report.StartedAt, report.Hosts
			e.NoChanges = succeeded && report.NoChanges()
			if report.Failure != nil {
				e.FailedTask, e.Error = report.Failure.Task, report.Failure.Message
			}
		}
		if !h.found {
			if _, err := ledger.FinishWith(ctx, d, run.Repo, run.Env, app, run.SHA, succeeded, run.LogURL, run.unanchored(ctx, d, app, e)); err != nil {
				return fmt.Errorf("ledger %s: %w", app, err)
			}
			continue
		}
		if _, err := ledger.Finish(ctx, d, run.Repo, run.Env, app, run.SHA, succeeded, run.LogURL); err != nil {
			return fmt.Errorf("ledger %s: %w", app, err)
		}
		e.PR, e.PRURL = h.change.Number, h.change.URL
		target := post.Target{Change: h.change, App: app, Env: run.Env, Tag: h.tag}
		if _, err := p.Apply(ctx, target, merged(h.change, app), e); err != nil {
			return fmt.Errorf("state of %s: %w", app, err)
		}
	}
	return nil
}
