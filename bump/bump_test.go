package bump

import (
	"slices"
	"testing"
)

var cases = []struct {
	ref, app, tag string
	ok            bool
}{
	{"bump/alert-relay-v0.3.0", "alert-relay", "v0.3.0", true},
	{"bump/a-b-c-v1.2.3", "a-b-c", "v1.2.3", true},
	{"bump/alert-relay-v0.3", "", "", false},
	{"bump/Alert-v1.0.0", "", "", false},
	{"fix/alert-relay-v0.3.0", "", "", false},
	{"bump/-v1.0.0", "", "", false},
	{"bump/notes", "", "", false},
}

func TestParse(t *testing.T) {
	for _, tc := range cases {
		app, tag, ok := Parse(tc.ref)
		if app != tc.app || tag != tc.tag || ok != tc.ok {
			t.Errorf("%s: %q %q %v", tc.ref, app, tag, ok)
		}
	}
}

func TestName(t *testing.T) {
	branch, title, err := Name("alert-relay", "v0.3.0")
	if err != nil || branch != "bump/alert-relay-v0.3.0" || title != "chore(apps): bump alert-relay to v0.3.0" {
		t.Fatalf("%q %q %v", branch, title, err)
	}
}

func TestNameRejects(t *testing.T) {
	for _, in := range [][2]string{{"Alert", "v1.0.0"}, {"a b", "v1.0.0"}, {"alert-relay", "v1.2"}, {"", "v1.0.0"}} {
		if _, _, err := Name(in[0], in[1]); err == nil {
			t.Errorf("%v accepted", in)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	branch, _, _ := Name("a-b-c", "v1.2.3")
	if app, tag, ok := Parse(branch); !ok || app != "a-b-c" || tag != "v1.2.3" {
		t.Fatalf("%q %q %v", app, tag, ok)
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
	}
	got := Superseded("alert-relay", "v0.2.11", branches)
	if !slices.Equal(got, []string{"bump/alert-relay-v0.2.3", "bump/alert-relay-v0.2.10"}) {
		t.Fatalf("%v", got)
	}
}
