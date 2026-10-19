package render

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ambrdk/ship/event"
	"github.com/ambrdk/ship/release"
)

var t0 = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

func example() release.State {
	return release.State{
		Stage: release.Deployed, App: "alert-relay", From: "v0.2.0", To: "v0.2.2",
		PR: 6, PRURL: "https://github.com/ambrdk/ansible/pull/6", RunURL: "RUN",
		Hosts:     []event.Host{{Name: "monitoring-01", OK: 18, Changed: 2}},
		StartedAt: t0, FinishedAt: t0.Add(72 * time.Second),
	}
}

func TestPostExample(t *testing.T) {
	want := "✅ <b>Deployed: alert-relay v0.2.2</b> (took 1m12s)\n\nVersion: v0.2.0 → <b>v0.2.2</b>\nHosts: monitoring-01\n" +
		`<a href="https://github.com/ambrdk/ansible/pull/6">ansible#6</a> · <a href="RUN">run</a>`
	if got := Post(example()); got != want {
		t.Fatalf("post\n%q\nwant\n%q", got, want)
	}
}

func TestPostStages(t *testing.T) {
	cases := map[release.Stage]string{
		release.Opened:       "📦 <b>Release: alert-relay v0.2.2</b> (waiting for merge)",
		release.ChecksFailed: "📦 <b>Release: alert-relay v0.2.2</b> (checks failed)",
		release.Merged:       "🔀 <b>Merged: alert-relay v0.2.2</b> (waiting for deploy)",
		release.Deploying:    "🚀 <b>Deploy: alert-relay</b>",
		release.Deployed:     "✅ <b>Deployed: alert-relay v0.2.2</b> (took 1m12s)",
		release.Failed:       "❌ <b>Deploy failed: alert-relay v0.2.2</b> (after 1m12s)",
		release.Cancelled:    "⚪ <b>Deploy cancelled: alert-relay v0.2.2</b>",
		release.Closed:       "⚪ <b>Closed: alert-relay v0.2.2</b> (not merged)",
	}
	for stage, want := range cases {
		s := example()
		s.Stage = stage
		if got := strings.SplitN(Post(s), "\n", 2)[0]; got != want {
			t.Errorf("%s: %q want %q", stage, got, want)
		}
	}
	s := example()
	s.NoChanges = true
	if got := strings.SplitN(Post(s), "\n", 2)[0]; got != "✅ <b>Deployed: alert-relay v0.2.2</b> (no changes)" {
		t.Errorf("no changes: %q", got)
	}
	s = example()
	s.Stage = release.Failed
	s.FailedTask = "Wait for app health"
	if !strings.Contains(Post(s), "\nFailed at: Wait for app health\n") {
		t.Errorf("failed post %q", Post(s))
	}
}

func TestPostWithoutVersions(t *testing.T) {
	s := release.State{Stage: release.Deploying, Title: "fix(apps): tune alert-relay env", PR: 9, PRURL: "https://github.com/ambrdk/ansible/pull/9"}
	want := "🚀 <b>Deploy: fix(apps): tune alert-relay env</b>\n\nChange: fix(apps): tune alert-relay env\n" + `<a href="https://github.com/ambrdk/ansible/pull/9">ansible#9</a>`
	if got := Post(s); got != want {
		t.Fatalf("post\n%q\nwant\n%q", got, want)
	}
}

func TestPostFallbackName(t *testing.T) {
	s := release.State{Stage: release.Failed, StartedAt: t0, FinishedAt: t0.Add(time.Minute), FailedTask: "plan job", RunURL: "RUN"}
	want := "❌ <b>Deploy failed: deploy run</b> (after 1m0s)"
	if got := strings.SplitN(Post(s), "\n", 2)[0]; got != want {
		t.Fatalf("%q want %q", got, want)
	}
}

func TestCommentExample(t *testing.T) {
	s := release.State{
		App: "alert-relay", Image: "ghcr.io/ambrdk/alert-relay", From: "v0.2.2", To: "v0.2.3",
		Title: "chore(apps): bump alert-relay to v0.2.3", PR: 12, MergedBy: "ambrdk", Commit: "1a2b3c4d5e",
		Timeline: []release.Line{
			{At: t0.Add(62 * time.Second), Tag: "PR", Text: "Bump opened: v0.2.2 → v0.2.3"},
			{At: t0.Add(100 * time.Second), Tag: "CI", Text: "Checks passed"},
			{At: t0.Add(6*time.Minute + 22*time.Second), Tag: "DONE", Text: "Deployed, took 1m08s"},
			{At: t0.Add(7 * time.Minute), Tag: "REOPENED"},
		},
	}
	want := "📋 Deploy Details\nApp: alert-relay\nImage: ghcr.io/ambrdk/alert-relay\n       v0.2.2 → v0.2.3\n" +
		"Change: chore(apps): bump alert-relay to v0.2.3 (#12)\nMerged by: ambrdk · commit 1a2b3c4\n\n📋 Timeline\n" +
		"[10:01:02 UTC] [PR] Bump opened: v0.2.2 → v0.2.3\n[10:01:40 UTC] [CI] Checks passed\n[10:06:22 UTC] [DONE] Deployed, took 1m08s\n[10:07:00 UTC] [REOPENED]"
	if got := Comment(s, 0); got != want {
		t.Fatalf("comment\n%q\nwant\n%q", got, want)
	}
}

func TestRenderEscapes(t *testing.T) {
	s := example()
	s.Stage = release.Failed
	s.FailedTask = "<script>&"
	if !strings.Contains(Post(s), "Failed at: &lt;script&gt;&amp;") {
		t.Fatalf("not escaped: %q", Post(s))
	}
}

func TestCommentSkip(t *testing.T) {
	s := example()
	s.Timeline = nil
	for i := range 10 {
		s.Timeline = append(s.Timeline, release.Line{At: t0.Add(time.Duration(i) * time.Second), Tag: "HOST", Text: fmt.Sprintf("line%02d", i)})
	}
	c := Comment(s, 3)
	for _, want := range []string{"line00", "line04", "line09"} {
		if !strings.Contains(c, want) {
			t.Fatalf("%s missing in %q", want, c)
		}
	}
	for _, gone := range []string{"line01", "line03"} {
		if strings.Contains(c, gone) {
			t.Fatalf("%s kept in %q", gone, c)
		}
	}
	if Comment(s, 50) != Comment(s, 51) || !strings.Contains(Comment(s, 50), "line00") || strings.Contains(Comment(s, 50), "line09") {
		t.Fatalf("over-skip %q", Comment(s, 50))
	}
}
