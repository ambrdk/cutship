package notify

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/event"
	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/forge/fake"
	"github.com/ambrdk/cutship/notifier"
	nfake "github.com/ambrdk/cutship/notifier/fake"
	"github.com/ambrdk/cutship/post"
	"github.com/ambrdk/cutship/release"
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
release:
  bump_branch_prefix: bump/
`
)

var ctx = context.Background()

type world struct {
	f      *fake.Forge
	cfg    config.Config
	n      *nfake.Notifier
	p      *post.Publisher
	change forge.Change
	base   string
}

func setup(t *testing.T) *world {
	t.Helper()
	cfg, problems := config.Parse([]byte(cfgY))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	w := &world{f: fake.New("release-pat"), cfg: cfg, n: nfake.New("telegram:-100123")}
	w.base = w.f.Commit(repo, "main", map[string][]byte{"apps/alert-relay/app.yml": []byte("image: " + own + ":v0.3.0\n")})
	ch, err := w.f.PushChange(ctx, repo, forge.ChangeRequest{Base: "main", Branch: "bump/alert-relay-v0.3.1", Title: "chore(apps): bump alert-relay to v0.3.1", Files: map[string][]byte{"apps/alert-relay/app.yml": []byte("image: " + own + ":v0.3.1\n")}})
	if err != nil {
		t.Fatal(err)
	}
	w.change = ch
	w.p = post.New(w.f.As("github-actions[bot]"), repo, []notifier.Notifier{w.n}, func(s string) { t.Errorf("warning %s", s) })
	return w
}

func (w *world) pull(action string, merged bool, ref string) []byte {
	mergedBy := `null`
	if merged {
		mergedBy = `{"login":"exassed"}`
	}
	return fmt.Appendf(nil, `{"action":%q,"pull_request":{"number":%d,"title":"chore(apps): bump alert-relay to v0.3.1","html_url":"https://github.com/ambrdk/ansible/pull/%d","created_at":"2026-10-19T12:00:00Z","updated_at":"2026-10-19T12:05:00Z","closed_at":"2026-10-19T12:05:00Z","merged":%t,"merge_commit_sha":"m1","merged_by":%s,"user":{"login":"release-pat"},"head":{"ref":%q,"sha":%q},"base":{"sha":%q}}}`,
		action, w.change.Number, w.change.Number, merged, mergedBy, ref, w.change.HeadSHA, w.base)
}

func (w *world) handle(t *testing.T, name string, payload []byte) []Item {
	t.Helper()
	items, err := Parse(ctx, w.f, w.cfg, repo, name, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := Handle(ctx, items, w.p); err != nil {
		t.Fatal(err)
	}
	return items
}

func (w *world) timeline(t *testing.T) []release.Line {
	t.Helper()
	rec, ok, err := w.p.Store().Load(ctx, w.change, "alert-relay")
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	return rec.State.Timeline
}

func TestNotifyOpenedBump(t *testing.T) {
	w := setup(t)
	items := w.handle(t, "pull_request", w.pull("opened", false, "bump/alert-relay-v0.3.1"))
	if len(items) != 1 || items[0].Event.Kind != event.ReleaseOpened || items[0].Event.Image != own || items[0].Event.From != "v0.3.0" || items[0].Event.To != "v0.3.1" {
		t.Fatalf("%+v", items)
	}
	if items[0].Event.ID != fmt.Sprintf("release:%d:%s", w.change.Number, w.change.HeadSHA) {
		t.Fatalf("opened id %q differs from cutship release", items[0].Event.ID)
	}
	if w.n.Posts() != 1 || len(w.timeline(t)) != 1 {
		t.Fatalf("posts %d timeline %v", w.n.Posts(), w.timeline(t))
	}
}

func TestNotifyClosedUnmerged(t *testing.T) {
	w := setup(t)
	w.handle(t, "pull_request", w.pull("opened", false, "bump/alert-relay-v0.3.1"))
	w.handle(t, "pull_request", w.pull("closed", false, "bump/alert-relay-v0.3.1"))
	last := w.n.Views[len(w.n.Views)-1]
	if last.State.Stage != release.Closed || !last.Final {
		t.Fatalf("%+v", last)
	}
	published := w.n.Published()
	w.handle(t, "pull_request", w.pull("closed", false, "bump/alert-relay-v0.3.1"))
	if w.n.Published() != published {
		t.Fatal("replay published again")
	}
}

func TestNotifyMergedMatchesDeploy(t *testing.T) {
	w := setup(t)
	items := w.handle(t, "pull_request", w.pull("closed", true, "bump/alert-relay-v0.3.1"))
	e := items[0].Event
	if e.Kind != event.Merged || e.Actor != "exassed" || e.ID != fmt.Sprintf("pull_request:closed:%d:%s", w.change.Number, w.change.HeadSHA) {
		t.Fatalf("%+v", e)
	}
}

func run(name, conclusion, ref string, n int) []byte {
	return fmt.Appendf(nil, `{"action":"completed","workflow_run":{"id":77,"name":%q,"event":"pull_request","conclusion":%q,"run_attempt":1,"updated_at":"2026-10-19T12:03:00Z","pull_requests":[{"number":%d,"head":{"ref":%q,"sha":"h"}}]}}`, name, conclusion, n, ref)
}

func TestNotifyCIResult(t *testing.T) {
	w := setup(t)
	w.handle(t, "pull_request", w.pull("opened", false, "bump/alert-relay-v0.3.1"))
	items := w.handle(t, "workflow_run", run("ci", "failure", "bump/alert-relay-v0.3.1", w.change.Number))
	if len(items) != 1 || items[0].Event.Kind != event.ChecksFinished || items[0].Target.Change.Author != "release-pat" {
		t.Fatalf("%+v", items)
	}
	if last := w.n.Views[len(w.n.Views)-1]; last.State.Stage != release.ChecksFailed || !last.Loud {
		t.Fatalf("%+v", last.State)
	}
	if items := w.handle(t, "workflow_run", run("deploy", "success", "bump/alert-relay-v0.3.1", w.change.Number)); len(items) != 0 {
		t.Fatalf("deploy run handled: %+v", items)
	}
}

func TestNotifyIgnoresOtherBranches(t *testing.T) {
	w := setup(t)
	for name, payload := range map[string][]byte{
		"pull_request": w.pull("opened", false, "fix/env"),
		"workflow_run": run("ci", "success", "fix/env", 9),
		"push":         []byte(`{}`),
	} {
		if items := w.handle(t, name, payload); len(items) != 0 {
			t.Errorf("%s: %+v", name, items)
		}
	}
	if items := w.handle(t, "pull_request", w.pull("synchronize", false, "bump/alert-relay-v0.3.1")); len(items) != 0 {
		t.Errorf("synchronize: %+v", items)
	}
}

func TestNotifyReplayIsNoop(t *testing.T) {
	w := setup(t)
	for range 2 {
		w.handle(t, "pull_request", w.pull("opened", false, "bump/alert-relay-v0.3.1"))
		w.handle(t, "workflow_run", run("ci", "success", "bump/alert-relay-v0.3.1", w.change.Number))
	}
	if w.n.Published() != 2 || len(w.timeline(t)) != 2 {
		t.Fatalf("published %d timeline %v", w.n.Published(), w.timeline(t))
	}
	if !strings.Contains(w.f.CommentsOf(repo, w.change.Number)[0].Body, "cutship:v1") {
		t.Fatal("state comment missing")
	}
}

func TestNotifyBadPayload(t *testing.T) {
	w := setup(t)
	if _, err := Parse(ctx, w.f, w.cfg, repo, "pull_request", []byte(`{`)); err == nil {
		t.Fatal("bad payload accepted")
	}
}
