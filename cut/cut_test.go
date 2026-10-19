package cut

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/forge/fake"
	"github.com/ambrdk/cutship/registry"
)

const (
	service    = "ambrdk/alert-relay"
	deployRepo = "ambrdk/ansible"
	image      = "ghcr.io/ambrdk/alert-relay"
	cfgYAML    = `version: 1
forge: { type: github }
deploy_repo: ambrdk/ansible
apps:
  - name: alert-relay
    image: ghcr.io/ambrdk/alert-relay
    pin: { preset: ansible }
  - name: other
    image: ghcr.io/ambrdk/other
    pin: { preset: ansible }
release:
  bump_branch_prefix: bump/
`
)

var (
	ctx = context.Background()
	t0  = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
)

type images map[string]string

func (m images) Revision(_ context.Context, img, tag string) (string, error) {
	rev, ok := m[img+":"+tag]
	if !ok {
		return "", fmt.Errorf("%s:%s: %w", img, tag, registry.ErrMissing)
	}
	return rev, nil
}

type world struct {
	svc, dep *fake.Forge
	reg      images
	in       Input
}

func appYML(tag string) []byte {
	return []byte("---\nimage: " + image + ":" + tag + "\nhealth_url: x\n")
}

func setup(t *testing.T, pinned string) *world {
	t.Helper()
	w := &world{svc: fake.New("github-actions[bot]"), dep: fake.New("cutship-bot"), reg: images{}}
	sha := w.svc.Commit(service, "main", map[string][]byte{"main.go": []byte("package main")})
	w.dep.Commit(deployRepo, "main", map[string][]byte{
		"cutship.yaml":             []byte(cfgYAML),
		"apps/alert-relay/app.yml": appYML(pinned),
		"apps/other/app.yml":       []byte("image: ghcr.io/ambrdk/other:v0.1.0\n"),
	})
	w.reg[image+":v0.3.1"] = sha
	w.in = Input{App: "alert-relay", Tag: "v0.3.1", SHA: sha, ServiceRepo: service, DeployRepo: deployRepo, Now: t0}
	return w
}

func (w *world) run() (Result, error) {
	return Release(ctx, w.svc, w.dep, w.reg, w.in)
}

func (w *world) noSideEffects(t *testing.T) {
	t.Helper()
	if tags := w.svc.Tags(service); len(tags) != 0 {
		t.Errorf("tags %v", tags)
	}
	if changes := w.dep.Changes(deployRepo); len(changes) != 0 {
		t.Errorf("changes %+v", changes)
	}
}

func TestReleaseOpensBump(t *testing.T) {
	w := setup(t, "v0.3.0")
	res, err := w.run()
	if err != nil || res.Outcome != Bumped {
		t.Fatalf("%+v %v", res, err)
	}
	if w.svc.Tags(service)["v0.3.1"] != w.in.SHA {
		t.Fatalf("tags %v", w.svc.Tags(service))
	}
	changes := w.dep.Changes(deployRepo)
	if len(changes) != 1 || changes[0].HeadRef != "bump/alert-relay-v0.3.1" || changes[0].Title != "chore(apps): bump alert-relay to v0.3.1" || res.Change.Number != changes[0].Number {
		t.Fatalf("changes %+v", changes)
	}
	got, err := w.dep.FileAt(ctx, deployRepo, "apps/alert-relay/app.yml", "bump/alert-relay-v0.3.1")
	if err != nil || string(got) != string(appYML("v0.3.1")) {
		t.Fatalf("%q %v", got, err)
	}
	if !w.dep.AutoMergeOn(deployRepo, changes[0].Number) {
		t.Fatal("auto-merge off")
	}
	comments := w.dep.CommentsOf(deployRepo, changes[0].Number)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "<!-- cutship:v1 ") || !strings.Contains(comments[0].Body, `"app":"alert-relay"`) || !strings.Contains(comments[0].Body, `"from":"v0.3.0"`) {
		t.Fatalf("comments %+v", comments)
	}
}

func TestReleaseSame(t *testing.T) {
	w := setup(t, "v0.3.1")
	res, err := w.run()
	if err != nil || res.Outcome != Same {
		t.Fatalf("%+v %v", res, err)
	}
	if len(w.dep.Changes(deployRepo)) != 0 || w.svc.Tags(service)["v0.3.1"] != w.in.SHA {
		t.Fatalf("changes %+v tags %v", w.dep.Changes(deployRepo), w.svc.Tags(service))
	}
}

func TestReleaseOlderThanOpenBump(t *testing.T) {
	w := setup(t, "v0.3.0")
	newer := w.dep.AddChange(deployRepo, forge.Change{HeadRef: "bump/alert-relay-v0.3.2", Title: "newer"})
	res, err := w.run()
	if err != nil || res.Outcome != Superseded || res.Change.Number != newer.Number {
		t.Fatalf("%+v %v", res, err)
	}
	if changes := w.dep.Changes(deployRepo); len(changes) != 1 || changes[0].State != forge.Open {
		t.Fatalf("changes %+v", changes)
	}
}

func TestReleaseClosesSuperseded(t *testing.T) {
	w := setup(t, "v0.2.0")
	older := w.dep.AddChange(deployRepo, forge.Change{HeadRef: "bump/alert-relay-v0.3.0"})
	other := w.dep.AddChange(deployRepo, forge.Change{HeadRef: "bump/other-v0.0.9"})
	res, err := w.run()
	if err != nil || res.Outcome != Bumped {
		t.Fatalf("%+v %v", res, err)
	}
	if w.dep.Change(deployRepo, older.Number).State != forge.Closed || w.dep.Change(deployRepo, other.Number).State != forge.Open {
		t.Fatalf("changes %+v", w.dep.Changes(deployRepo))
	}
}

func TestReleaseSupersededCloseFailureWarns(t *testing.T) {
	w := setup(t, "v0.2.0")
	w.dep.AddChange(deployRepo, forge.Change{HeadRef: "bump/alert-relay-v0.3.0"})
	w.dep.FailNext("CloseChange", errors.New("boom"))
	res, err := w.run()
	if err != nil || res.Outcome != Bumped || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "boom") {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestReleaseImageMissing(t *testing.T) {
	w := setup(t, "v0.3.0")
	delete(w.reg, image+":v0.3.1")
	if _, err := w.run(); err == nil || !strings.Contains(err.Error(), "push the image first") {
		t.Fatalf("err = %v", err)
	}
	w.noSideEffects(t)
}

func TestReleaseImageFromOtherCommit(t *testing.T) {
	w := setup(t, "v0.3.0")
	w.reg[image+":v0.3.1"] = "deadbeef"
	if _, err := w.run(); err == nil || !strings.Contains(err.Error(), "release a new tag") || !strings.Contains(err.Error(), "deadbeef") {
		t.Fatalf("err = %v", err)
	}
	w.noSideEffects(t)
}

func TestReleaseNoRevisionLabel(t *testing.T) {
	w := setup(t, "v0.3.0")
	w.reg[image+":v0.3.1"] = ""
	res, err := w.run()
	if err != nil || res.Outcome != Bumped || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], registry.RevisionLabel) {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestReleaseTagElsewhere(t *testing.T) {
	w := setup(t, "v0.3.0")
	w.svc.Tag(service, "v0.3.1", "older")
	if _, err := w.run(); !errors.Is(err, forge.ErrTagElsewhere) {
		t.Fatalf("err = %v", err)
	}
	if len(w.dep.Changes(deployRepo)) != 0 {
		t.Fatal("bump opened")
	}
}

func TestReleaseLowerThanPin(t *testing.T) {
	w := setup(t, "v0.3.2")
	if _, err := w.run(); err == nil || !strings.Contains(err.Error(), "lower than") {
		t.Fatalf("err = %v", err)
	}
	if len(w.dep.Changes(deployRepo)) != 0 {
		t.Fatal("bump opened")
	}
}

func TestReleaseRejectsInput(t *testing.T) {
	for name, mutate := range map[string]func(*world){
		"unknown app": func(w *world) { w.in.App = "api" },
		"bad tag":     func(w *world) { w.in.Tag = "0.3.1" },
		"bad config": func(w *world) {
			w.dep.Commit(deployRepo, "main", map[string][]byte{"cutship.yaml": []byte("version: 2\n")})
		},
		"no config": func(w *world) {
			w.dep.Commit(deployRepo, "main", map[string][]byte{"cutship.yaml": nil})
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := setup(t, "v0.3.0")
			mutate(w)
			if _, err := w.run(); err == nil {
				t.Fatal("accepted")
			}
			w.noSideEffects(t)
		})
	}
	w := setup(t, "v0.3.0")
	w.dep.Commit(deployRepo, "main", map[string][]byte{"cutship.yaml": []byte("version: 2\n")})
	var cfgErr *ConfigError
	if _, err := w.run(); !errors.As(err, &cfgErr) || cfgErr.File != "ambrdk/ansible:cutship.yaml" || len(cfgErr.Problems) == 0 {
		t.Fatalf("err = %v", err)
	}
}

func TestReleaseStateBeforeAutoMerge(t *testing.T) {
	w := setup(t, "v0.3.0")
	w.dep.FailNext("AutoMerge", errors.New("unstable status"))
	if _, err := w.run(); err == nil {
		t.Fatal("auto-merge error swallowed")
	}
	changes := w.dep.Changes(deployRepo)
	if len(changes) != 1 || len(w.dep.CommentsOf(deployRepo, changes[0].Number)) != 1 {
		t.Fatalf("state missing after the auto-merge failure: %+v", changes)
	}
}

func TestReleaseConcurrentHigherBump(t *testing.T) {
	w := setup(t, "v0.3.0")
	w.dep.BeforePush(func() { w.dep.AddChange(deployRepo, forge.Change{HeadRef: "bump/alert-relay-v0.3.2"}) })
	res, err := w.run()
	if err != nil || res.Outcome != Superseded || res.Change.HeadRef != "bump/alert-relay-v0.3.2" {
		t.Fatalf("%+v %v", res, err)
	}
	for _, ch := range w.dep.Changes(deployRepo) {
		want := forge.Open
		if ch.HeadRef == "bump/alert-relay-v0.3.1" {
			want = forge.Closed
		}
		if ch.State != want {
			t.Errorf("%s is %s, want %s", ch.HeadRef, ch.State, want)
		}
	}
}

func TestReleaseConcurrentLowerBump(t *testing.T) {
	w := setup(t, "v0.2.0")
	w.dep.BeforePush(func() { w.dep.AddChange(deployRepo, forge.Change{HeadRef: "bump/alert-relay-v0.3.0"}) })
	res, err := w.run()
	if err != nil || res.Outcome != Bumped {
		t.Fatalf("%+v %v", res, err)
	}
	for _, ch := range w.dep.Changes(deployRepo) {
		if ch.HeadRef == "bump/alert-relay-v0.3.0" && ch.State != forge.Closed {
			t.Errorf("lower bump left open: %+v", ch)
		}
	}
}

func TestReleaseRerunAfterEachStep(t *testing.T) {
	for _, step := range []struct {
		dep    bool
		method string
	}{
		{false, "CreateTag"},
		{true, "OpenChanges"},
		{true, "PushChange"},
		{true, "AutoMerge"},
		{true, "Comments"},
		{true, "CreateComment"},
	} {
		t.Run(step.method, func(t *testing.T) {
			w := setup(t, "v0.3.0")
			target := w.svc
			if step.dep {
				target = w.dep
			}
			target.FailNext(step.method, errors.New("boom"))
			if _, err := w.run(); err == nil {
				t.Fatal("first run passed")
			}
			res, err := w.run()
			if err != nil || res.Outcome != Bumped {
				t.Fatalf("second run: %+v %v", res, err)
			}
			changes := w.dep.Changes(deployRepo)
			if len(changes) != 1 || len(w.svc.Tags(service)) != 1 || !w.dep.AutoMergeOn(deployRepo, changes[0].Number) {
				t.Fatalf("changes %+v tags %v", changes, w.svc.Tags(service))
			}
			if comments := w.dep.CommentsOf(deployRepo, changes[0].Number); len(comments) != 1 {
				t.Fatalf("comments %+v", comments)
			}
		})
	}
}
