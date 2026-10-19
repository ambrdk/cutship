package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ambrdk/cutship/forge"
)

var ctx = context.Background()

func serve(t *testing.T, routes map[string]http.HandlerFunc) *Client {
	t.Helper()
	mux := http.NewServeMux()
	for pattern, h := range routes {
		mux.HandleFunc(pattern, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func reply(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	}
}

func status(code int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = fmt.Fprint(w, body)
	}
}

func paged(pages ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page := 1
		_, _ = fmt.Sscan(r.URL.Query().Get("page"), &page)
		if page < len(pages) {
			next := *r.URL
			q := next.Query()
			q.Set("page", fmt.Sprint(page+1))
			next.RawQuery = q.Encode()
			w.Header().Set("Link", fmt.Sprintf(`<http://%s%s>; rel="next"`, r.Host, next.RequestURI()))
		}
		reply(pages[page-1])(w, r)
	}
}

func TestDefaultBranch(t *testing.T) {
	c := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r": reply(`{"default_branch":"main"}`)})
	if b, err := c.DefaultBranch(ctx, "o/r"); err != nil || b != "main" {
		t.Fatalf("%q %v", b, err)
	}
	if _, err := c.DefaultBranch(ctx, "nope"); err == nil {
		t.Fatal("bad repo accepted")
	}
}

func TestRepoSettings(t *testing.T) {
	c := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r": reply(`{"permissions":{"push":true},"allow_auto_merge":false,"delete_branch_on_merge":true}`)})
	s, err := c.RepoSettings(ctx, "o/r")
	if err != nil || s.Push == nil || !*s.Push || s.AutoMerge == nil || *s.AutoMerge || s.DeleteBranchOnMerge == nil || !*s.DeleteBranchOnMerge {
		t.Fatalf("%+v %v", s, err)
	}
	bare := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r": reply(`{}`)})
	if s, err := bare.RepoSettings(ctx, "o/r"); err != nil || s.Push != nil || s.AutoMerge != nil {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestFileAt(t *testing.T) {
	c := serve(t, map[string]http.HandlerFunc{
		"GET /repos/o/r/contents/apps/a/app.yml": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("ref") != "main" {
				t.Errorf("ref = %q", r.URL.Query().Get("ref"))
			}
			reply(`{"type":"file","encoding":"base64","content":"aW1hZ2U6IHgK"}`)(w, r)
		},
	})
	got, err := c.FileAt(ctx, "o/r", "apps/a/app.yml", "main")
	if err != nil || string(got) != "image: x\n" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestFileAtNotFound(t *testing.T) {
	c := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r/contents/x": status(404, `{"message":"Not Found"}`)})
	if _, err := c.FileAt(ctx, "o/r", "x", "main"); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveRef(t *testing.T) {
	c := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r/commits/main": func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "abc123")
	}})
	if sha, err := c.ResolveRef(ctx, "o/r", "main"); err != nil || sha != "abc123" {
		t.Fatalf("%q %v", sha, err)
	}
}

func TestOpenChangesByPrefix(t *testing.T) {
	c := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r/pulls": paged(
		`[{"number":1,"state":"open","head":{"ref":"bump/a-v1.0.0","sha":"s1"}},{"number":2,"state":"open","head":{"ref":"fix/x","sha":"s2"}}]`,
		`[{"number":3,"state":"open","head":{"ref":"bump/b-v2.0.0","sha":"s3"}}]`,
	)})
	got, err := c.OpenChanges(ctx, "o/r", "bump/")
	if err != nil {
		t.Fatal(err)
	}
	var numbers []int
	for _, ch := range got {
		numbers = append(numbers, ch.Number)
	}
	if !slices.Equal(numbers, []int{1, 3}) || got[1].HeadSHA != "s3" || got[0].State != forge.Open {
		t.Fatalf("%+v", got)
	}
}

func TestChangeStates(t *testing.T) {
	c := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r/pulls": func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("head") != "o:bump/a-v1.0.0" || r.URL.Query().Get("state") != "all" {
			t.Errorf("query %v", r.URL.Query())
		}
		reply(`[{"number":7,"title":"t","html_url":"u","state":"closed","merged_at":"2026-10-18T10:00:00Z","merge_commit_sha":"m7","merged_by":{"login":"bot"},"head":{"ref":"bump/a-v1.0.0","sha":"h7"}}]`)(w, r)
	}})
	ch, ok, err := c.ChangeByHead(ctx, "o/r", "bump/a-v1.0.0")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	want := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	if ch.State != forge.Merged || ch.MergeSHA != "m7" || ch.MergedBy != "bot" || ch.MergedAt == nil || !ch.MergedAt.Equal(want) || ch.URL != "u" {
		t.Fatalf("%+v", ch)
	}
	closed := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r/pulls": reply(`[{"number":8,"state":"closed","head":{"ref":"x"}}]`)})
	if ch, _, _ := closed.ChangeByHead(ctx, "o/r", "x"); ch.State != forge.Closed {
		t.Fatalf("%+v", ch)
	}
	none := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r/pulls": reply(`[]`)})
	if _, ok, err := none.ChangeByHead(ctx, "o/r", "x"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestGetChange(t *testing.T) {
	c := serve(t, map[string]http.HandlerFunc{
		"GET /repos/o/r/pulls/5": reply(`{"number":5,"state":"closed","merged_at":"2026-10-18T10:00:00Z","merged_by":{"login":"exassed"},"user":{"login":"pat"},"head":{"ref":"bump/a-v1.0.0","sha":"h5"}}`),
	})
	ch, err := c.GetChange(ctx, "o/r", 5)
	if err != nil || ch.MergedBy != "exassed" || ch.Author != "pat" || ch.State != forge.Merged {
		t.Fatalf("%+v %v", ch, err)
	}
	missing := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r/pulls/5": status(404, `{"message":"Not Found"}`)})
	if _, err := missing.GetChange(ctx, "o/r", 5); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestChangesForCommitAndFiles(t *testing.T) {
	c := serve(t, map[string]http.HandlerFunc{
		"GET /repos/o/r/commits/s1/pulls": reply(`[{"number":5,"state":"closed","merged_at":"2026-10-18T10:00:00Z","head":{"ref":"bump/a-v1.0.0","sha":"h5"}}]`),
		"GET /repos/o/r/pulls/5/files":    paged(`[{"filename":"apps/a/app.yml","status":"modified","patch":"-a\n+b"}]`, `[{"filename":"apps/a/env.j2","status":"added"}]`),
	})
	changes, err := c.ChangesForCommit(ctx, "o/r", "s1")
	if err != nil || len(changes) != 1 || changes[0].State != forge.Merged || changes[0].HeadSHA != "h5" {
		t.Fatalf("%+v %v", changes, err)
	}
	files, err := c.ChangeFiles(ctx, "o/r", 5)
	want := []forge.File{{Path: "apps/a/app.yml", Status: "modified", Patch: "-a\n+b"}, {Path: "apps/a/env.j2", Status: "added"}}
	if err != nil || !slices.Equal(files, want) {
		t.Fatalf("%+v %v", files, err)
	}
}

func TestChecksNormalized(t *testing.T) {
	c := serve(t, map[string]http.HandlerFunc{
		"GET /repos/o/r/commits/s1/check-runs": paged(
			`{"total_count":4,"check_runs":[{"name":"lint","status":"completed","conclusion":"failure","completed_at":"2026-10-18T10:00:00Z"},{"name":"lint","status":"completed","conclusion":"success","completed_at":"2026-10-18T10:05:00Z"}]}`,
			`{"total_count":4,"check_runs":[{"name":"docs","status":"completed","conclusion":"skipped","completed_at":"2026-10-18T10:00:00Z"},{"name":"slow","status":"in_progress"},{"name":"meh","status":"completed","conclusion":"neutral","completed_at":"2026-10-18T10:00:00Z"},{"name":"bad","status":"completed","conclusion":"timed_out","completed_at":"2026-10-18T10:00:00Z"}]}`,
		),
		"GET /repos/o/r/commits/s1/status": reply(`{"state":"pending","statuses":[{"context":"ext","state":"error","updated_at":"2026-10-18T10:00:00Z"},{"context":"wait","state":"pending"}]}`),
	})
	checks, err := c.Checks(ctx, "o/r", "s1")
	if err != nil {
		t.Fatal(err)
	}
	state := func(name string) forge.CheckState {
		got, ok := forge.Latest(checks, name)
		if !ok {
			t.Fatalf("no %s in %+v", name, checks)
		}
		return got.State
	}
	for name, want := range map[string]forge.CheckState{
		"lint": forge.Success, "docs": forge.Success, "meh": forge.Success, "slow": forge.Pending,
		"bad": forge.Failure, "ext": forge.Failure, "wait": forge.Pending,
	} {
		if got := state(name); got != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
}

func TestLatestPendingWins(t *testing.T) {
	t0 := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	checks := []forge.Check{{Name: "lint", State: forge.Success, CompletedAt: t0}, {Name: "lint", State: forge.Pending}}
	if got, _ := forge.Latest(checks, "lint"); got.State != forge.Pending {
		t.Fatalf("%+v", got)
	}
	if _, ok := forge.Latest(checks, "other"); ok {
		t.Fatal("found other")
	}
}

func TestChangedPathsPaginates(t *testing.T) {
	c := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r/compare/b...h": paged(
		`{"files":[{"filename":"apps/a/app.yml"},{"filename":"README.md"}]}`,
		`{"files":[{"filename":"apps/b/env.j2"},{"filename":"README.md"}]}`,
	)})
	got, err := c.ChangedPaths(ctx, "o/r", "b", "h")
	if err != nil || !slices.Equal(got, []string{"README.md", "apps/a/app.yml", "apps/b/env.j2"}) {
		t.Fatalf("%v %v", got, err)
	}
}

func TestChangedPathsRenameHasBothPaths(t *testing.T) {
	c := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r/compare/b...h": reply(`{"files":[{"filename":"apps/b/app.yml","previous_filename":"apps/a/app.yml","status":"renamed"}]}`)})
	got, err := c.ChangedPaths(ctx, "o/r", "b", "h")
	if err != nil || !slices.Equal(got, []string{"apps/a/app.yml", "apps/b/app.yml"}) {
		t.Fatalf("%v %v", got, err)
	}
}

func TestSubtrees(t *testing.T) {
	c := serve(t, map[string]http.HandlerFunc{
		"GET /repos/o/r/contents/apps": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("ref") != "s1" {
				t.Errorf("ref %q", r.URL.Query().Get("ref"))
			}
			reply(`[{"type":"dir","name":"a","sha":"ta"},{"type":"file","name":"README.md","sha":"f"},{"type":"dir","name":"b","sha":"tb"}]`)(w, r)
		},
	})
	got, err := c.Subtrees(ctx, "o/r", "apps", "s1")
	if err != nil || len(got) != 2 || got["a"] != "ta" || got["b"] != "tb" {
		t.Fatalf("%v %v", got, err)
	}
	missing := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r/contents/apps": status(404, `{"message":"Not Found"}`)})
	if got, err := missing.Subtrees(ctx, "o/r", "apps", "s1"); err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}

func TestChangedPathsTooManyAcrossPages(t *testing.T) {
	page := func(from int) string {
		files := make([]string, 200)
		for i := range files {
			files[i] = fmt.Sprintf(`{"filename":"f%d"}`, from+i)
		}
		return `{"files":[` + strings.Join(files, ",") + `]}`
	}
	c := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r/compare/b...h": paged(page(0), page(200))})
	if _, err := c.ChangedPaths(ctx, "o/r", "b", "h"); !errors.Is(err, forge.ErrTooMany) {
		t.Fatalf("err = %v", err)
	}
}

func TestChangedPathsTooMany(t *testing.T) {
	files := make([]string, 300)
	for i := range files {
		files[i] = fmt.Sprintf(`{"filename":"f%d"}`, i)
	}
	c := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r/compare/b...h": reply(`{"files":[` + strings.Join(files, ",") + `]}`)})
	if _, err := c.ChangedPaths(ctx, "o/r", "b", "h"); !errors.Is(err, forge.ErrTooMany) {
		t.Fatalf("err = %v", err)
	}
}

func TestComments(t *testing.T) {
	c := serve(t, map[string]http.HandlerFunc{"GET /repos/o/r/issues/5/comments": paged(`[{"id":1,"body":"a","user":{"login":"x"}}]`, `[{"id":2,"body":"b","user":{"login":"y"}}]`)})
	got, err := c.Comments(ctx, "o/r", 5)
	want := []forge.Comment{{ID: 1, Author: "x", Body: "a"}, {ID: 2, Author: "y", Body: "b"}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestViewer(t *testing.T) {
	user := serve(t, map[string]http.HandlerFunc{"GET /user": reply(`{"login":"exassed"}`)})
	if got, err := user.Viewer(ctx); err != nil || got != "exassed" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestViewerActionsToken(t *testing.T) {
	actions := serve(t, map[string]http.HandlerFunc{"GET /user": status(403, `{"message":"Resource not accessible by integration"}`)})
	if got, err := actions.Viewer(ctx); err != nil || got != ActionsBot {
		t.Fatalf("%q %v", got, err)
	}
	broken := serve(t, map[string]http.HandlerFunc{"GET /user": status(401, `{"message":"Bad credentials"}`)})
	if _, err := broken.Viewer(ctx); err == nil {
		t.Fatal("bad credentials accepted")
	}
	limited := serve(t, map[string]http.HandlerFunc{"GET /user": status(403, `{"message":"API rate limit exceeded"}`)})
	if _, err := limited.Viewer(ctx); err == nil {
		t.Fatal("other 403 taken for the actions bot")
	}
}
