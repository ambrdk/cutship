package cut

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ambrdk/cutship/bump"
	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/event"
	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/pin"
	"github.com/ambrdk/cutship/registry"
	"github.com/ambrdk/cutship/release"
	"github.com/ambrdk/cutship/state"
)

const ConfigPath = "cutship.yaml"

type Outcome string

const (
	Bumped     Outcome = "bump"
	Same       Outcome = "same"
	Superseded Outcome = "superseded"
)

type Input struct {
	App         string
	Tag         string
	SHA         string
	ServiceRepo string
	DeployRepo  string
	Now         time.Time
}

type Result struct {
	Outcome  Outcome
	Change   forge.Change
	Warnings []string
}

type ConfigError struct {
	File     string
	Problems []config.Problem
}

func (e *ConfigError) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		parts[i] = p.Error()
	}
	return e.File + ": " + strings.Join(parts, "; ")
}

func Link(r state.Record) string {
	return "cutship release " + r.App + " " + r.Tag
}

func LoadConfig(ctx context.Context, f forge.Reader, repo string) (config.Config, string, error) {
	branch, err := f.DefaultBranch(ctx, repo)
	if err != nil {
		return config.Config{}, "", fmt.Errorf("read %s: %w", repo, err)
	}
	data, err := f.FileAt(ctx, repo, ConfigPath, branch)
	if err != nil {
		return config.Config{}, "", fmt.Errorf("read %s:%s on %s: %w", repo, ConfigPath, branch, err)
	}
	cfg, problems := config.Parse(data)
	if len(problems) > 0 {
		return config.Config{}, "", &ConfigError{File: repo + ":" + ConfigPath, Problems: problems}
	}
	return cfg, branch, nil
}

func Release(ctx context.Context, service, deploy forge.Forge, reg registry.Registry, in Input) (Result, error) {
	cfg, branch, err := LoadConfig(ctx, deploy, in.DeployRepo)
	if err != nil {
		return Result{}, err
	}
	app, ok := cfg.App(in.App)
	if !ok {
		return Result{}, fmt.Errorf("app %q is not in %s:%s", in.App, in.DeployRepo, ConfigPath)
	}
	rel := cfg.Release
	if _, ok := rel.Version(in.Tag); !ok {
		return Result{}, fmt.Errorf("tag %q does not match release.tag_format %q: %w", in.Tag, rel.TagFormat, pin.ErrRule)
	}
	var res Result
	rev, err := reg.Revision(ctx, app.Image, in.Tag)
	switch {
	case errors.Is(err, registry.ErrMissing):
		return Result{}, fmt.Errorf("image %s:%s not found: push the image first", app.Image, in.Tag)
	case err != nil:
		return Result{}, fmt.Errorf("read image %s:%s: %w", app.Image, in.Tag, err)
	case rev == "":
		res.Warnings = append(res.Warnings, fmt.Sprintf("image %s:%s has no %s label, its commit is not checked", app.Image, in.Tag, registry.RevisionLabel))
	case rev != in.SHA:
		return Result{}, fmt.Errorf("image %s:%s was built from %s, not %s: release a new tag", app.Image, in.Tag, rev, in.SHA)
	}
	if err := service.CreateTag(ctx, in.ServiceRepo, in.Tag, in.SHA); err != nil {
		return Result{}, fmt.Errorf("tag %s in %s: %w", in.Tag, in.ServiceRepo, err)
	}
	content, err := deploy.FileAt(ctx, in.DeployRepo, pin.Path(app), branch)
	if err != nil {
		return Result{}, fmt.Errorf("read %s:%s: %w", in.DeployRepo, pin.Path(app), err)
	}
	current, ok := pin.Line(content)
	if !ok {
		return Result{}, fmt.Errorf("no top-level image in %s:%s: %w", in.DeployRepo, pin.Path(app), pin.ErrRule)
	}
	needed, err := pin.Check(app, rel, current, in.Tag)
	if err != nil {
		return Result{}, err
	}
	branchName, title, err := bump.Name(rel, app.Name, in.Tag)
	if err != nil {
		return Result{}, err
	}
	open, err := deploy.OpenChanges(ctx, in.DeployRepo, rel.BumpBranchPrefix)
	if err != nil {
		return Result{}, err
	}
	for _, ch := range open {
		other, tag, ok := bump.Parse(rel, ch.HeadRef)
		if !ok || other != app.Name || ch.HeadRef == branchName {
			continue
		}
		if len(bump.Superseded(rel, app.Name, tag, []string{branchName})) > 0 {
			res.Outcome, res.Change = Superseded, ch
			return res, nil
		}
	}
	for _, ch := range open {
		other, tag, ok := bump.Parse(rel, ch.HeadRef)
		if !ok || other != app.Name || len(bump.Superseded(rel, app.Name, in.Tag, []string{ch.HeadRef})) == 0 {
			continue
		}
		if err := deploy.CloseChange(ctx, in.DeployRepo, ch.Number, true); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("close superseded bump %s (#%d): %v", tag, ch.Number, err))
		}
	}
	if !needed {
		res.Outcome = Same
		return res, nil
	}
	updated, err := pin.Replace(content, app, rel, in.Tag)
	if err != nil {
		return Result{}, err
	}
	ch, err := deploy.PushChange(ctx, in.DeployRepo, forge.ChangeRequest{
		Base:    branch,
		Branch:  branchName,
		Title:   title,
		Message: title,
		Files:   map[string][]byte{pin.Path(app): updated},
	})
	if err != nil {
		return Result{}, fmt.Errorf("push %s to %s: %w", branchName, in.DeployRepo, err)
	}
	if am, ok := deploy.(forge.AutoMerger); ok {
		if err := am.AutoMerge(ctx, in.DeployRepo, ch.Number); err != nil {
			return Result{}, err
		}
	}
	_, from := pin.ParseRef(current)
	e := event.Event{
		Kind:  event.ReleaseOpened,
		ID:    fmt.Sprintf("release:%d:%s", ch.Number, ch.HeadSHA),
		At:    in.Now,
		App:   app.Name,
		From:  from,
		To:    in.Tag,
		Image: app.Image,
		Title: title,
		PR:    ch.Number,
		PRURL: ch.URL,
	}
	env := cfg.Environments[0]
	if _, err := state.New(deploy, in.DeployRepo, Link).Update(ctx, ch, func(r *state.Record) {
		r.App, r.Env, r.Tag = app.Name, env, in.Tag
		r.State, _ = release.Apply(r.State, e)
	}); err != nil {
		return Result{}, fmt.Errorf("save the release state on #%d: %w", ch.Number, err)
	}
	res.Outcome, res.Change = Bumped, ch
	return res, nil
}
