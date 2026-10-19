package source

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ambrdk/cutship/event"
	"github.com/ambrdk/cutship/github"
)

const repo = "ambrdk/ansible"

type fakeGitHub struct {
	files    map[string]string
	commits  map[string]string
	heads    map[string]string
	jobs     map[int64]string
	reports  map[int64]map[string]string
	prFiles  map[string]string
	requests []string
}

func (f *fakeGitHub) server(t *testing.T) *github.API {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.Path)
		p := strings.TrimPrefix(r.URL.Path, "/repos/"+repo)
		switch {
		case strings.HasPrefix(p, "/contents/"):
			body, ok := f.files[r.URL.Query().Get("ref")+":"+strings.TrimPrefix(p, "/contents/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = fmt.Fprint(w, body)
		case strings.HasPrefix(p, "/commits/") && strings.HasSuffix(p, "/pulls"):
			_, _ = fmt.Fprint(w, orEmpty(f.commits[strings.TrimSuffix(strings.TrimPrefix(p, "/commits/"), "/pulls")]))
		case p == "/pulls":
			_, _ = fmt.Fprint(w, orEmpty(f.heads[r.URL.Query().Get("head")]))
		case strings.HasPrefix(p, "/pulls/") && strings.HasSuffix(p, "/files"):
			_, _ = fmt.Fprint(w, orEmpty(f.prFiles[strings.TrimSuffix(strings.TrimPrefix(p, "/pulls/"), "/files")]))
		case strings.HasSuffix(p, "/jobs"):
			var id int64
			_, _ = fmt.Sscanf(p, "/actions/runs/%d/jobs", &id)
			_, _ = fmt.Fprintf(w, `{"jobs":%s}`, orEmpty(f.jobs[id]))
		case strings.HasSuffix(p, "/artifacts"):
			var id int64
			_, _ = fmt.Sscanf(p, "/actions/runs/%d/artifacts", &id)
			if _, ok := f.reports[id]; !ok {
				_, _ = fmt.Fprint(w, `{"artifacts":[]}`)
				return
			}
			_, _ = fmt.Fprintf(w, `{"artifacts":[{"name":"deploy-report","archive_download_url":"%s/zip/%d"}]}`, srv.URL, id)
		case strings.HasPrefix(r.URL.Path, "/zip/"):
			var id int64
			_, _ = fmt.Sscanf(r.URL.Path, "/zip/%d", &id)
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			for name, body := range f.reports[id] {
				fw, _ := zw.Create(name)
				_, _ = fw.Write([]byte(body))
			}
			_ = zw.Close()
			_, _ = w.Write(buf.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return github.New(srv.URL, "tok")
}

func orEmpty(s string) string {
	if s == "" {
		return "[]"
	}
	return s
}

func image(tag string) string {
	return "---\nimage: ghcr.io/ambrdk/alert-relay:" + tag + "\nvolumes:\n  - alert-relay-data\n"
}

func prJSON(action string, merged bool, head string) string {
	mergedBy := "null"
	if merged {
		mergedBy = `{"login":"ambrdk"}`
	}
	return fmt.Sprintf(`{"action":%q,"number":12,"pull_request":{"number":12,"title":"chore(apps): bump alert-relay to v0.2.3","html_url":"https://github.com/ambrdk/ansible/pull/12",
		"created_at":"2026-10-18T10:01:02Z","updated_at":"2026-10-18T10:02:00Z","closed_at":"2026-10-18T10:05:11Z","merged":%t,"merged_at":"2026-10-18T10:05:11Z",
		"merge_commit_sha":"m1","merged_by":%s,"head":{"ref":%q,"sha":"h1"},"base":{"ref":"main","sha":"b1"}}}`, action, merged, mergedBy, head)
}

func TestParsePullRequest(t *testing.T) {
	f := &fakeGitHub{files: map[string]string{"b1:apps/alert-relay/app.yml": image("v0.2.2"), "h1:apps/alert-relay/app.yml": image("v0.2.3")}}
	api := f.server(t)
	cases := []struct {
		name    string
		payload string
		kind    event.Kind
	}{
		{"opened", prJSON("opened", false, "bump/alert-relay-v0.2.3"), event.ReleaseOpened},
		{"reopened", prJSON("reopened", false, "bump/alert-relay-v0.2.3"), event.Reopened},
		{"merged", prJSON("closed", true, "bump/alert-relay-v0.2.3"), event.Merged},
		{"closed", prJSON("closed", false, "bump/alert-relay-v0.2.3"), event.ClosedUnmerged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(context.Background(), api, repo, "pull_request", []byte(tc.payload))
			if err != nil || len(got) != 1 {
				t.Fatalf("got %+v err %v", got, err)
			}
			e := got[0].Event
			if got[0].PR != 12 || e.Kind != tc.kind || e.App != "alert-relay" || e.From != "v0.2.2" || e.To != "v0.2.3" || e.Image != "ghcr.io/ambrdk/alert-relay" || e.PRURL != "https://github.com/ambrdk/ansible/pull/12" {
				t.Fatalf("event %+v", e)
			}
			if !strings.HasPrefix(e.ID, "pull_request:") || !strings.HasSuffix(e.ID, ":12:h1") {
				t.Fatalf("id %s", e.ID)
			}
			if tc.kind == event.Merged && (e.Actor != "ambrdk" || e.Commit != "m1") {
				t.Fatalf("merged %+v", e)
			}
		})
	}
	got, err := Parse(context.Background(), api, repo, "pull_request", []byte(prJSON("opened", false, "fix/env")))
	if err != nil || len(got) != 0 {
		t.Fatalf("non bump %+v err %v", got, err)
	}
	got, err = Parse(context.Background(), api, repo, "pull_request", []byte(prJSON("opened", false, "bump/notes")))
	if err != nil || len(got) != 0 {
		t.Fatalf("bump prefix without version %+v err %v", got, err)
	}
}

func TestPullRequestToFromBranch(t *testing.T) {
	f := &fakeGitHub{files: map[string]string{"b1:apps/alert-relay/app.yml": image("v0.2.2"), "h1:apps/alert-relay/app.yml": image("v0.2.4")}}
	got, err := Parse(context.Background(), f.server(t), repo, "pull_request", []byte(prJSON("opened", false, "bump/alert-relay-v0.2.3")))
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v err %v", got, err)
	}
	if e := got[0].Event; e.To != "v0.2.3" || e.From != "v0.2.2" || e.Image != "ghcr.io/ambrdk/alert-relay" {
		t.Fatalf("event %+v", e)
	}
}

func runJSON(name, action, ev, conclusion string, id int64, attempt int, prs string) string {
	return fmt.Sprintf(`{"action":%q,"workflow_run":{"id":%d,"name":%q,"event":%q,"status":"completed","conclusion":%q,"head_sha":"m1","run_attempt":%d,
		"html_url":"https://github.com/ambrdk/ansible/actions/runs/%d","run_started_at":"2026-10-18T10:05:14Z","updated_at":"2026-10-18T10:06:22Z","pull_requests":%s}}`,
		action, id, name, ev, conclusion, attempt, id, prs)
}

func TestParseCIRun(t *testing.T) {
	api := (&fakeGitHub{}).server(t)
	prs := `[{"number":12,"head":{"ref":"bump/alert-relay-v0.2.3","sha":"h1"}}]`
	got, err := Parse(context.Background(), api, repo, "workflow_run", []byte(runJSON("ci", "completed", "pull_request", "success", 5, 1, prs)))
	if err != nil || len(got) != 1 || got[0].PR != 12 || got[0].Event.Kind != event.ChecksFinished || got[0].Event.Result != event.Success || got[0].Event.ID != "ci:5:1" {
		t.Fatalf("got %+v err %v", got, err)
	}
	for _, payload := range []string{
		runJSON("ci", "completed", "push", "success", 6, 1, `[]`),
		runJSON("ci", "completed", "pull_request", "success", 7, 1, `[{"number":9,"head":{"ref":"fix/env","sha":"h9"}}]`),
		runJSON("ci", "requested", "pull_request", "", 8, 1, prs),
	} {
		got, err := Parse(context.Background(), api, repo, "workflow_run", []byte(payload))
		if err != nil || len(got) != 0 {
			t.Fatalf("ignored payload gave %+v err %v", got, err)
		}
	}
}

const mergedPR = `[{"number":12,"title":"chore(apps): bump alert-relay to v0.2.3","html_url":"https://github.com/ambrdk/ansible/pull/12","merged_at":"2026-10-18T10:05:11Z","head":{"ref":"bump/alert-relay-v0.2.3","sha":"h1"}}]`

func report(changed int, failure string) map[string]string {
	return map[string]string{
		"extra.json":         `{"deploy_apps":["alert-relay"]}`,
		"deploy-report.json": fmt.Sprintf(`{"version":1,"started_at":"2026-10-18T10:05:20Z","finished_at":"2026-10-18T10:06:20Z","hosts":{"monitoring-01":{"ok":18,"changed":%d,"failed":0,"unreachable":0,"skipped":5},"node-05":{"ok":1,"changed":0,"failed":0,"unreachable":0,"skipped":0}},"failure":%s}`, changed, failure),
	}
}

func TestParseDeployRun(t *testing.T) {
	jobsOK := `[{"name":"ci / lint","conclusion":"success"},{"name":"plan","conclusion":"success"},{"name":"deploy / deploy","conclusion":"success"}]`
	f := &fakeGitHub{
		files:   map[string]string{"m1:apps/alert-relay/app.yml": image("v0.2.3"), "m1:apps/other/app.yml": "image: ghcr.io/ambrdk/other:v1.0.0\n"},
		commits: map[string]string{"m1": mergedPR},
		heads: map[string]string{
			"ambrdk:bump/alert-relay-v0.2.3": mergedPR,
			"ambrdk:bump/other-v1.0.0":       `[{"number":11,"title":"chore(apps): bump other to v1.0.0","html_url":"https://github.com/ambrdk/ansible/pull/11","merged_at":"2026-10-18T09:00:00Z","head":{"ref":"bump/other-v1.0.0","sha":"h0"}}]`,
		},
		jobs: map[int64]string{
			1: jobsOK, 2: jobsOK, 4: jobsOK, 5: jobsOK,
			3: `[{"name":"ci / lint","conclusion":"success"},{"name":"plan","conclusion":"success"},{"name":"deploy","conclusion":"skipped"}]`,
			6: `[{"name":"ci / lint","conclusion":"success"},{"name":"plan","conclusion":"failure"},{"name":"deploy","conclusion":"skipped"}]`,
			7: `[{"name":"ci / lint","conclusion":"success"},{"name":"plan","conclusion":"success"},{"name":"deploy / deploy","conclusion":"cancelled"}]`,
		},
		reports: map[int64]map[string]string{
			1: report(2, "null"),
			2: report(0, `{"host":"monitoring-01","task":"Wait for app health","message":"status 503"}`),
			4: {"extra.json": `{"deploy_apps":["alert-relay","other"]}`, "deploy-report.json": report(2, "null")["deploy-report.json"]},
			5: report(0, "null"),
		},
	}
	api := f.server(t)
	parse := func(payload string) []Anchored {
		t.Helper()
		got, err := Parse(context.Background(), api, repo, "workflow_run", []byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	got := parse(runJSON("deploy", "requested", "push", "", 1, 1, `[]`))
	if len(got) != 1 || got[0].PR != 12 || got[0].Event.Kind != event.DeployStarted || got[0].Event.RunURL != "https://github.com/ambrdk/ansible/actions/runs/1" {
		t.Fatalf("requested %+v", got)
	}
	if got := parse(runJSON("deploy", "requested", "workflow_dispatch", "", 1, 1, `[]`)); len(got) != 0 {
		t.Fatalf("dispatch requested %+v", got)
	}

	got = parse(runJSON("deploy", "completed", "push", "success", 1, 1, `[]`))
	if len(got) != 1 || got[0].PR != 12 {
		t.Fatalf("success %+v", got)
	}
	e := got[0].Event
	if e.Kind != event.DeployFinished || e.Result != event.Success || e.App != "alert-relay" || e.To != "v0.2.3" || len(e.Hosts) != 1 || e.Hosts[0].Name != "monitoring-01" || e.Hosts[0].Changed != 2 || e.NoChanges || e.ID != "deploy:1:1:alert-relay" {
		t.Fatalf("success event %+v", e)
	}

	e = parse(runJSON("deploy", "completed", "push", "failure", 2, 2, `[]`))[0].Event
	if e.Result != event.Failure || e.FailedTask != "Wait for app health" || e.Error != "status 503" || !strings.HasSuffix(e.ID, ":2:alert-relay") {
		t.Fatalf("failure event %+v", e)
	}

	got = parse(runJSON("deploy", "completed", "push", "success", 3, 1, `[]`))
	if len(got) != 1 || got[0].PR != 12 || got[0].Event.Result != event.Success || !got[0].Event.NoChanges {
		t.Fatalf("empty plan closes the anchored thread %+v", got)
	}
	if got := parse(runJSON("deploy", "completed", "workflow_dispatch", "success", 3, 1, `[]`)); len(got) != 0 {
		t.Fatalf("empty dispatch plan %+v", got)
	}

	got = parse(runJSON("deploy", "completed", "push", "failure", 6, 1, `[]`))
	if len(got) != 1 || got[0].PR != 12 || got[0].Event.Result != event.Failure || got[0].Event.FailedTask != "plan job" {
		t.Fatalf("plan failed %+v", got)
	}

	got = parse(runJSON("deploy", "completed", "push", "cancelled", 7, 1, `[]`))
	if len(got) != 1 || got[0].PR != 12 || got[0].Event.Result != event.Cancelled {
		t.Fatalf("cancelled %+v", got)
	}

	got = parse(runJSON("deploy", "completed", "push", "success", 4, 1, `[]`))
	if len(got) != 2 || got[0].PR != 12 || got[1].PR != 11 || got[1].Event.App != "other" || got[1].Event.To != "v1.0.0" {
		t.Fatalf("two apps %+v", got)
	}

	got = parse(runJSON("deploy", "completed", "workflow_dispatch", "success", 5, 1, `[]`))
	if len(got) != 1 || got[0].PR != 0 || !got[0].Event.NoChanges || got[0].Event.App != "alert-relay" || got[0].Event.To != "v0.2.3" {
		t.Fatalf("dispatch %+v", got)
	}
}

func TestParseDeployEdgeRuns(t *testing.T) {
	jobsOK := `[{"name":"ci / lint","conclusion":"success"},{"name":"plan","conclusion":"success"},{"name":"deploy / deploy","conclusion":"success"}]`
	envPR := `[{"number":20,"title":"fix(apps): tune alert-relay env","html_url":"https://github.com/ambrdk/ansible/pull/20","merged_at":"2026-10-18T11:00:00Z","head":{"ref":"fix/env","sha":"h20"}}]`
	f := &fakeGitHub{
		files:   map[string]string{"m1:apps/alert-relay/app.yml": image("v0.2.3"), "m2:apps/alert-relay/app.yml": image("v0.2.3")},
		commits: map[string]string{"m1": mergedPR, "m2": envPR},
		heads:   map[string]string{"ambrdk:bump/alert-relay-v0.2.3": mergedPR},
		prFiles: map[string]string{"20": `[{"filename":"apps/alert-relay/env.j2"}]`},
		jobs: map[int64]string{
			10: jobsOK, 11: jobsOK,
			12: `[{"name":"ci / lint","conclusion":"cancelled"},{"name":"plan","conclusion":"skipped"},{"name":"deploy","conclusion":"skipped"}]`,
		},
		reports: map[int64]map[string]string{
			10: report(1, "null"),
			11: {"extra.json": `{"deploy_apps":["alert-relay"]}`},
		},
	}
	api := f.server(t)
	parse := func(payload string) []Anchored {
		t.Helper()
		got, err := Parse(context.Background(), api, repo, "workflow_run", []byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	onM2 := func(s string) string { return strings.Replace(s, `"head_sha":"m1"`, `"head_sha":"m2"`, 1) }

	got := parse(onM2(runJSON("deploy", "requested", "push", "", 10, 1, `[]`)))
	if len(got) != 1 || got[0].PR != 20 {
		t.Fatalf("env PR start %+v", got)
	}
	got = parse(onM2(runJSON("deploy", "completed", "push", "success", 10, 1, `[]`)))
	if len(got) != 1 || got[0].PR != 20 || got[0].Event.App != "alert-relay" {
		t.Fatalf("env PR finish %+v", got)
	}
	if want := "2026-10-18T10:05:14Z"; got[0].Event.StartedAt.Format(time.RFC3339) != want {
		t.Fatalf("started at %v", got[0].Event.StartedAt)
	}

	got = parse(runJSON("deploy", "completed", "push", "failure", 11, 1, `[]`))
	if len(got) != 1 || got[0].PR != 12 || got[0].Event.Result != event.Failure || got[0].Event.FailedTask != "deploy job" {
		t.Fatalf("report without deploy-report.json %+v", got)
	}

	got = parse(runJSON("deploy", "completed", "push", "cancelled", 12, 1, `[]`))
	if len(got) != 1 || got[0].PR != 12 || got[0].Event.Result != event.Cancelled {
		t.Fatalf("cancelled during ci %+v", got)
	}
}

func TestReportHostsSkipIdle(t *testing.T) {
	r := Report{Hosts: map[string]ReportHost{
		"monitoring-01": {OK: 18},
		"node-05":       {OK: 1},
		"panel-01":      {OK: 1, Unreachable: 1},
		"node-07":       {OK: 1, Failed: 1},
	}}
	var names []string
	for _, h := range r.hosts() {
		names = append(names, h.Name)
	}
	if strings.Join(names, ",") != "monitoring-01,node-07,panel-01" {
		t.Fatalf("hosts %v", names)
	}
}

func TestParseReportJSON(t *testing.T) {
	var r Report
	if err := json.Unmarshal([]byte(report(1, "null")["deploy-report.json"]), &r); err != nil || r.Version != 1 || r.Hosts["monitoring-01"].Changed != 1 || r.Failure != nil {
		t.Fatalf("report %+v err %v", r, err)
	}
}
