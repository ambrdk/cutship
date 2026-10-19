package bump

import (
	"slices"
	"testing"

	"github.com/ambrdk/cutship/config"
)

var ambrdk = config.Release{
	TagFormat:        "v{version}",
	BumpBranchPrefix: "bump/",
	BumpTitle:        "chore(apps): bump {app} to {tag}",
}

func defaults(t *testing.T) config.Release {
	t.Helper()
	cfg, problems := config.Parse([]byte("version: 1\nforge: {type: github}\ndeploy_repo: a/b\napps:\n  - {name: x, image: x/x, pin: {preset: ansible}}\n"))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	return cfg.Release
}

var cases = []struct {
	ref, app, tag string
	ok            bool
}{
	{"bump/alert-relay-v0.3.0", "alert-relay", "v0.3.0", true},
	{"bump/a-b-c-v1.2.3", "a-b-c", "v1.2.3", true},
	{"bump/alert-relay-v0.3", "", "", false},
	{"bump/alert-relay-v0.3.0-rc.1", "", "", false},
	{"bump/Alert-v1.0.0", "", "", false},
	{"fix/alert-relay-v0.3.0", "", "", false},
	{"cutship/alert-relay-v0.3.0", "", "", false},
	{"bump/-v1.0.0", "", "", false},
	{"bump/notes", "", "", false},
}

func TestParse(t *testing.T) {
	for _, tc := range cases {
		app, tag, ok := Parse(ambrdk, tc.ref)
		if app != tc.app || tag != tc.tag || ok != tc.ok {
			t.Errorf("%s: %q %q %v", tc.ref, app, tag, ok)
		}
	}
}

func TestName(t *testing.T) {
	branch, title, err := Name(ambrdk, "alert-relay", "v0.3.0")
	if err != nil || branch != "bump/alert-relay-v0.3.0" || title != "chore(apps): bump alert-relay to v0.3.0" {
		t.Fatalf("%q %q %v", branch, title, err)
	}
}

func TestNameRejects(t *testing.T) {
	for _, in := range [][2]string{{"Alert", "v1.0.0"}, {"a b", "v1.0.0"}, {"alert-relay", "v1.2"}, {"alert-relay", "1.2.3"}, {"", "v1.0.0"}} {
		if _, _, err := Name(ambrdk, in[0], in[1]); err == nil {
			t.Errorf("%v accepted", in)
		}
	}
}

func TestBumpPrefixFromConfig(t *testing.T) {
	rel := defaults(t)
	branch, title, err := Name(rel, "alert-relay", "v0.3.0")
	if err != nil || branch != "cutship/alert-relay-v0.3.0" || title != "chore(apps): bump alert-relay to v0.3.0" {
		t.Fatalf("default: %q %q %v", branch, title, err)
	}
	if _, _, ok := Parse(rel, "bump/alert-relay-v0.3.0"); ok {
		t.Error("default prefix parsed bump/")
	}
	custom := config.Release{TagFormat: "release-{version}", BumpBranchPrefix: "deploy/bumps/", BumpTitle: "{app}: {tag}"}
	branch, title, err = Name(custom, "a-release", "release-1.2.3")
	if err != nil || branch != "deploy/bumps/a-release-release-1.2.3" || title != "a-release: release-1.2.3" {
		t.Fatalf("custom: %q %q %v", branch, title, err)
	}
	if app, tag, ok := Parse(custom, branch); !ok || app != "a-release" || tag != "release-1.2.3" {
		t.Fatalf("custom parse: %q %q %v", app, tag, ok)
	}
}

func TestRoundTrip(t *testing.T) {
	for _, rel := range []config.Release{ambrdk, {TagFormat: "{version}", BumpBranchPrefix: "cutship/", BumpTitle: "{app} {tag}"}} {
		branch, _, err := Name(rel, "a-b-c", rel.Tag("1.2.3"))
		if err != nil {
			t.Fatal(err)
		}
		if app, tag, ok := Parse(rel, branch); !ok || app != "a-b-c" || tag != rel.Tag("1.2.3") {
			t.Fatalf("%s: %q %q %v", branch, app, tag, ok)
		}
	}
}

func TestSuperseded(t *testing.T) {
	branches := []string{
		"bump/alert-relay-v0.2.3",
		"main",
		"bump/alert-relay-v0.2.11",
		"bump/alert-relay-v0.2.10",
		"bump/other-v0.1.0",
		"bump/alert-relay-v0.3.0",
		"bump/alert-relay-notes",
		"cutship/alert-relay-v0.1.0",
	}
	got := Superseded(ambrdk, "alert-relay", "v0.2.11", branches)
	if !slices.Equal(got, []string{"bump/alert-relay-v0.2.3", "bump/alert-relay-v0.2.10"}) {
		t.Fatalf("%v", got)
	}
}
