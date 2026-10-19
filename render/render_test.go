package render

import (
	"slices"
	"testing"

	"github.com/ambrdk/cutship/release"
)

func TestBuildFinalAndLoud(t *testing.T) {
	for stage, want := range map[release.Stage][2]bool{
		release.Opened:       {false, false},
		release.ChecksFailed: {false, true},
		release.Merged:       {false, false},
		release.Deploying:    {false, false},
		release.Deployed:     {true, false},
		release.Failed:       {true, true},
		release.Cancelled:    {true, false},
		release.Closed:       {true, false},
	} {
		v := Build(release.State{Stage: stage})
		if v.Final != want[0] || v.Loud != want[1] {
			t.Errorf("%s: final %v loud %v", stage, v.Final, v.Loud)
		}
	}
}

func TestBuildLinks(t *testing.T) {
	v := Build(release.State{PRURL: "https://github.com/ambrdk/ansible/pull/6", RunURL: "RUN"})
	want := []Link{{Label: "ansible#6", URL: "https://github.com/ambrdk/ansible/pull/6"}, {Label: "run", URL: "RUN"}}
	if !slices.Equal(v.Links, want) {
		t.Fatalf("%+v", v.Links)
	}
	if got := Build(release.State{PRURL: "https://gitlab.example.com/x"}).Links; len(got) != 1 || got[0].Label != "PR" {
		t.Fatalf("%+v", got)
	}
	if got := Build(release.State{}).Links; len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestBuildKeepsState(t *testing.T) {
	s := release.State{Stage: release.Deployed, App: "alert-relay", To: "v1.0.0"}
	if v := Build(s); v.State.App != "alert-relay" || v.State.To != "v1.0.0" {
		t.Fatalf("%+v", v)
	}
}
