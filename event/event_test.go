package event

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestEventJSONRoundTrip(t *testing.T) {
	in := Event{
		Kind: DeployFinished, ID: "deploy:1:2:alert-relay", At: time.Date(2026, 10, 18, 10, 6, 22, 0, time.UTC),
		App: "alert-relay", From: "v0.2.2", To: "v0.2.3", Image: "ghcr.io/ambrdk/alert-relay", Title: "chore(apps): bump alert-relay to v0.2.3",
		PR: 12, Actor: "ambrdk", Commit: "1a2b3c4d", RunURL: "https://github.com/ambrdk/ansible/actions/runs/1", PRURL: "https://github.com/ambrdk/ansible/pull/12",
		Attempt: 2, Hosts: []Host{{Name: "monitoring-01", OK: 18, Changed: 2, Failed: 1, Unreachable: 1}},
		Result: Failure, FailedTask: "Wait for app health", Error: "status 503", NoChanges: true,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Event
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip\n in  %+v\n out %+v", in, out)
	}
	if string(raw[:9]) != `{"kind":"` {
		t.Fatalf("json %s", raw)
	}
}
