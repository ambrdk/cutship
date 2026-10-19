package github

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ambrdk/cutship/forge"
)

type recorder struct {
	mu    sync.Mutex
	calls []string
	body  map[string]map[string]any
}

func (r *recorder) has(call string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.calls, call)
}

func record(t *testing.T, routes map[string]http.HandlerFunc) (*Client, *recorder) {
	t.Helper()
	rec := &recorder{body: map[string]map[string]any{}}
	mux := http.NewServeMux()
	for pattern, h := range routes {
		mux.HandleFunc(pattern, h)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := r.Method + " " + r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		rec.mu.Lock()
		rec.calls = append(rec.calls, call)
		rec.body[call] = body
		rec.mu.Unlock()
		r.Body = io.NopCloser(strings.NewReader(string(raw)))
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	return c, rec
}

func TestCreateTagNew(t *testing.T) {
	c, rec := record(t, map[string]http.HandlerFunc{
		"GET /repos/o/r/git/ref/tags/v1.0.0": status(404, `{"message":"Not Found"}`),
		"POST /repos/o/r/git/refs":           status(201, `{"ref":"refs/tags/v1.0.0","object":{"sha":"s1"}}`),
	})
	if err := c.CreateTag(ctx, "o/r", "v1.0.0", "s1"); err != nil {
		t.Fatal(err)
	}
	if b := rec.body["POST /repos/o/r/git/refs"]; b["ref"] != "refs/tags/v1.0.0" || b["sha"] != "s1" {
		t.Fatalf("body %v", b)
	}
}

func TestCreateTagIdempotent(t *testing.T) {
	c, rec := record(t, map[string]http.HandlerFunc{
		"GET /repos/o/r/git/ref/tags/v1.0.0": reply(`{"ref":"refs/tags/v1.0.0","object":{"type":"commit","sha":"s1"}}`),
		"GET /repos/o/r/git/ref/tags/v2.0.0": reply(`{"ref":"refs/tags/v2.0.0","object":{"type":"tag","sha":"t2"}}`),
		"GET /repos/o/r/git/tags/t2":         reply(`{"sha":"t2","object":{"type":"commit","sha":"s1"}}`),
	})
	for _, tag := range []string{"v1.0.0", "v2.0.0"} {
		if err := c.CreateTag(ctx, "o/r", tag, "s1"); err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
	}
	if rec.has("POST /repos/o/r/git/refs") {
		t.Fatal("tag created again")
	}
}

func TestCreateTagElsewhere(t *testing.T) {
	c, _ := record(t, map[string]http.HandlerFunc{
		"GET /repos/o/r/git/ref/tags/v1.0.0": reply(`{"ref":"refs/tags/v1.0.0","object":{"type":"commit","sha":"s2"}}`),
	})
	if err := c.CreateTag(ctx, "o/r", "v1.0.0", "s1"); !errors.Is(err, forge.ErrTagElsewhere) {
		t.Fatalf("err = %v", err)
	}
}

func pushRoutes(branch http.HandlerFunc, branchCommit string, pulls string) map[string]http.HandlerFunc {
	routes := map[string]http.HandlerFunc{
		"GET /repos/o/r/git/ref/heads/main":             reply(`{"object":{"sha":"b1"}}`),
		"GET /repos/o/r/git/commits/b1":                 reply(`{"sha":"b1","tree":{"sha":"t0"}}`),
		"POST /repos/o/r/git/trees":                     status(201, `{"sha":"t1"}`),
		"GET /repos/o/r/git/ref/heads/bump/a-v1.0.0":    branch,
		"POST /repos/o/r/git/commits":                   status(201, `{"sha":"c1"}`),
		"POST /repos/o/r/git/refs":                      status(201, `{"object":{"sha":"c1"}}`),
		"PATCH /repos/o/r/git/refs/heads/bump/a-v1.0.0": reply(`{"object":{"sha":"c1"}}`),
		"GET /repos/o/r/pulls":                          reply(pulls),
		"POST /repos/o/r/pulls":                         status(201, `{"number":9,"state":"open","html_url":"u9","head":{"ref":"bump/a-v1.0.0","sha":"c1"}}`),
	}
	if branchCommit != "" {
		routes["GET /repos/o/r/git/commits/c0"] = reply(`{"sha":"c0","tree":{"sha":"` + branchCommit + `"}}`)
	}
	return routes
}

var bumpRequest = forge.ChangeRequest{
	Base:    "main",
	Branch:  "bump/a-v1.0.0",
	Title:   "chore(apps): bump a to v1.0.0",
	Message: "chore(apps): bump a to v1.0.0",
	Files:   map[string][]byte{"apps/a/app.yml": []byte("image: x:v1.0.0\n")},
}

func TestPushChangeCreatesBranchAndPR(t *testing.T) {
	c, rec := record(t, pushRoutes(status(404, `{"message":"Not Found"}`), "", `[]`))
	ch, err := c.PushChange(ctx, "o/r", bumpRequest)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Number != 9 || ch.HeadSHA != "c1" || ch.State != forge.Open {
		t.Fatalf("%+v", ch)
	}
	tree := rec.body["POST /repos/o/r/git/trees"]
	entries, _ := tree["tree"].([]any)
	if tree["base_tree"] != "t0" || len(entries) != 1 {
		t.Fatalf("tree %v", tree)
	}
	entry, _ := entries[0].(map[string]any)
	if entry["path"] != "apps/a/app.yml" || entry["content"] != "image: x:v1.0.0\n" || entry["mode"] != "100644" {
		t.Fatalf("entry %v", entry)
	}
	commit := rec.body["POST /repos/o/r/git/commits"]
	if commit["tree"] != "t1" || commit["message"] != bumpRequest.Message || !strings.Contains(strings.Join(anyStrings(commit["parents"]), ","), "b1") {
		t.Fatalf("commit %v", commit)
	}
	if ref := rec.body["POST /repos/o/r/git/refs"]; ref["ref"] != "refs/heads/bump/a-v1.0.0" || ref["sha"] != "c1" {
		t.Fatalf("ref %v", ref)
	}
	if pr := rec.body["POST /repos/o/r/pulls"]; pr["head"] != "bump/a-v1.0.0" || pr["base"] != "main" || pr["title"] != bumpRequest.Title {
		t.Fatalf("pull %v", pr)
	}
}

func anyStrings(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestPushChangeReusesOpenPR(t *testing.T) {
	open := `[{"number":9,"state":"open","head":{"ref":"bump/a-v1.0.0","sha":"c0"}}]`
	c, rec := record(t, pushRoutes(reply(`{"object":{"sha":"c0"}}`), "t_old", open))
	ch, err := c.PushChange(ctx, "o/r", bumpRequest)
	if err != nil || ch.Number != 9 || ch.HeadSHA != "c1" {
		t.Fatalf("%+v %v", ch, err)
	}
	if !rec.has("PATCH /repos/o/r/git/refs/heads/bump/a-v1.0.0") || rec.has("POST /repos/o/r/pulls") {
		t.Fatalf("calls %v", rec.calls)
	}
	if force := rec.body["PATCH /repos/o/r/git/refs/heads/bump/a-v1.0.0"]["force"]; force != true {
		t.Fatalf("force %v", force)
	}
}

func TestPushChangeSameTreeNoPush(t *testing.T) {
	open := `[{"number":9,"state":"open","head":{"ref":"bump/a-v1.0.0","sha":"c0"}}]`
	c, rec := record(t, pushRoutes(reply(`{"object":{"sha":"c0"}}`), "t1", open))
	ch, err := c.PushChange(ctx, "o/r", bumpRequest)
	if err != nil || ch.Number != 9 || ch.HeadSHA != "c0" {
		t.Fatalf("%+v %v", ch, err)
	}
	if rec.has("POST /repos/o/r/git/commits") || rec.has("PATCH /repos/o/r/git/refs/heads/bump/a-v1.0.0") {
		t.Fatalf("calls %v", rec.calls)
	}
}

func TestPushChangeClosedPRGetsNewOne(t *testing.T) {
	closed := `[{"number":8,"state":"closed","head":{"ref":"bump/a-v1.0.0","sha":"c0"}}]`
	c, rec := record(t, pushRoutes(reply(`{"object":{"sha":"c0"}}`), "t_old", closed))
	if ch, err := c.PushChange(ctx, "o/r", bumpRequest); err != nil || ch.Number != 9 {
		t.Fatalf("%+v %v", ch, err)
	}
	if !rec.has("POST /repos/o/r/pulls") {
		t.Fatalf("calls %v", rec.calls)
	}
}

func autoMergeRoutes(graphql string) map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"GET /repos/o/r/pulls/9":       reply(`{"number":9,"node_id":"PR_9","state":"open","head":{"ref":"bump/a-v1.0.0","sha":"c1"}}`),
		"POST /graphql":                reply(graphql),
		"PUT /repos/o/r/pulls/9/merge": reply(`{"merged":true}`),
	}
}

func TestAutoMergeEnabled(t *testing.T) {
	c, rec := record(t, autoMergeRoutes(`{"data":{"enablePullRequestAutoMerge":{"clientMutationId":null}}}`))
	if err := c.AutoMerge(ctx, "o/r", 9); err != nil {
		t.Fatal(err)
	}
	vars, _ := rec.body["POST /graphql"]["variables"].(map[string]any)
	if vars["id"] != "PR_9" || rec.has("PUT /repos/o/r/pulls/9/merge") {
		t.Fatalf("body %v calls %v", rec.body["POST /graphql"], rec.calls)
	}
}

func TestAutoMergeCleanStatusMerges(t *testing.T) {
	c, rec := record(t, autoMergeRoutes(`{"data":null,"errors":[{"message":"Pull request Pull request is in clean status"}]}`))
	if err := c.AutoMerge(ctx, "o/r", 9); err != nil {
		t.Fatal(err)
	}
	if rec.body["PUT /repos/o/r/pulls/9/merge"]["merge_method"] != "squash" {
		t.Fatalf("merge %v", rec.body["PUT /repos/o/r/pulls/9/merge"])
	}
}

func TestAutoMergeDisabledHint(t *testing.T) {
	c, _ := record(t, autoMergeRoutes(`{"data":null,"errors":[{"message":"Pull request Auto merge is not allowed for this repository"}]}`))
	if err := c.AutoMerge(ctx, "o/r", 9); err == nil || !strings.Contains(err.Error(), "Allow auto-merge") {
		t.Fatalf("err = %v", err)
	}
}

func TestCloseChangeBranchGone(t *testing.T) {
	c, rec := record(t, map[string]http.HandlerFunc{
		"GET /repos/o/r/pulls/9":                         reply(`{"number":9,"state":"open","head":{"ref":"bump/a-v0.9.0"}}`),
		"PATCH /repos/o/r/pulls/9":                       reply(`{"number":9,"state":"closed"}`),
		"DELETE /repos/o/r/git/refs/heads/bump/a-v0.9.0": status(422, `{"message":"Reference does not exist"}`),
	})
	if err := c.CloseChange(ctx, "o/r", 9, true); err != nil {
		t.Fatal(err)
	}
	if rec.body["PATCH /repos/o/r/pulls/9"]["state"] != "closed" || !rec.has("DELETE /repos/o/r/git/refs/heads/bump/a-v0.9.0") {
		t.Fatalf("calls %v", rec.calls)
	}
}

func TestWriteComments(t *testing.T) {
	c, rec := record(t, map[string]http.HandlerFunc{
		"POST /repos/o/r/issues/9/comments":  status(201, `{"id":5,"body":"a","user":{"login":"bot"}}`),
		"PATCH /repos/o/r/issues/comments/5": reply(`{"id":5,"body":"b"}`),
	})
	cm, err := c.CreateComment(ctx, "o/r", 9, "a")
	if err != nil || cm.ID != 5 || cm.Author != "bot" {
		t.Fatalf("%+v %v", cm, err)
	}
	if err := c.UpdateComment(ctx, "o/r", 5, "b"); err != nil || rec.body["PATCH /repos/o/r/issues/comments/5"]["body"] != "b" {
		t.Fatalf("%v %v", err, rec.body)
	}
}
