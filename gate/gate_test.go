package gate

import (
	"testing"
	"time"

	"github.com/ambrdk/cutship/config"
)

var (
	t0     = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	ambrdk = config.Release{TagFormat: "v{version}", BumpBranchPrefix: "bump/", BumpTitle: "{app} {tag}"}
	lint   = config.Lanes{RequiredCheck: "lint"}
)

func pull(ref string, merged bool) Pull {
	var p Pull
	p.Head.Ref, p.Head.SHA = ref, "h14"
	if merged {
		at := t0
		p.MergedAt = &at
	}
	return p
}

func bumpPull() Pull { return pull("bump/alert-relay-v0.2.4", true) }

func run(name, conclusion string, minute int) CheckRun {
	at := t0.Add(time.Duration(minute) * time.Minute)
	return CheckRun{Name: name, Conclusion: conclusion, CompletedAt: &at}
}

func green() []CheckRun { return []CheckRun{run("lint", "success", 0)} }

func TestSkip(t *testing.T) {
	none := []string{}
	cases := []struct {
		name    string
		pulls   []Pull
		runs    []CheckRun
		pending []string
		skip    bool
	}{
		{"bump with green lint", []Pull{bumpPull()}, green(), none, true},
		{"pending app", []Pull{bumpPull()}, green(), []string{"foo"}, false},
		{"pending unknown", []Pull{bumpPull()}, green(), nil, false},
		{"lint failed", []Pull{bumpPull()}, []CheckRun{run("lint", "failure", 0)}, none, false},
		{"latest green wins", []Pull{bumpPull()}, []CheckRun{run("lint", "failure", 0), run("lint", "success", 5)}, none, true},
		{"latest red wins", []Pull{bumpPull()}, []CheckRun{run("lint", "success", 0), run("lint", "failure", 5)}, none, false},
		{"lint running", []Pull{bumpPull()}, []CheckRun{{Name: "lint"}}, none, false},
		{"not bump", []Pull{pull("fix/env", true)}, green(), none, false},
		{"no pull", nil, green(), none, false},
		{"lint missing", []Pull{bumpPull()}, []CheckRun{run("renovate-config", "success", 0)}, none, false},
		{"unmerged", []Pull{pull("bump/alert-relay-v0.2.4", false)}, green(), none, false},
	}
	for _, tc := range cases {
		if got := Skip(ambrdk, lint, tc.pulls, tc.runs, tc.pending); got != tc.skip {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestGateRequiredCheckFromConfig(t *testing.T) {
	ci := config.Lanes{RequiredCheck: "ci / test"}
	if Skip(ambrdk, ci, []Pull{bumpPull()}, green(), []string{}) {
		t.Error("lint counted for required check ci / test")
	}
	if !Skip(ambrdk, ci, []Pull{bumpPull()}, []CheckRun{run("ci / test", "success", 0)}, []string{}) {
		t.Error("green ci / test not counted")
	}
	cutship := config.Release{TagFormat: "v{version}", BumpBranchPrefix: "cutship/", BumpTitle: "{app} {tag}"}
	if Skip(cutship, lint, []Pull{bumpPull()}, green(), []string{}) {
		t.Error("bump/ branch counted with prefix cutship/")
	}
	if Head(cutship, []Pull{pull("cutship/alert-relay-v0.2.4", true)}) != "h14" {
		t.Error("cutship/ branch not found")
	}
}

func TestSkipNotBump(t *testing.T) {
	if Skip(ambrdk, lint, []Pull{pull("bump/notes", true)}, green(), []string{}) {
		t.Fatal("bump/notes treated as a bump")
	}
	if Head(ambrdk, []Pull{pull("bump/notes", true)}) != "" {
		t.Fatal("head of bump/notes")
	}
}

func TestHead(t *testing.T) {
	if got := Head(ambrdk, []Pull{pull("fix/env", true), bumpPull()}); got != "h14" {
		t.Fatalf("got %q", got)
	}
	if got := Head(ambrdk, []Pull{pull("fix/env", true)}); got != "" {
		t.Fatalf("got %q", got)
	}
}
