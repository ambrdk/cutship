package main

import (
	"strings"
	"testing"
)

func TestBumpCLI(t *testing.T) {
	cfg := writeConfig(t, ambrdkConfig)
	bump := func(stdin string, args ...string) (int, string, string) {
		return run(t, stdin, append([]string{"bump", args[0], "--config", cfg}, args[1:]...)...)
	}
	if code, out, _ := bump("", "name", "alert-relay", "v0.3.0"); code != 0 || out != "branch=bump/alert-relay-v0.3.0\ntitle=chore(apps): bump alert-relay to v0.3.0\n" {
		t.Fatalf("name: %d %q", code, out)
	}
	if code, _, errOut := bump("", "name", "Alert", "v0.3.0"); code != 1 || errOut == "" {
		t.Fatalf("bad name: %d %q", code, errOut)
	}
	if code, out, _ := bump("", "parse", "bump/alert-relay-v0.3.0"); code != 0 || out != "alert-relay v0.3.0\n" {
		t.Fatalf("parse: %d %q", code, out)
	}
	if code, out, _ := bump("", "parse", "main"); code != 0 || out != "" {
		t.Fatalf("parse other: %d %q", code, out)
	}
	if code, out, _ := bump("bump/alert-relay-v0.2.4\nbump/alert-relay-v0.3.1\n", "superseded", "alert-relay", "v0.3.0"); code != 0 || out != "bump/alert-relay-v0.2.4\n" {
		t.Fatalf("superseded: %d %q", code, out)
	}
	if code, _, _ := bump("", "superseded", "alert-relay", "latest"); code != 1 {
		t.Fatalf("superseded bad tag: %d", code)
	}
	if code, _, _ := bump("", "name", "alert-relay"); code != 2 {
		t.Fatalf("missing args: %d", code)
	}
}

func TestBumpCLIDefaultPrefix(t *testing.T) {
	cfg := writeConfig(t, goodConfig)
	if code, out, _ := run(t, "", "bump", "name", "--config", cfg, "alert-relay", "v0.3.0"); code != 0 || !strings.HasPrefix(out, "branch=cutship/alert-relay-v0.3.0\n") {
		t.Fatalf("name: %d %q", code, out)
	}
}
