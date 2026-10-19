package main

import "testing"

func TestBumpCLI(t *testing.T) {
	if code, out, _ := run(t, "", "bump", "name", "alert-relay", "v0.3.0"); code != 0 || out != "branch=bump/alert-relay-v0.3.0\ntitle=chore(apps): bump alert-relay to v0.3.0\n" {
		t.Fatalf("name: %d %q", code, out)
	}
	if code, _, errOut := run(t, "", "bump", "name", "Alert", "v0.3.0"); code != 1 || errOut == "" {
		t.Fatalf("bad name: %d %q", code, errOut)
	}
	if code, out, _ := run(t, "", "bump", "parse", "bump/alert-relay-v0.3.0"); code != 0 || out != "alert-relay v0.3.0\n" {
		t.Fatalf("parse: %d %q", code, out)
	}
	if code, out, _ := run(t, "", "bump", "parse", "main"); code != 0 || out != "" {
		t.Fatalf("parse other: %d %q", code, out)
	}
	if code, out, _ := run(t, "bump/alert-relay-v0.2.4\nbump/alert-relay-v0.3.1\n", "bump", "superseded", "alert-relay", "v0.3.0"); code != 0 || out != "bump/alert-relay-v0.2.4\n" {
		t.Fatalf("superseded: %d %q", code, out)
	}
	if code, _, _ := run(t, "", "bump", "superseded", "alert-relay", "latest"); code != 1 {
		t.Fatalf("superseded bad tag: %d", code)
	}
}
