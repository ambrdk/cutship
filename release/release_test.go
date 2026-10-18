package release

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ambrdk/deploy-notify/event"
)

var t0 = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

func ev(kind event.Kind, id string, minute int, mods ...func(*event.Event)) event.Event {
	e := event.Event{Kind: kind, ID: id, At: t0.Add(time.Duration(minute) * time.Minute), App: "alert-relay", From: "v0.2.2", To: "v0.2.3", PR: 12}
	for _, m := range mods {
		m(&e)
	}
	return e
}

func result(r event.Result) func(*event.Event) { return func(e *event.Event) { e.Result = r } }

func hosts(changed int) func(*event.Event) {
	return func(e *event.Event) { e.Hosts = []event.Host{{Name: "monitoring-01", OK: 18, Changed: changed}} }
}

func failed(e *event.Event) {
	e.Result = event.Failure
	e.FailedTask = "Wait for app health"
	e.Hosts = []event.Host{{Name: "monitoring-01", OK: 17, Failed: 1}}
}

func attempt(n int) func(*event.Event) { return func(e *event.Event) { e.Attempt = n } }

func tags(s State) []string {
	out := make([]string, 0, len(s.Timeline))
	for _, l := range s.Timeline {
		out = append(out, l.Tag)
	}
	return out
}

func TestApply(t *testing.T) {
	cases := []struct {
		name      string
		events    []event.Event
		wantStage Stage
		wantTags  []string
	}{
		{"happy path", []event.Event{
			ev(event.ReleaseOpened, "pr:opened", 0),
			ev(event.ChecksFinished, "ci:1:1", 1, result(event.Success)),
			ev(event.Merged, "pr:closed", 5),
			ev(event.DeployStarted, "deploy:2:1", 6),
			ev(event.DeployFinished, "deploy:2:1:alert-relay", 7, result(event.Success), hosts(2)),
		}, Deployed, []string{"PR", "CI", "MERGED", "DEPLOY", "HOST", "DONE"}},
		{"started before merged", []event.Event{
			ev(event.ReleaseOpened, "pr:opened", 0),
			ev(event.DeployStarted, "deploy:2:1", 6),
			ev(event.Merged, "pr:closed", 5),
		}, Deploying, []string{"PR", "MERGED", "DEPLOY"}},
		{"failure then retry", []event.Event{
			ev(event.ReleaseOpened, "pr:opened", 0),
			ev(event.Merged, "pr:closed", 5),
			ev(event.DeployFinished, "deploy:2:1:alert-relay", 7, failed),
			ev(event.DeployFinished, "deploy:2:2:alert-relay", 9, result(event.Success), hosts(2), attempt(2)),
		}, Deployed, []string{"PR", "MERGED", "HOST", "FAIL", "FAILED", "RETRY", "HOST", "DONE"}},
		{"closed after merged", []event.Event{
			ev(event.ReleaseOpened, "pr:opened", 0),
			ev(event.Merged, "pr:closed", 5),
			ev(event.ClosedUnmerged, "pr:closed:2", 6),
		}, Merged, []string{"PR", "MERGED"}},
		{"closed then reopened", []event.Event{
			ev(event.ReleaseOpened, "pr:opened", 0),
			ev(event.ClosedUnmerged, "pr:closed", 3),
			ev(event.Reopened, "pr:reopened", 4),
		}, Opened, []string{"PR", "CLOSED", "REOPENED"}},
		{"checks failed", []event.Event{
			ev(event.ReleaseOpened, "pr:opened", 0),
			ev(event.ChecksFinished, "ci:1:1", 1, result(event.Failure)),
		}, ChecksFailed, []string{"PR", "CI"}},
		{"cancelled then deployed by next run", []event.Event{
			ev(event.Merged, "pr:closed", 5),
			ev(event.DeployStarted, "deploy:2:1", 6),
			ev(event.DeployFinished, "deploy:2:1:alert-relay", 7, result(event.Cancelled)),
			ev(event.DeployFinished, "deploy:3:1:alert-relay", 9, result(event.Success), hosts(2)),
		}, Deployed, []string{"MERGED", "DEPLOY", "CANCELLED", "RETRY", "HOST", "DONE"}},
		{"cancelled stays", []event.Event{
			ev(event.DeployStarted, "deploy:2:1", 6),
			ev(event.DeployFinished, "deploy:2:1:alert-relay", 7, result(event.Cancelled)),
		}, Cancelled, []string{"DEPLOY", "CANCELLED"}},
		{"old failure after success", []event.Event{
			ev(event.DeployFinished, "deploy:3:1:alert-relay", 9, result(event.Success), hosts(2)),
			ev(event.DeployFinished, "deploy:2:1:alert-relay", 7, failed),
		}, Deployed, []string{"HOST", "FAIL", "FAILED", "HOST", "DONE"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s State
			for _, e := range tc.events {
				s, _ = Apply(s, e)
			}
			if s.Stage != tc.wantStage {
				t.Fatalf("stage %s want %s", s.Stage, tc.wantStage)
			}
			if got := tags(s); !slices.Equal(got, tc.wantTags) {
				t.Fatalf("tags %v want %v", got, tc.wantTags)
			}
		})
	}
}

func TestApplyDuplicateIsNoop(t *testing.T) {
	s, changed := Apply(State{}, ev(event.ReleaseOpened, "pr:opened", 0))
	if !changed {
		t.Fatal("first apply did not change")
	}
	again, changed := Apply(s, ev(event.ReleaseOpened, "pr:opened", 0))
	if changed || len(again.Timeline) != 1 {
		t.Fatalf("duplicate changed=%v timeline %d", changed, len(again.Timeline))
	}
}

func TestApplyFields(t *testing.T) {
	var s State
	s, _ = Apply(s, ev(event.ReleaseOpened, "pr:opened", 0, func(e *event.Event) {
		e.Image = "ghcr.io/ambrdk/alert-relay"
		e.Title = "chore(apps): bump alert-relay to v0.2.3"
		e.PRURL = "https://github.com/ambrdk/ansible/pull/12"
	}))
	s, _ = Apply(s, ev(event.Merged, "pr:closed", 5, func(e *event.Event) { e.Actor = "ambrdk"; e.Commit = "1a2b3c4d" }))
	s, _ = Apply(s, ev(event.DeployStarted, "deploy:2:1", 6, func(e *event.Event) { e.RunURL = "RUN" }))
	s, _ = Apply(s, ev(event.DeployFinished, "deploy:2:1:alert-relay", 7, result(event.Success), hosts(0), func(e *event.Event) { e.NoChanges = true }))
	if s.App != "alert-relay" || s.From != "v0.2.2" || s.To != "v0.2.3" || s.Image != "ghcr.io/ambrdk/alert-relay" || s.PR != 12 || s.MergedBy != "ambrdk" || s.Commit != "1a2b3c4d" || s.RunURL != "RUN" || !s.NoChanges {
		t.Fatalf("state %+v", s)
	}
	if s.FinishedAt.Sub(s.StartedAt) != time.Minute || len(s.Hosts) != 1 {
		t.Fatalf("times %v %v hosts %+v", s.StartedAt, s.FinishedAt, s.Hosts)
	}
	want := []string{"Bump opened: v0.2.2 → v0.2.3", "by ambrdk", "Started", "monitoring-01: ok=18 changed=0", "Deployed, took 1m0s"}
	for i, l := range s.Timeline {
		if l.Text != want[i] {
			t.Fatalf("line %d %q want %q", i, l.Text, want[i])
		}
	}
}

func TestApplyFailureLines(t *testing.T) {
	var s State
	s, _ = Apply(s, ev(event.DeployStarted, "deploy:2:1", 6))
	s, _ = Apply(s, ev(event.DeployFinished, "deploy:2:1:alert-relay", 7, failed, func(e *event.Event) { e.Hosts[0].Unreachable = 1 }))
	texts := []string{}
	for _, l := range s.Timeline {
		texts = append(texts, l.Tag+" "+l.Text)
	}
	want := []string{"DEPLOY Started", "HOST monitoring-01: ok=17 changed=0 failed=1 unreachable=1", "FAIL monitoring-01: Wait for app health", "FAILED lasted 1m0s"}
	if !slices.Equal(texts, want) || s.FailedTask != "Wait for app health" {
		t.Fatalf("lines %q task %q", texts, s.FailedTask)
	}
}

func TestApplyTrimsTimeline(t *testing.T) {
	s, _ := Apply(State{}, ev(event.ReleaseOpened, "pr:opened", 0))
	for i := 1; i < 40; i++ {
		s, _ = Apply(s, ev(event.ChecksFinished, fmt.Sprintf("ci:%d:1", i), i, result(event.Success)))
	}
	if len(s.Timeline) != 31 || s.Timeline[0].Tag != "PR" || !s.Timeline[30].At.Equal(t0.Add(39*time.Minute)) {
		t.Fatalf("timeline %d first %s", len(s.Timeline), s.Timeline[0].Tag)
	}
}
