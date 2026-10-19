package notify

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ambrdk/cutship/event"
	"github.com/ambrdk/cutship/release"
	"github.com/ambrdk/cutship/source"
	"github.com/ambrdk/cutship/store"
	"github.com/ambrdk/notifykit/telegram"
	"github.com/ambrdk/notifykit/telegram/telegramtest"
	"github.com/ambrdk/notifykit/thread"
)

const (
	channel   = "-1001"
	groupChat = "-1002"
)

var t0 = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

type env struct {
	f   *telegramtest.Fake
	th  *thread.Thread
	st  *store.Memory
	log *slog.Logger
	buf *strings.Builder
}

func newEnv() *env {
	f := telegramtest.New(channel, -1002)
	buf := &strings.Builder{}
	return &env{
		f:   f,
		th:  thread.New(f, channel, thread.Waiting(f, 0), thread.WithMaxWait(10*time.Millisecond)),
		st:  store.NewMemory(),
		log: slog.New(slog.NewTextHandler(buf, nil)),
		buf: buf,
	}
}

func (e *env) handle(events ...source.Anchored) error {
	return Handle(context.Background(), events, e.st, e.th, e.log)
}

func (e *env) comments() []telegramtest.Sent {
	var out []telegramtest.Sent
	for _, m := range e.f.Messages(groupChat) {
		if !m.Forwarded {
			out = append(out, m)
		}
	}
	return out
}

func anchored(pr int, kind event.Kind, id string, minute int) source.Anchored {
	return source.Anchored{PR: pr, Event: event.Event{Kind: kind, ID: id, At: t0.Add(time.Duration(minute) * time.Minute), App: "alert-relay", From: "v0.2.2", To: "v0.2.3", PR: pr, Actor: "ambrdk", Result: event.Success}}
}

func TestHandleCreatesThenEdits(t *testing.T) {
	e := newEnv()
	if err := e.handle(anchored(12, event.ReleaseOpened, "a", 0)); err != nil {
		t.Fatal(err)
	}
	first, _, _ := e.st.Load(context.Background(), 12)
	if err := e.handle(anchored(12, event.Merged, "b", 5)); err != nil {
		t.Fatal(err)
	}
	posts := e.f.Messages(channel)
	if len(posts) != 1 || !strings.HasPrefix(posts[0].Text, "🔀 <b>Merged: alert-relay v0.2.3</b>") {
		t.Fatalf("posts %+v", posts)
	}
	rec, ok, _ := e.st.Load(context.Background(), 12)
	if !ok || rec.State.Stage != release.Merged || rec.IDs != first.IDs || rec.IDs.CommentID == 0 {
		t.Fatalf("record %+v first %+v", rec, first.IDs)
	}
	if c := e.comments(); len(c) != 1 || c[0].ReplyTo != rec.IDs.CopyID {
		t.Fatalf("comments %+v", c)
	}
}

func TestHandleSequentialEventsKeepBoth(t *testing.T) {
	e := newEnv()
	_ = e.handle(anchored(12, event.Merged, "merged", 5))
	_ = e.handle(anchored(12, event.DeployStarted, "start", 5))
	rec, _, _ := e.st.Load(context.Background(), 12)
	var tags []string
	for _, l := range rec.State.Timeline {
		tags = append(tags, l.Tag)
	}
	c := e.comments()
	if strings.Join(tags, ",") != "MERGED,DEPLOY" || len(c) != 1 || !strings.Contains(c[0].Text, "[MERGED]") || !strings.Contains(c[0].Text, "[DEPLOY]") {
		t.Fatalf("tags %v comments %+v", tags, c)
	}
}

func TestHandleDuplicateIsNoop(t *testing.T) {
	e := newEnv()
	ev := anchored(12, event.ReleaseOpened, "a", 0)
	_ = e.handle(ev)
	sends, edits := e.f.Calls("sendMessage"), e.f.Calls("editMessageText")
	_ = e.handle(ev)
	if e.f.Calls("sendMessage") != sends || e.f.Calls("editMessageText") != edits {
		t.Fatalf("duplicate published: sends %d→%d edits %d→%d", sends, e.f.Calls("sendMessage"), edits, e.f.Calls("editMessageText"))
	}
}

func TestHandleDuplicateRetriesMissingComment(t *testing.T) {
	e := newEnv()
	e.f.HoldCopies(true)
	ev := anchored(12, event.ReleaseOpened, "a", 0)
	if err := e.handle(ev); err != nil {
		t.Fatal(err)
	}
	if rec, _, _ := e.st.Load(context.Background(), 12); rec.IDs.CommentID != 0 || !rec.IDs.CopyWaited {
		t.Fatalf("record %+v", rec.IDs)
	}
	if err := e.handle(ev); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := e.st.Load(context.Background(), 12)
	c := e.comments()
	if len(c) != 1 || c[0].ReplyTo != 0 || rec.IDs.CommentID != c[0].ID {
		t.Fatalf("comments %+v record %+v", c, rec.IDs)
	}
}

func TestHandleUnanchored(t *testing.T) {
	e := newEnv()
	finished := anchored(0, event.DeployFinished, "deploy:1:1:alert-relay", 7)
	started := anchored(0, event.DeployStarted, "deploy:1:1:start", 6)
	if err := e.handle(started, finished); err != nil {
		t.Fatal(err)
	}
	if posts := e.f.Messages(channel); len(posts) != 1 || !strings.HasPrefix(posts[0].Text, "✅") {
		t.Fatalf("posts %+v", posts)
	}
	if _, ok, _ := e.st.Load(context.Background(), 0); ok {
		t.Fatal("unanchored post saved state")
	}
}

func TestHandleKeepsStateOnPublishError(t *testing.T) {
	e := newEnv()
	e.f.FailChat(channel, 1, errors.New("telegram down"))
	ev := anchored(12, event.ReleaseOpened, "a", 0)
	if err := e.handle(ev); err == nil {
		t.Fatal("expected error")
	}
	if _, ok, _ := e.st.Load(context.Background(), 12); ok {
		t.Fatal("state saved after a failed publish")
	}
	if err := e.handle(ev); err != nil || len(e.f.Messages(channel)) != 1 {
		t.Fatalf("retry err %v posts %d", err, len(e.f.Messages(channel)))
	}
}

func TestHandleSavesIDsOnPartialFailure(t *testing.T) {
	e := newEnv()
	e.f.FailChat(groupChat, 1, &telegram.RateLimitedError{RetryAfter: time.Minute})
	ev := anchored(12, event.ReleaseOpened, "a", 0)
	var limited *telegram.RateLimitedError
	if err := e.handle(ev); !errors.As(err, &limited) {
		t.Fatalf("err %v", err)
	}
	if rec, _, _ := e.st.Load(context.Background(), 12); rec.IDs.PostID == 0 {
		t.Fatalf("post id not kept: %+v", rec)
	}
	if err := e.handle(ev); err != nil {
		t.Fatal(err)
	}
	if posts := e.f.Messages(channel); len(posts) != 1 || len(e.comments()) != 1 {
		t.Fatalf("posts %+v comments %+v", posts, e.comments())
	}
}

func TestHandleLogsNoCopy(t *testing.T) {
	e := newEnv()
	e.f.HoldCopies(true)
	if err := e.handle(anchored(12, event.ReleaseOpened, "a", 0)); err != nil {
		t.Fatal(err)
	}
	out := e.buf.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "admin of the discussion group") || !strings.Contains(out, "post_id=1") {
		t.Fatalf("log %q", out)
	}
}

func TestHandleQuietWhenCommented(t *testing.T) {
	e := newEnv()
	if err := e.handle(anchored(12, event.ReleaseOpened, "a", 0)); err != nil {
		t.Fatal(err)
	}
	if e.buf.Len() != 0 {
		t.Fatalf("log %q", e.buf.String())
	}
}

func TestHandleWithoutDiscussion(t *testing.T) {
	f := telegramtest.New(channel, 0)
	buf := &strings.Builder{}
	err := Handle(context.Background(), []source.Anchored{anchored(12, event.ReleaseOpened, "a", 0)}, store.NewMemory(), thread.New(f, channel, thread.Waiting(f, 0)), slog.New(slog.NewTextHandler(buf, nil)))
	if err != nil || len(f.Messages(channel)) != 1 || !strings.Contains(buf.String(), "no discussion group") {
		t.Fatalf("err %v posts %d log %q", err, len(f.Messages(channel)), buf.String())
	}
}
