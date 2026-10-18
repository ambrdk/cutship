package notify

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ambrdk/deploy-notify/event"
	"github.com/ambrdk/deploy-notify/release"
	"github.com/ambrdk/deploy-notify/source"
	"github.com/ambrdk/deploy-notify/store"
	"github.com/ambrdk/notifykit/telegram"
	"github.com/ambrdk/notifykit/thread"
)

var t0 = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

type call struct {
	ids     thread.IDs
	post    string
	comment string
}

type fakeMessenger struct {
	calls []call
	errs  []error
}

func (f *fakeMessenger) Publish(_ context.Context, ids thread.IDs, post, comment string) (thread.IDs, error) {
	f.calls = append(f.calls, call{ids: ids, post: post, comment: comment})
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return ids, err
		}
	}
	if ids.PostID == 0 {
		ids = thread.IDs{PostID: 101, CopyID: 501, CommentID: 102}
	}
	return ids, nil
}

func anchored(pr int, kind event.Kind, id string, minute int) source.Anchored {
	return source.Anchored{PR: pr, Event: event.Event{Kind: kind, ID: id, At: t0.Add(time.Duration(minute) * time.Minute), App: "alert-relay", From: "v0.2.2", To: "v0.2.3", PR: pr, Actor: "ambrdk", Result: event.Success}}
}

func TestHandleCreatesThenEdits(t *testing.T) {
	st, m := store.NewMemory(), &fakeMessenger{}
	ctx := context.Background()
	if err := Handle(ctx, []source.Anchored{anchored(12, event.ReleaseOpened, "a", 0)}, st, m); err != nil {
		t.Fatal(err)
	}
	if err := Handle(ctx, []source.Anchored{anchored(12, event.Merged, "b", 5)}, st, m); err != nil {
		t.Fatal(err)
	}
	if len(m.calls) != 2 || m.calls[0].ids != (thread.IDs{}) || m.calls[1].ids != (thread.IDs{PostID: 101, CopyID: 501, CommentID: 102}) {
		t.Fatalf("calls %+v", m.calls)
	}
	if !strings.HasPrefix(m.calls[1].post, "🔀 <b>Merged: alert-relay v0.2.3</b>") {
		t.Fatalf("post %q", m.calls[1].post)
	}
	rec, ok, _ := st.Load(ctx, 12)
	if !ok || rec.State.Stage != release.Merged || rec.IDs.PostID != 101 {
		t.Fatalf("record %+v", rec)
	}
}

func TestHandleSequentialEventsKeepBoth(t *testing.T) {
	st, m := store.NewMemory(), &fakeMessenger{}
	ctx := context.Background()
	_ = Handle(ctx, []source.Anchored{anchored(12, event.Merged, "merged", 5)}, st, m)
	_ = Handle(ctx, []source.Anchored{anchored(12, event.DeployStarted, "start", 5)}, st, m)
	rec, _, _ := st.Load(ctx, 12)
	var tags []string
	for _, l := range rec.State.Timeline {
		tags = append(tags, l.Tag)
	}
	if strings.Join(tags, ",") != "MERGED,DEPLOY" || !strings.Contains(m.calls[1].comment, "[MERGED]") || !strings.Contains(m.calls[1].comment, "[DEPLOY]") {
		t.Fatalf("tags %v comment %q", tags, m.calls[1].comment)
	}
}

func TestHandleDuplicateIsNoop(t *testing.T) {
	st, m := store.NewMemory(), &fakeMessenger{}
	e := anchored(12, event.ReleaseOpened, "a", 0)
	_ = Handle(context.Background(), []source.Anchored{e}, st, m)
	_ = Handle(context.Background(), []source.Anchored{e}, st, m)
	if len(m.calls) != 1 {
		t.Fatalf("calls %d", len(m.calls))
	}
}

func TestHandleUnanchored(t *testing.T) {
	st, m := store.NewMemory(), &fakeMessenger{}
	finished := anchored(0, event.DeployFinished, "deploy:1:1:alert-relay", 7)
	started := anchored(0, event.DeployStarted, "deploy:1:1:start", 6)
	if err := Handle(context.Background(), []source.Anchored{started, finished}, st, m); err != nil {
		t.Fatal(err)
	}
	if len(m.calls) != 1 || m.calls[0].ids != (thread.IDs{}) || !strings.HasPrefix(m.calls[0].post, "✅") {
		t.Fatalf("calls %+v", m.calls)
	}
	if _, ok, _ := st.Load(context.Background(), 0); ok {
		t.Fatal("unanchored post saved state")
	}
}

func TestHandleKeepsStateOnPublishError(t *testing.T) {
	st, m := store.NewMemory(), &fakeMessenger{errs: []error{errors.New("telegram down")}}
	e := anchored(12, event.ReleaseOpened, "a", 0)
	if err := Handle(context.Background(), []source.Anchored{e}, st, m); err == nil {
		t.Fatal("expected error")
	}
	if _, ok, _ := st.Load(context.Background(), 12); ok {
		t.Fatal("state saved after a failed publish")
	}
	if err := Handle(context.Background(), []source.Anchored{e}, st, m); err != nil || len(m.calls) != 2 {
		t.Fatalf("retry err %v calls %d", err, len(m.calls))
	}
}

type partialMessenger struct{ calls []thread.IDs }

func (p *partialMessenger) Publish(_ context.Context, ids thread.IDs, _, _ string) (thread.IDs, error) {
	p.calls = append(p.calls, ids)
	if len(p.calls) == 1 {
		return thread.IDs{PostID: 101}, errors.New("comment failed")
	}
	return thread.IDs{PostID: 101, CopyID: 501, CommentID: 102}, nil
}

func TestHandleSavesIDsOnPartialFailure(t *testing.T) {
	st, m := store.NewMemory(), &partialMessenger{}
	e := anchored(12, event.ReleaseOpened, "a", 0)
	if err := Handle(context.Background(), []source.Anchored{e}, st, m); err == nil {
		t.Fatal("expected error")
	}
	if err := Handle(context.Background(), []source.Anchored{e}, st, m); err != nil {
		t.Fatal(err)
	}
	if m.calls[1] != (thread.IDs{PostID: 101}) {
		t.Fatalf("second publish got %+v, a new post would be sent", m.calls[1])
	}
}

type noCopyMessenger struct{}

func (noCopyMessenger) Publish(_ context.Context, _ thread.IDs, _, _ string) (thread.IDs, error) {
	return thread.IDs{PostID: 101}, nil
}

func TestWarnWithoutComment(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, nil))
	if _, err := WarnWithoutComment(noCopyMessenger{}, log, true).Publish(context.Background(), thread.IDs{}, "p", "c"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "level=WARN") || !strings.Contains(buf.String(), "admin of the discussion group") || !strings.Contains(buf.String(), "post_id=101") {
		t.Fatalf("log %q", buf.String())
	}
	buf.Reset()
	_, _ = WarnWithoutComment(noCopyMessenger{}, log, false).Publish(context.Background(), thread.IDs{}, "p", "c")
	_, _ = WarnWithoutComment(&fakeMessenger{}, log, true).Publish(context.Background(), thread.IDs{}, "p", "c")
	if buf.Len() != 0 {
		t.Fatalf("unexpected log %q", buf.String())
	}
}

func TestRetryOnRateLimit(t *testing.T) {
	m := &fakeMessenger{errs: []error{&telegram.RateLimitedError{RetryAfter: 10 * time.Millisecond}, nil}}
	ids, err := Retry(m, time.Second).Publish(context.Background(), thread.IDs{}, "p", "c")
	if err != nil || ids.PostID != 101 || len(m.calls) != 2 {
		t.Fatalf("ids %+v err %v calls %d", ids, err, len(m.calls))
	}
	m = &fakeMessenger{errs: []error{&telegram.RateLimitedError{RetryAfter: time.Minute}}}
	if _, err := Retry(m, time.Second).Publish(context.Background(), thread.IDs{}, "p", "c"); err == nil || len(m.calls) != 1 {
		t.Fatalf("long retry err %v calls %d", err, len(m.calls))
	}
}
