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
	"github.com/ambrdk/cutship/notifier"
	"github.com/ambrdk/cutship/pin"
	"github.com/ambrdk/cutship/post"
	"github.com/ambrdk/cutship/registry"
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
	Notifiers   func(config.Config) ([]notifier.Notifier, error)
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
	b := bumps{deploy: deploy, repo: in.DeployRepo, rel: rel, app: app.Name, tag: in.Tag, branch: branchName}
	higher, warnings, err := b.settle(ctx)
	res.Warnings = append(res.Warnings, warnings...)
	if err != nil {
		return Result{}, err
	}
	if higher != nil {
		res.Outcome, res.Change = Superseded, *higher
		return res, nil
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
	higher, warnings, err = b.settle(ctx)
	res.Warnings = append(res.Warnings, warnings...)
	if err != nil {
		return Result{}, err
	}
	if higher != nil {
		if err := deploy.CloseChange(ctx, in.DeployRepo, ch.Number, true); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("close own bump #%d superseded by #%d: %v", ch.Number, higher.Number, err))
		}
		res.Outcome, res.Change = Superseded, *higher
		return res, nil
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
	var ns []notifier.Notifier
	if in.Notifiers != nil {
		if ns, err = in.Notifiers(cfg); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("the release goes on without a post: %v", err))
		}
	}
	p := post.New(deploy, in.DeployRepo, ns, func(w string) { res.Warnings = append(res.Warnings, w) })
	target := post.Target{Change: ch, App: app.Name, Env: cfg.Environments[0], Tag: in.Tag}
	if _, err := p.Apply(ctx, target, e); err != nil {
		return Result{}, fmt.Errorf("save the release state on #%d: %w", ch.Number, err)
	}
	if am, ok := deploy.(forge.AutoMerger); ok {
		if err := am.AutoMerge(ctx, in.DeployRepo, ch.Number); err != nil {
			return Result{}, err
		}
	}
	res.Outcome, res.Change = Bumped, ch
	return res, nil
}

type bumps struct {
	deploy forge.Forge
	repo   string
	rel    config.Release
	app    string
	tag    string
	branch string
}

func (b bumps) settle(ctx context.Context) (*forge.Change, []string, error) {
	open, err := b.deploy.OpenChanges(ctx, b.repo, b.rel.BumpBranchPrefix)
	if err != nil {
		return nil, nil, err
	}
	var warnings []string
	for _, ch := range open {
		other, tag, ok := bump.Parse(b.rel, ch.HeadRef)
		if !ok || other != b.app || ch.HeadRef == b.branch {
			continue
		}
		if len(bump.Superseded(b.rel, b.app, tag, []string{b.branch})) > 0 {
			return &ch, warnings, nil
		}
	}
	for _, ch := range open {
		other, tag, ok := bump.Parse(b.rel, ch.HeadRef)
		if !ok || other != b.app || len(bump.Superseded(b.rel, b.app, b.tag, []string{ch.HeadRef})) == 0 {
			continue
		}
		if err := b.deploy.CloseChange(ctx, b.repo, ch.Number, true); err != nil {
			warnings = append(warnings, fmt.Sprintf("close superseded bump %s (#%d): %v", tag, ch.Number, err))
		}
	}
	return nil, warnings, nil
}
