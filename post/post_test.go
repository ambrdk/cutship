package post

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ambrdk/cutship/event"
	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/forge/fake"
	"github.com/ambrdk/cutship/notifier"
	nfake "github.com/ambrdk/cutship/notifier/fake"
)

const repo = "ambrdk/ansible"

var (
	ctx = context.Background()
	t0  = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
)

type world struct {
	f        *fake.Forge
	n        *nfake.Notifier
	p        *Publisher
	target   Target
	warnings []string
}

func setup(t *testing.T) *world {
	t.Helper()
	w := &world{f: fake.New("cutship-bot"), n: nfake.New("telegram:-100123")}
	w.f.Commit(repo, "main", map[string][]byte{"a": []byte("1")})
	ch, err := w.f.PushChange(ctx, repo, forge.ChangeRequest{Base: "main", Branch: "bump/alert-relay-v0.3.1", Title: "t", Files: map[string][]byte{"a": []byte("2")}})
	if err != nil {
		t.Fatal(err)
	}
	w.p = New(w.f, repo, []notifier.Notifier{w.n}, func(s string) { w.warnings = append(w.warnings, s) })
	w.target = Target{Change: ch, App: "alert-relay", Env: "prod", Tag: "v0.3.1"}
	return w
}

func ev(id string, kind event.Kind, minute int) event.Event {
	return event.Event{Kind: kind, ID: id, At: t0.Add(time.Duration(minute) * time.Minute), App: "alert-relay", To: "v0.3.1"}
}

func TestApplyPublishesAndStoresRefs(t *testing.T) {
	w := setup(t)
	rec, err := w.p.Apply(ctx, w.target, ev("e1", event.ReleaseOpened, 0))
	if err != nil || w.n.Published() != 1 || len(rec.Messages["telegram:-100123"]) == 0 || rec.Env != "prod" || rec.Tag != "v0.3.1" {
		t.Fatalf("%+v %v", rec, err)
	}
	comments := w.f.CommentsOf(repo, w.target.Change.Number)
	if len(comments) != 1 || !strings.HasPrefix(comments[0].Body, "Release thread: https://chat.test/telegram:-100123/1\n\n<!-- cutship:v1 ") {
		t.Fatalf("comments %+v", comments)
	}
	if len(w.warnings) != 0 {
		t.Fatalf("warnings %v", w.warnings)
	}
}

func TestApplyRepeatDoesNothing(t *testing.T) {
	w := setup(t)
	for range 2 {
		if _, err := w.p.Apply(ctx, w.target, ev("e1", event.ReleaseOpened, 0)); err != nil {
			t.Fatal(err)
		}
	}
	if w.n.Published() != 1 {
		t.Fatalf("published %d times", w.n.Published())
	}
	if _, err := w.p.Apply(ctx, w.target, ev("e2", event.ChecksFinished, 1)); err != nil || w.n.Published() != 2 || w.n.Posts() != 1 {
		t.Fatalf("published %d posts %d %v", w.n.Published(), w.n.Posts(), err)
	}
}

func TestApplyIncompleteRepublishes(t *testing.T) {
	w := setup(t)
	w.n.Incomplete = true
	for range 2 {
		if _, err := w.p.Apply(ctx, w.target, ev("e1", event.ReleaseOpened, 0)); err != nil {
			t.Fatal(err)
		}
	}
	if w.n.Published() != 2 || w.n.Posts() != 1 {
		t.Fatalf("published %d posts %d", w.n.Published(), w.n.Posts())
	}
}

func TestApplyNotifierFailureWarns(t *testing.T) {
	w := setup(t)
	w.n.FailNext(errors.New("telegram is down"))
	rec, err := w.p.Apply(ctx, w.target, ev("e1", event.ReleaseOpened, 0))
	if err != nil || len(rec.Messages) != 0 || len(w.warnings) != 1 || !strings.Contains(w.warnings[0], "telegram is down") {
		t.Fatalf("%+v %v %v", rec, err, w.warnings)
	}
	if _, err := w.p.Apply(ctx, w.target, ev("e2", event.Merged, 1)); err != nil {
		t.Fatal(err)
	}
	last := w.n.Views[len(w.n.Views)-1]
	if seen := strings.Join(last.State.Seen, ","); !strings.Contains(seen, "e1") || !strings.Contains(seen, "e2") {
		t.Fatalf("the late post lacks history: %v", last.State.Seen)
	}
}

func TestApplyStatusWarning(t *testing.T) {
	w := setup(t)
	w.n.Status = notifier.NoCopy
	if _, err := w.p.Apply(ctx, w.target, ev("e1", event.ReleaseOpened, 0)); err != nil {
		t.Fatal(err)
	}
	if len(w.warnings) != 1 || !strings.Contains(w.warnings[0], "without a comment") {
		t.Fatalf("%v", w.warnings)
	}
}

func TestMigratedThreadIsEdited(t *testing.T) {
	w := setup(t)
	old := `{"state":{"stage":"opened","app":"alert-relay","to":"v0.3.1","seen":["e0"]},"ids":{"post_id":7,"copy_id":8,"comment_id":9}}`
	w.f.AddComment(repo, w.target.Change.Number, "cutship-bot", "Telegram release thread\n\n<!-- deploy-notify:v1 "+old+" -->")
	rec, err := w.p.Apply(ctx, w.target, ev("e1", event.ChecksFinished, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(w.n.Refs) != 1 || !strings.Contains(string(w.n.Refs[0]), `"post_id":7`) {
		t.Fatalf("refs %s", w.n.Refs)
	}
	if _, ok := rec.Messages["telegram"]; ok || len(rec.Messages["telegram:-100123"]) == 0 {
		t.Fatalf("messages %v", rec.Messages)
	}
	if got := w.f.CommentsOf(repo, w.target.Change.Number); len(got) != 1 || strings.Contains(got[0].Body, "deploy-notify") {
		t.Fatalf("comments %+v", got)
	}
}

func TestApplyNoopDoesNotCreatePost(t *testing.T) {
	w := setup(t)
	silent := New(w.f, repo, nil, func(string) {})
	if _, err := silent.Apply(ctx, w.target, ev("e1", event.ReleaseOpened, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.p.Apply(ctx, w.target, ev("e1", event.ReleaseOpened, 0)); err != nil {
		t.Fatal(err)
	}
	if w.n.Posts() != 0 {
		t.Fatalf("a replayed event opened post %d while the first writer was still publishing", w.n.Posts())
	}
	if _, err := w.p.Apply(ctx, w.target, ev("e2", event.Merged, 1)); err != nil || w.n.Posts() != 1 {
		t.Fatalf("posts %d %v", w.n.Posts(), err)
	}
}

type racing struct {
	*fake.Forge
	hook func()
}

func (r *racing) UpdateComment(ctx context.Context, name string, id int64, body string) error {
	err := r.Forge.UpdateComment(ctx, name, id, body)
	if h := r.hook; h != nil {
		r.hook = nil
		h()
	}
	return err
}

func TestApplyPublishesTheLatestState(t *testing.T) {
	w := setup(t)
	if _, err := w.p.Apply(ctx, w.target, ev("e1", event.ReleaseOpened, 0)); err != nil {
		t.Fatal(err)
	}
	other := New(w.f, repo, nil, func(string) {})
	r := &racing{Forge: w.f, hook: func() {
		if _, err := other.Apply(ctx, w.target, ev("e3", event.DeployStarted, 2)); err != nil {
			t.Error(err)
		}
	}}
	p := New(r, repo, []notifier.Notifier{w.n}, func(string) {})
	if _, err := p.Apply(ctx, w.target, ev("e2", event.Merged, 1)); err != nil {
		t.Fatal(err)
	}
	last := w.n.Views[len(w.n.Views)-1].State
	if last.Stage != "deploying" {
		t.Fatalf("published a stale view: %s %v", last.Stage, last.Seen)
	}
}

func TestApplyWithoutNotifiers(t *testing.T) {
	w := setup(t)
	p := New(w.f, repo, nil, func(s string) { t.Errorf("warning %s", s) })
	rec, err := p.Apply(ctx, w.target, ev("e1", event.ReleaseOpened, 0))
	if err != nil || rec.State.Stage == "" {
		t.Fatalf("%+v %v", rec, err)
	}
	if got := w.f.CommentsOf(repo, w.target.Change.Number); len(got) != 1 || !strings.HasPrefix(got[0].Body, "cutship release alert-relay v0.3.1") {
		t.Fatalf("comments %+v", got)
	}
}
