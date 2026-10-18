package store

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ambrdk/deploy-notify/github"
	"github.com/ambrdk/deploy-notify/release"
	"github.com/ambrdk/notifykit/thread"
)

type comment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
}

type fakeGitHub struct {
	mu       sync.Mutex
	comments []comment
	posts    int
	patches  int
}

func (f *fakeGitHub) add(login, body string) {
	c := comment{ID: int64(len(f.comments) + 1), Body: body}
	c.User.Login = login
	f.comments = append(f.comments, c)
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/6/comments":
		_ = json.NewEncoder(w).Encode(f.comments)
	case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/6/comments":
		var in struct{ Body string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.posts++
		f.add("github-actions[bot]", in.Body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(f.comments[len(f.comments)-1])
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/repos/o/r/issues/comments/"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/repos/o/r/issues/comments/"), 10, 64)
		var in struct{ Body string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.patches++
		f.comments[id-1].Body = in.Body
		_ = json.NewEncoder(w).Encode(f.comments[id-1])
	default:
		http.NotFound(w, r)
	}
}

func newStore(t *testing.T, f *fakeGitHub) *PRComment {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return NewPRComment(github.New(srv.URL, "tok"), "o/r", func(r Record) string {
		return fmt.Sprintf("Telegram post %d", r.IDs.PostID)
	})
}

func record() Record {
	return Record{
		State: release.State{Stage: release.Merged, App: "alert-relay", To: "v0.2.3", Timeline: []release.Line{{Tag: "FAIL", Text: "task --> <b>"}}},
		IDs:   thread.IDs{PostID: 101, CopyID: 501, CommentID: 102},
	}
}

func TestPRCommentStoreRoundTrip(t *testing.T) {
	f := &fakeGitHub{}
	st := newStore(t, f)
	ctx := context.Background()
	if _, ok, err := st.Load(ctx, 6); ok || err != nil {
		t.Fatalf("empty load ok=%v err=%v", ok, err)
	}
	if err := st.Save(ctx, 6, record()); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.Load(ctx, 6)
	if err != nil || !ok || got.IDs != record().IDs || got.State.App != "alert-relay" || got.State.Timeline[0].Text != "task --> <b>" {
		t.Fatalf("load %+v ok=%v err=%v", got, ok, err)
	}
	if !strings.HasPrefix(f.comments[0].Body, "Telegram post 101\n\n<!-- deploy-notify:v1 ") || strings.Count(f.comments[0].Body, "-->") != 1 {
		t.Fatalf("body %q", f.comments[0].Body)
	}
	if err := st.Save(ctx, 6, record()); err != nil {
		t.Fatal(err)
	}
	if f.posts != 1 || f.patches != 1 {
		t.Fatalf("posts %d patches %d", f.posts, f.patches)
	}
}

func TestPRCommentStoreMarkerLost(t *testing.T) {
	f := &fakeGitHub{}
	f.add("github-actions[bot]", "Telegram post 101, someone removed the marker")
	f.add("ambrdk", "copy: <!-- deploy-notify:v1 {\"ids\":{\"post_id\":7}} -->")
	st := newStore(t, f)
	ctx := context.Background()
	if _, ok, err := st.Load(ctx, 6); ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if err := st.Save(ctx, 6, record()); err != nil {
		t.Fatal(err)
	}
	if f.posts != 1 || f.patches != 0 || len(f.comments) != 3 {
		t.Fatalf("posts %d patches %d comments %d", f.posts, f.patches, len(f.comments))
	}
}

func TestPRCommentStoreBadJSON(t *testing.T) {
	f := &fakeGitHub{}
	f.add("github-actions[bot]", "x\n\n<!-- deploy-notify:v1 {not json -->")
	st := newStore(t, f)
	if _, ok, err := st.Load(context.Background(), 6); ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if err := st.Save(context.Background(), 6, record()); err != nil {
		t.Fatal(err)
	}
	if f.posts != 0 || f.patches != 1 {
		t.Fatalf("posts %d patches %d", f.posts, f.patches)
	}
}

func TestPRCommentStoreUnknownVersion(t *testing.T) {
	f := &fakeGitHub{}
	f.add("github-actions[bot]", "x\n\n<!-- deploy-notify:v2 {} -->")
	st := newStore(t, f)
	if _, ok, err := st.Load(context.Background(), 6); ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestMemoryStore(t *testing.T) {
	st := NewMemory()
	ctx := context.Background()
	if _, ok, _ := st.Load(ctx, 6); ok {
		t.Fatal("empty memory store loaded")
	}
	_ = st.Save(ctx, 6, record())
	if got, ok, _ := st.Load(ctx, 6); !ok || got.IDs != record().IDs {
		t.Fatalf("got %+v", got)
	}
}
