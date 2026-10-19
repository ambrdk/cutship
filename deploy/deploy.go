package deploy

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ambrdk/cutship/bump"
	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/cut"
	"github.com/ambrdk/cutship/event"
	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/gate"
	"github.com/ambrdk/cutship/ledger"
	"github.com/ambrdk/cutship/pin"
	"github.com/ambrdk/cutship/release"
	"github.com/ambrdk/cutship/state"
)

var appDir = regexp.MustCompile(`^apps/([a-z0-9][a-z0-9-]*)/`)

type Plan struct {
	Apps   []string
	SkipCI bool
	Change int
}

type Run struct {
	Repo    string
	Env     string
	SHA     string
	ID      string
	Attempt int
	LogURL  string
	Now     time.Time
}

func checkEnv(cfg config.Config, env string) error {
	if !slices.Contains(cfg.Environments, env) {
		return fmt.Errorf("environment %q is not in cutship.yaml, want one of %v", env, cfg.Environments)
	}
	return nil
}

func dir(app string) string {
	return path.Dir(pin.Path(config.App{Name: app})) + "/"
}

type differ struct {
	ctx   context.Context
	f     forge.Reader
	repo  string
	cache map[[2]string][]string
}

func (d *differ) touched(app, from, to string) (bool, error) {
	if from == to {
		return false, nil
	}
	key := [2]string{from, to}
	paths, ok := d.cache[key]
	if !ok {
		var err error
		if paths, err = d.f.ChangedPaths(d.ctx, d.repo, from, to); err != nil {
			return false, err
		}
		d.cache[key] = paths
	}
	prefix := dir(app)
	return slices.ContainsFunc(paths, func(p string) bool { return strings.HasPrefix(p, prefix) }), nil
}

func MakePlan(ctx context.Context, f forge.Forge, d forge.Deployments, cfg config.Config, repo, env, base, head string) (Plan, error) {
	if err := checkEnv(cfg, env); err != nil {
		return Plan{}, err
	}
	diff := &differ{ctx: ctx, f: f, repo: repo, cache: map[[2]string][]string{}}
	pushed, err := f.ChangedPaths(ctx, repo, base, head)
	if err != nil {
		return Plan{}, err
	}
	diff.cache[[2]string{base, head}] = pushed
	names := map[string]bool{}
	for _, a := range cfg.Apps {
		names[a.Name] = true
	}
	for _, p := range pushed {
		if m := appDir.FindStringSubmatch(p); m != nil {
			names[m[1]] = true
		}
	}
	var plan Plan
	pending := []string{}
	for _, name := range sortedKeys(names) {
		from := base
		last, ok, err := ledger.LastSuccess(ctx, d, repo, env, name)
		if err != nil {
			return Plan{}, err
		}
		if ok {
			from = last.SHA
		}
		latest, _, err := ledger.Latest(ctx, d, repo, env, name)
		if err != nil {
			return Plan{}, err
		}
		failed := latest.State == forge.DeployFailure
		changed, err := diff.touched(name, from, head)
		if err != nil {
			return Plan{}, err
		}
		before, err := diff.touched(name, from, base)
		if err != nil {
			return Plan{}, err
		}
		if failed || before {
			pending = append(pending, name)
		}
		if !changed && !failed {
			continue
		}
		if _, err := f.FileAt(ctx, repo, pin.Path(config.App{Name: name}), head); errors.Is(err, forge.ErrNotFound) {
			continue
		} else if err != nil {
			return Plan{}, err
		}
		plan.Apps = append(plan.Apps, name)
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

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func bumpOf(ctx context.Context, f forge.Reader, cfg config.Config, repo, sha, app string) (forge.Change, bool, error) {
	changes, err := f.ChangesForCommit(ctx, repo, sha)
	if err != nil {
		return forge.Change{}, false, err
	}
	for _, c := range changes {
		if other, _, ok := bump.Parse(cfg.Release, c.HeadRef); ok && other == app && c.MergedAt != nil {
			return c, true, nil
		}
	}
	return forge.Change{}, false, nil
}

func record(ctx context.Context, f forge.Forge, cfg config.Config, run Run, app string, e event.Event) error {
	ch, ok, err := bumpOf(ctx, f, cfg, run.Repo, run.SHA, app)
	if err != nil || !ok {
		return err
	}
	e.PR, e.PRURL = ch.Number, ch.URL
	_, err = state.New(f, run.Repo, cut.Link).Update(ctx, ch, func(r *state.Record) {
		if r.App == "" {
			r.App = app
		}
		r.Env = run.Env
		if _, tag, ok := bump.Parse(cfg.Release, ch.HeadRef); ok && r.Tag == "" {
			r.Tag = tag
		}
		r.State, _ = release.Apply(r.State, e)
	})
	return err
}

func Start(ctx context.Context, f forge.Forge, d forge.Deployments, cfg config.Config, run Run, apps []string) error {
	if err := checkEnv(cfg, run.Env); err != nil {
		return err
	}
	for _, app := range apps {
		if _, err := ledger.Start(ctx, d, run.Repo, run.Env, app, run.SHA, run.LogURL); err != nil {
			return fmt.Errorf("ledger %s: %w", app, err)
		}
		e := event.Event{Kind: event.DeployStarted, ID: "deploy:" + run.ID + ":" + app + ":started", At: run.Now, App: app, Commit: run.SHA, RunURL: run.LogURL, Attempt: run.Attempt}
		if err := record(ctx, f, cfg, run, app, e); err != nil {
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
	for _, app := range apps {
		if _, err := ledger.Finish(ctx, d, run.Repo, run.Env, app, run.SHA, succeeded, run.LogURL); err != nil {
			return fmt.Errorf("ledger %s: %w", app, err)
		}
		e := event.Event{Kind: event.DeployFinished, ID: "deploy:" + run.ID + ":" + app + ":finished", At: run.Now, App: app, Commit: run.SHA, RunURL: run.LogURL, Attempt: run.Attempt, Result: result}
		if report != nil {
			e.StartedAt, e.Hosts = report.StartedAt, report.Hosts
			e.NoChanges = succeeded && report.NoChanges()
			if report.Failure != nil {
				e.FailedTask, e.Error = report.Failure.Task, report.Failure.Message
			}
		}
		if err := record(ctx, f, cfg, run, app, e); err != nil {
			return fmt.Errorf("state of %s: %w", app, err)
		}
	}
	return nil
}
