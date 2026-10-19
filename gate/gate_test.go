package gate

import (
	"testing"
	"time"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/forge"
)

var (
	t0     = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	ambrdk = config.Release{TagFormat: "v{version}", BumpBranchPrefix: "bump/", BumpTitle: "{app} {tag}"}
	lint   = config.Lanes{RequiredCheck: "lint"}
)

func pull(ref string, merged bool) forge.Change {
	c := forge.Change{HeadRef: ref, HeadSHA: "h14", State: forge.Open}
	if merged {
		at := t0
		c.MergedAt, c.State = &at, forge.Merged
	}
	return c
}

func bumpPull() forge.Change { return pull("bump/alert-relay-v0.2.4", true) }

func run(name string, state forge.CheckState, minute int) forge.Check {
	c := forge.Check{Name: name, State: state}
	if state != forge.Pending {
		c.CompletedAt = t0.Add(time.Duration(minute) * time.Minute)
	}
	return c
}

func green() []forge.Check { return []forge.Check{run("lint", forge.Success, 0)} }

func TestSkip(t *testing.T) {
	none := []string{}
	cases := []struct {
		name    string
		pulls   []forge.Change
		runs    []forge.Check
		pending []string
		skip    bool
	}{
		{"bump with green lint", []forge.Change{bumpPull()}, green(), none, true},
		{"pending app", []forge.Change{bumpPull()}, green(), []string{"foo"}, false},
		{"pending unknown", []forge.Change{bumpPull()}, green(), nil, false},
		{"lint failed", []forge.Change{bumpPull()}, []forge.Check{run("lint", forge.Failure, 0)}, none, false},
		{"latest green wins", []forge.Change{bumpPull()}, []forge.Check{run("lint", forge.Failure, 0), run("lint", forge.Success, 5)}, none, true},
		{"latest red wins", []forge.Change{bumpPull()}, []forge.Check{run("lint", forge.Success, 0), run("lint", forge.Failure, 5)}, none, false},
		{"lint running", []forge.Change{bumpPull()}, []forge.Check{run("lint", forge.Pending, 0)}, none, false},
		{"lint rerun running", []forge.Change{bumpPull()}, []forge.Check{run("lint", forge.Success, 0), run("lint", forge.Pending, 0)}, none, false},
		{"not bump", []forge.Change{pull("fix/env", true)}, green(), none, false},
		{"no pull", nil, green(), none, false},
		{"lint missing", []forge.Change{bumpPull()}, []forge.Check{run("renovate-config", forge.Success, 0)}, none, false},
		{"unmerged", []forge.Change{pull("bump/alert-relay-v0.2.4", false)}, green(), none, false},
	}
	for _, tc := range cases {
		if got := Skip(ambrdk, lint, tc.pulls, tc.runs, tc.pending); got != tc.skip {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestGateRequiredCheckFromConfig(t *testing.T) {
	ci := config.Lanes{RequiredCheck: "ci / test"}
	if Skip(ambrdk, ci, []forge.Change{bumpPull()}, green(), []string{}) {
		t.Error("lint counted for required check ci / test")
	}
	if !Skip(ambrdk, ci, []forge.Change{bumpPull()}, []forge.Check{run("ci / test", forge.Success, 0)}, []string{}) {
		t.Error("green ci / test not counted")
	}
	cutship := config.Release{TagFormat: "v{version}", BumpBranchPrefix: "cutship/", BumpTitle: "{app} {tag}"}
	if Skip(cutship, lint, []forge.Change{bumpPull()}, green(), []string{}) {
		t.Error("bump/ branch counted with prefix cutship/")
	}
	if got, _ := Bump(cutship, []forge.Change{pull("cutship/alert-relay-v0.2.4", true)}); got.HeadSHA != "h14" {
		t.Error("cutship/ branch not found")
	}
}

func TestSkipNotBump(t *testing.T) {
	if Skip(ambrdk, lint, []forge.Change{pull("bump/notes", true)}, green(), []string{}) {
		t.Fatal("bump/notes treated as a bump")
	}
	if _, ok := Bump(ambrdk, []forge.Change{pull("bump/notes", true)}); ok {
		t.Fatal("bump/notes found")
	}
}

func TestBump(t *testing.T) {
	if got, ok := Bump(ambrdk, []forge.Change{pull("fix/env", true), bumpPull()}); !ok || got.HeadSHA != "h14" {
		t.Fatalf("got %+v", got)
	}
	if _, ok := Bump(ambrdk, []forge.Change{pull("fix/env", true)}); ok {
		t.Fatal("found")
	}
}
