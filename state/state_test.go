package state

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ambrdk/cutship/event"
	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/forge/fake"
	"github.com/ambrdk/cutship/release"
)

const deployRepo = "ambrdk/ansible"

var (
	ctx = context.Background()
	t0  = time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
)

func link(r Record) string { return "Release thread of " + r.App }

func forgeChange() forge.Change { return forge.Change{HeadRef: "bump/alert-relay-v0.3.1"} }

func setup(t *testing.T) (*fake.Forge, *Store, int) {
	t.Helper()
	f := fake.New("cutship-bot")
	f.Commit(deployRepo, "main", nil)
	n := f.AddChange(deployRepo, forgeChange()).Number
	return f, New(f, deployRepo, link), n
}

func opened(id string) event.Event {
	return event.Event{Kind: event.ReleaseOpened, ID: id, At: t0, App: "alert-relay", To: "v0.3.1", PR: 1}
}

func apply(e event.Event) func(*Record) {
	return func(r *Record) {
		r.App, r.Env, r.Tag = e.App, "prod", e.To
		r.State, _ = release.Apply(r.State, e)
	}
}

func TestStateRoundTrip(t *testing.T) {
	f, s, n := setup(t)
	if _, ok, err := s.Load(ctx, n); ok || err != nil {
		t.Fatalf("empty: %v %v", ok, err)
	}
	rec, err := s.Update(ctx, n, func(r *Record) {
		apply(opened("e1"))(r)
		r.Messages = map[string]json.RawMessage{"telegram": json.RawMessage(`{"post_id":5}`)}
	})
	if err != nil || rec.Version != 1 || rec.State.Stage == "" {
		t.Fatalf("%+v %v", rec, err)
	}
	comments := f.CommentsOf(deployRepo, n)
	if len(comments) != 1 || !strings.HasPrefix(comments[0].Body, "Release thread of alert-relay\n\n<!-- cutship:v1 {") {
		t.Fatalf("comments %+v", comments)
	}
	got, ok, err := New(f, deployRepo, link).Load(ctx, n)
	if err != nil || !ok || got.App != "alert-relay" || got.Tag != "v0.3.1" || string(got.Messages["telegram"]) != `{"post_id":5}` {
		t.Fatalf("%+v %v %v", got, ok, err)
	}
	if _, err := s.Update(ctx, n, apply(opened("e2"))); err != nil {
		t.Fatal(err)
	}
	if len(f.CommentsOf(deployRepo, n)) != 1 {
		t.Fatal("second comment")
	}
}

func TestStateReadsDeployNotifyV1(t *testing.T) {
	f, s, n := setup(t)
	old := `{"state":{"stage":"waiting","app":"alert-relay","to":"v0.3.0","seen":["e0"]},"ids":{"post_id":7,"copy_id":8,"comment_id":9}}`
	f.AddComment(deployRepo, n, "cutship-bot", "Telegram release thread\n\n<!-- deploy-notify:v1 "+old+" -->")
	rec, ok, err := s.Load(ctx, n)
	if err != nil || !ok || rec.Version != 1 || rec.App != "alert-relay" || rec.Tag != "v0.3.0" || rec.State.Stage != "waiting" {
		t.Fatalf("%+v %v %v", rec, ok, err)
	}
	if string(rec.Messages["telegram"]) != `{"post_id":7,"copy_id":8,"comment_id":9}` {
		t.Fatalf("messages %s", rec.Messages["telegram"])
	}
	if _, err := s.Update(ctx, n, func(r *Record) {}); err != nil {
		t.Fatal(err)
	}
	comments := f.CommentsOf(deployRepo, n)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "<!-- cutship:v1 ") || strings.Contains(comments[0].Body, "deploy-notify") {
		t.Fatalf("comments %+v", comments)
	}
}

func TestStateUpdateMergesConcurrentEvents(t *testing.T) {
	f, a, n := setup(t)
	if _, err := a.Update(ctx, n, apply(opened("e1"))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Load(ctx, n); err != nil {
		t.Fatal(err)
	}
	b := New(f, deployRepo, link)
	if _, err := b.Update(ctx, n, apply(event.Event{Kind: event.ChecksFinished, ID: "e2", At: t0.Add(time.Minute), Result: event.Success})); err != nil {
		t.Fatal(err)
	}
	rec, err := a.Update(ctx, n, apply(event.Event{Kind: event.Merged, ID: "e3", At: t0.Add(2 * time.Minute), Actor: "bot"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"e1", "e2", "e3"} {
		if !strings.Contains(strings.Join(rec.State.Seen, ","), id) {
			t.Errorf("%s missing from %v", id, rec.State.Seen)
		}
	}
}

func TestStateIgnoresOtherAuthors(t *testing.T) {
	f, s, n := setup(t)
	f.AddComment(deployRepo, n, "someone", "<!-- cutship:v1 {\"version\":1,\"app\":\"evil\"} -->")
	if _, ok, err := s.Load(ctx, n); ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if _, err := s.Update(ctx, n, apply(opened("e1"))); err != nil {
		t.Fatal(err)
	}
	if got := f.CommentsOf(deployRepo, n); len(got) != 2 || got[0].Body != "<!-- cutship:v1 {\"version\":1,\"app\":\"evil\"} -->" {
		t.Fatalf("%+v", got)
	}
}

func TestStateUnknownVersion(t *testing.T) {
	f, s, n := setup(t)
	f.AddComment(deployRepo, n, "cutship-bot", "<!-- cutship:v9 {} -->")
	if _, ok, err := s.Load(ctx, n); ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if _, err := s.Update(ctx, n, apply(opened("e1"))); err != nil {
		t.Fatal(err)
	}
	if got := f.CommentsOf(deployRepo, n); len(got) != 1 || !strings.Contains(got[0].Body, "cutship:v1") {
		t.Fatalf("%+v", got)
	}
}

func TestStateBadJSON(t *testing.T) {
	f, s, n := setup(t)
	f.AddComment(deployRepo, n, "cutship-bot", "<!-- cutship:v1 {not json -->")
	if _, ok, err := s.Load(ctx, n); ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	f.AddComment(deployRepo, 99, "cutship-bot", "<!-- cutship:v1 {\"version\":2} -->")
	if _, ok, _ := s.Load(ctx, 99); ok {
		t.Fatal("record version 2 accepted")
	}
}
