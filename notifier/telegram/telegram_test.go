package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ambrdk/cutship/notifier"
	"github.com/ambrdk/cutship/release"
	"github.com/ambrdk/cutship/render"
	"github.com/ambrdk/notifykit/thread"
)

var _ notifier.Notifier = (*Channel)(nil)

type fakeThread struct {
	got    []thread.IDs
	posts  []thread.Post
	next   thread.IDs
	status thread.Status
	err    error
}

func (f *fakeThread) Publish(_ context.Context, ids thread.IDs, p thread.Post) (thread.Outcome, error) {
	f.got = append(f.got, ids)
	f.posts = append(f.posts, p)
	return thread.Outcome{IDs: f.next, Status: f.status}, f.err
}

func view(stage release.Stage) render.View {
	return render.Build(release.State{Stage: stage, App: "alert-relay", To: "v0.3.1"})
}

func TestTelegramKey(t *testing.T) {
	if got := NewChannel("-100123", &fakeThread{}).Key(); got != "telegram:-100123" {
		t.Fatalf("%q", got)
	}
}

func TestTelegramPublishCreatesThenEdits(t *testing.T) {
	th := &fakeThread{next: thread.IDs{PostID: 5, CopyID: 6, CommentID: 7}, status: thread.Commented}
	ch := NewChannel("-100123", th)
	ref, status, err := ch.Publish(context.Background(), nil, view(release.Failed))
	if err != nil || status != notifier.OK {
		t.Fatalf("%s %v", status, err)
	}
	if th.got[0] != (thread.IDs{}) || !th.posts[0].Final || !th.posts[0].Loud || !strings.Contains(th.posts[0].Text, "Deploy failed: alert-relay v0.3.1") {
		t.Fatalf("%+v %+v", th.got, th.posts)
	}
	if !strings.Contains(th.posts[0].Comment(0), "📋 Timeline") {
		t.Fatalf("comment %q", th.posts[0].Comment(0))
	}
	if _, _, err := ch.Publish(context.Background(), ref, view(release.Deployed)); err != nil {
		t.Fatal(err)
	}
	if th.got[1] != (thread.IDs{PostID: 5, CopyID: 6, CommentID: 7}) {
		t.Fatalf("second publish ids %+v", th.got[1])
	}
	var ids thread.IDs
	if json.Unmarshal(ref, &ids) != nil || ids.PostID != 5 {
		t.Fatalf("ref %s", ref)
	}
}

func TestTelegramStatuses(t *testing.T) {
	for st, want := range map[thread.Status]notifier.Status{
		thread.Commented:    notifier.OK,
		thread.Posted:       notifier.OK,
		thread.CopyPending:  notifier.Pending,
		thread.NoCopy:       notifier.NoCopy,
		thread.NoDiscussion: notifier.NoDiscussion,
		thread.Unthreaded:   notifier.Unthreaded,
		thread.PostGone:     notifier.PostGone,
	} {
		_, got, _ := NewChannel("-1", &fakeThread{status: st}).Publish(context.Background(), nil, view(release.Opened))
		if got != want {
			t.Errorf("%s: got %s want %s", st, got, want)
		}
	}
}

func TestTelegramPartialFailureKeepsRef(t *testing.T) {
	th := &fakeThread{next: thread.IDs{PostID: 5}, err: errors.New("comment failed")}
	ref, _, err := NewChannel("-1", th).Publish(context.Background(), nil, view(release.Opened))
	if err == nil || !strings.Contains(string(ref), `"post_id":5`) {
		t.Fatalf("%s %v", ref, err)
	}
	if ref, _, err := NewChannel("-1", &fakeThread{err: errors.New("down")}).Publish(context.Background(), nil, view(release.Opened)); err == nil || ref != nil {
		t.Fatalf("%s %v", ref, err)
	}
}

func TestTelegramBadRef(t *testing.T) {
	if _, _, err := NewChannel("-1", &fakeThread{}).Publish(context.Background(), json.RawMessage(`{bad`), view(release.Opened)); err == nil {
		t.Fatal("bad ref accepted")
	}
}

func TestTelegramDone(t *testing.T) {
	ch := NewChannel("-1", &fakeThread{})
	for ref, want := range map[string]bool{
		`{"post_id":5,"comment_id":7}`: true,
		`{"post_id":5}`:                false,
		``:                             false,
		`{bad`:                         false,
	} {
		if got := ch.Done(json.RawMessage(ref)); got != want {
			t.Errorf("%s: %v", ref, got)
		}
	}
}

func TestPostLink(t *testing.T) {
	for _, c := range []struct {
		chat string
		ref  string
		want string
	}{
		{"-100123", `{"post_id":5}`, "https://t.me/c/123/5"},
		{"-100123", ``, ""},
		{"@channel", `{"post_id":5}`, ""},
	} {
		if got := NewChannel(c.chat, &fakeThread{}).Link(json.RawMessage(c.ref)); got != c.want {
			t.Errorf("%s %s: %q", c.chat, c.ref, got)
		}
	}
}
