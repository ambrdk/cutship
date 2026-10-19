package deploy

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/forge/fake"
	"github.com/ambrdk/cutship/ledger"
	"github.com/ambrdk/cutship/state"
)

const (
	repo = "ambrdk/ansible"
	own  = "ghcr.io/ambrdk/alert-relay"
	cfgY = `version: 1
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

type repoState struct {
	f    *fake.Forge
	cfg  config.Config
	base string
}

func app(tag string) []byte { return []byte("---\nimage: " + own + ":" + tag + "\n") }

func setup(t *testing.T) *repoState {
	t.Helper()
	cfg, problems := config.Parse([]byte(cfgY))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	f := fake.New("bot")
	base := f.Commit(repo, "main", map[string][]byte{
		"cutship.yaml":             []byte(cfgY),
		"apps/alert-relay/app.yml": app("v0.3.0"),
		"apps/alert-relay/env.j2":  []byte("A=1\n"),
		"apps/other/app.yml":       []byte("image: ghcr.io/ambrdk/other:v0.1.0\n"),
		"apps/cache/app.yml":       []byte("image: valkey/valkey:9.1.1\n"),
	})
	return &repoState{f: f, cfg: cfg, base: base}
}

func (r *repoState) commit(files map[string][]byte) string {
	return r.f.Commit(repo, "main", files)
}

func (r *repoState) deployed(t *testing.T, app, sha string, ok bool) {
	t.Helper()
	if _, err := ledger.Finish(ctx, r.f, repo, "prod", app, sha, ok, ""); err != nil {
		t.Fatal(err)
	}
}

func (r *repoState) plan(t *testing.T, base, head string) Plan {
	t.Helper()
	p, err := MakePlan(ctx, r.f, r.f, r.cfg, repo, "prod", base, head)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPlanWithoutLedger(t *testing.T) {
	r := setup(t)
	head := r.commit(map[string][]byte{"apps/alert-relay/app.yml": app("v0.3.1")})
	p := r.plan(t, r.base, head)
	if !slices.Equal(p.Apps, []string{"alert-relay"}) || p.SkipCI {
		t.Fatalf("%+v", p)
	}
}

func TestPlanUsesLedger(t *testing.T) {
	r := setup(t)
	for _, a := range []string{"alert-relay", "other", "cache"} {
		r.deployed(t, a, r.base, true)
	}
	before := r.commit(map[string][]byte{"apps/other/app.yml": []byte("image: ghcr.io/ambrdk/other:v0.1.1\n")})
	head := r.commit(map[string][]byte{"apps/alert-relay/env.j2": []byte("A=2\n")})
	p := r.plan(t, before, head)
	if !slices.Equal(p.Apps, []string{"alert-relay", "other"}) {
		t.Fatalf("%+v", p)
	}
	r.deployed(t, "other", before, true)
	if p := r.plan(t, before, head); !slices.Equal(p.Apps, []string{"alert-relay"}) {
		t.Fatalf("after other deployed: %+v", p)
	}
}

func TestPlanRetriesFailed(t *testing.T) {
	r := setup(t)
	r.deployed(t, "alert-relay", r.base, true)
	r.deployed(t, "alert-relay", r.base, false)
	head := r.commit(map[string][]byte{"apps/cache/app.yml": []byte("image: valkey/valkey:9.1.2\n")})
	if p := r.plan(t, r.base, head); !slices.Equal(p.Apps, []string{"alert-relay", "cache"}) {
		t.Fatalf("%+v", p)
	}
}

func TestPlanIgnoresDeletedApp(t *testing.T) {
	r := setup(t)
	head := r.commit(map[string][]byte{"apps/other/app.yml": nil, "apps/cache/app.yml": []byte("image: valkey/valkey:9.1.2\n")})
	if p := r.plan(t, r.base, head); !slices.Equal(p.Apps, []string{"cache"}) {
		t.Fatalf("%+v", p)
	}
}

func TestPlanRetriesFailedForeignApp(t *testing.T) {
	r := setup(t)
	for _, a := range []string{"alert-relay", "other"} {
		r.deployed(t, a, r.base, true)
	}
	r.deployed(t, "cache", r.base, false)
	_, head := r.mergeBump(t, "v0.3.1")
	p := r.plan(t, r.base, head)
	if !slices.Equal(p.Apps, []string{"alert-relay", "cache"}) || p.SkipCI {
		t.Fatalf("%+v", p)
	}
}

func TestPlanDeletedFailedAppDoesNotBlockSkip(t *testing.T) {
	r := setup(t)
	for _, a := range []string{"alert-relay", "cache"} {
		r.deployed(t, a, r.base, true)
	}
	r.deployed(t, "other", r.base, false)
	before := r.commit(map[string][]byte{"apps/other/app.yml": nil})
	r.deployed(t, "alert-relay", before, true)
	r.deployed(t, "cache", before, true)
	_, head := r.mergeBump(t, "v0.3.1")
	p := r.plan(t, before, head)
	if !slices.Equal(p.Apps, []string{"alert-relay"}) || !p.SkipCI {
		t.Fatalf("%+v", p)
	}
}

func TestPlanDoesNotCompareFiles(t *testing.T) {
	r := setup(t)
	r.deployed(t, "alert-relay", r.base, true)
	head := r.commit(map[string][]byte{"apps/alert-relay/app.yml": app("v0.3.1")})
	r.f.FailNext("ChangedPaths", forge.ErrTooMany)
	r.f.FailNext("ChangedPaths", forge.ErrTooMany)
	if p := r.plan(t, r.base, head); !slices.Equal(p.Apps, []string{"alert-relay"}) {
		t.Fatalf("%+v", p)
	}
}

func (r *repoState) mergeBump(t *testing.T, tag string) (forge.Change, string) {
	t.Helper()
	ch, err := r.f.PushChange(ctx, repo, forge.ChangeRequest{Base: "main", Branch: "bump/alert-relay-" + tag, Title: "bump", Files: map[string][]byte{"apps/alert-relay/app.yml": app(tag)}})
	if err != nil {
		t.Fatal(err)
	}
	r.f.SetChecks(repo, ch.HeadSHA, forge.Check{Name: "lint", State: forge.Success, CompletedAt: t0})
	head := r.f.Merge(repo, ch.Number, "bot", t0)
	return r.f.Change(repo, ch.Number), head
}

func TestPlanSkipCI(t *testing.T) {
	r := setup(t)
	for _, a := range []string{"alert-relay", "other", "cache"} {
		r.deployed(t, a, r.base, true)
	}
	ch, head := r.mergeBump(t, "v0.3.1")
	p := r.plan(t, r.base, head)
	if !p.SkipCI || p.Change != ch.Number || !slices.Equal(p.Apps, []string{"alert-relay"}) {
		t.Fatalf("%+v", p)
	}
	r.f.SetChecks(repo, ch.HeadSHA, forge.Check{Name: "lint", State: forge.Failure, CompletedAt: t0})
	if p := r.plan(t, r.base, head); p.SkipCI {
		t.Fatalf("red lint: %+v", p)
	}
}

func TestPlanSkipCIBlockedByPending(t *testing.T) {
	r := setup(t)
	for _, a := range []string{"alert-relay", "other", "cache"} {
		r.deployed(t, a, r.base, true)
	}
	before := r.commit(map[string][]byte{"apps/other/app.yml": []byte("image: ghcr.io/ambrdk/other:v0.1.1\n")})
	_, head := r.mergeBump(t, "v0.3.1")
	p := r.plan(t, before, head)
	if p.SkipCI || !slices.Equal(p.Apps, []string{"alert-relay", "other"}) {
		t.Fatalf("%+v", p)
	}
}

func TestPlanSkipCIWithoutLedger(t *testing.T) {
	r := setup(t)
	_, head := r.mergeBump(t, "v0.3.1")
	if p := r.plan(t, r.base, head); !p.SkipCI {
		t.Fatalf("%+v", p)
	}
}

const reportV1 = `{"version":1,"started_at":"2026-10-19T12:00:00Z","finished_at":"2026-10-19T12:01:30Z",
"hosts":{"mon-1":{"ok":12,"changed":2,"failed":0,"unreachable":0,"skipped":3},"idle":{"ok":1,"changed":0,"failed":0,"unreachable":0,"skipped":0}},
"failure":{"host":"mon-1","task":"restart","message":"boom"}}`

func TestReadReportV1(t *testing.T) {
	rep, err := ReadReport([]byte(reportV1))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Hosts) != 1 || rep.Hosts[0].Name != "mon-1" || rep.Hosts[0].Changed != 2 || rep.NoChanges() {
		t.Fatalf("%+v", rep)
	}
	if rep.Failure == nil || rep.Failure.Task != "restart" || !rep.StartedAt.Equal(t0) {
		t.Fatalf("%+v", rep)
	}
	idle, _ := ReadReport([]byte(`{"version":1,"hosts":{"a":{"ok":1}}}`))
	if !idle.NoChanges() {
		t.Fatal("idle report has changes")
	}
}

func TestReadReportUnknownVersion(t *testing.T) {
	for _, data := range []string{`{"version":2,"hosts":{}}`, `{"hosts":{}}`, `not json`} {
		if _, err := ReadReport([]byte(data)); err == nil {
			t.Errorf("%s accepted", data)
		}
	}
}

func TestStartFinishWritesLedgerAndState(t *testing.T) {
	r := setup(t)
	ch, head := r.mergeBump(t, "v0.3.1")
	run := Run{Repo: repo, Env: "prod", SHA: head, ID: "77-1", Attempt: 1, LogURL: "https://log/77", Now: t0}
	apps := []string{"alert-relay", "cache"}
	if err := Start(ctx, r.f, r.f, r.cfg, run, apps); err != nil {
		t.Fatal(err)
	}
	for _, a := range apps {
		if got, ok, _ := ledger.Latest(ctx, r.f, repo, "prod", a); !ok || got.State != forge.DeployInProgress || got.SHA != head {
			t.Fatalf("%s: %+v", a, got)
		}
	}
	st := state.New(r.f, repo, func(state.Record) string { return "" })
	rec, ok, err := st.Load(ctx, ch, "alert-relay")
	if err != nil || !ok || rec.State.Stage != "deploying" {
		t.Fatalf("%+v %v %v", rec, ok, err)
	}
	rep, _ := ReadReport([]byte(reportV1))
	run.Now = t0.Add(90 * time.Second)
	if err := Finish(ctx, r.f, r.f, r.cfg, run, apps, true, &rep); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := ledger.Latest(ctx, r.f, repo, "prod", "cache"); got.State != forge.DeploySuccess {
		t.Fatalf("%+v", got)
	}
	rec, _, _ = st.Load(ctx, ch, "alert-relay")
	if rec.State.Stage != "deployed" || len(rec.State.Hosts) != 1 || len(r.f.CommentsOf(repo, ch.Number)) != 1 {
		t.Fatalf("%+v", rec.State)
	}
	if err := Finish(ctx, r.f, r.f, r.cfg, run, apps, true, &rep); err != nil {
		t.Fatal(err)
	}
	if again, _, _ := st.Load(ctx, ch, "alert-relay"); len(again.State.Timeline) != len(rec.State.Timeline) {
		t.Fatalf("repeated finish changed the timeline: %d vs %d", len(again.State.Timeline), len(rec.State.Timeline))
	}
}

func TestStartWithActionsTokenKeepsOneState(t *testing.T) {
	r := setup(t)
	ch, head := r.mergeBump(t, "v0.3.1")
	st := state.New(r.f, repo, func(state.Record) string { return "" })
	if _, err := st.Update(ctx, ch, "alert-relay", func(rec *state.Record) { rec.App, rec.Tag = "alert-relay", "v0.3.1" }); err != nil {
		t.Fatal(err)
	}
	actions := r.f.As("github-actions[bot]")
	run := Run{Repo: repo, Env: "prod", SHA: head, ID: "79-1", Now: t0}
	if err := Start(ctx, actions, actions, r.cfg, run, []string{"alert-relay"}); err != nil {
		t.Fatal(err)
	}
	comments := r.f.CommentsOf(repo, ch.Number)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, `"stage":"deploying"`) {
		t.Fatalf("comments %+v", comments)
	}
}

func TestFinishWithoutStart(t *testing.T) {
	r := setup(t)
	head := r.commit(map[string][]byte{"apps/alert-relay/env.j2": []byte("A=3\n")})
	run := Run{Repo: repo, Env: "prod", SHA: head, ID: "78-1", Attempt: 1, Now: t0}
	if err := Finish(ctx, r.f, r.f, r.cfg, run, []string{"alert-relay"}, false, nil); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := ledger.Latest(ctx, r.f, repo, "prod", "alert-relay"); !ok || got.State != forge.DeployFailure {
		t.Fatalf("%+v", got)
	}
	if p := r.plan(t, head, head); !slices.Equal(p.Apps, []string{"alert-relay"}) {
		t.Fatalf("failed app not retried: %+v", p)
	}
}

func TestStartRejectsUnknownEnv(t *testing.T) {
	r := setup(t)
	err := Start(ctx, r.f, r.f, r.cfg, Run{Repo: repo, Env: "staging", SHA: r.base, ID: "1-1"}, []string{"alert-relay"})
	if err == nil || !strings.Contains(err.Error(), "staging") {
		t.Fatalf("err = %v", err)
	}
	if _, err := MakePlan(ctx, r.f, r.f, r.cfg, repo, "staging", r.base, r.base); err == nil {
		t.Fatal("plan accepted unknown env")
	}
}
