package main

import (
	"os"
	"path/filepath"
	"testing"
)

const bumpPulls = `[{"number":14,"merged_at":"2026-10-18T10:00:00Z","head":{"ref":"bump/alert-relay-v0.2.4","sha":"h14"}}]`

func file(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGateCLI(t *testing.T) {
	pulls := file(t, bumpPulls)
	pages := file(t, `[{"total_count":2,"check_runs":[{"name":"fast","conclusion":"success","completed_at":"2026-10-18T10:00:00Z"}]},{"total_count":2,"check_runs":[{"name":"lint","conclusion":"success","completed_at":"2026-10-18T10:00:00Z"}]}]`)
	empty := file(t, `[{"check_runs":[]}]`)
	cases := []struct {
		args []string
		out  string
	}{
		{[]string{"gate", "head", "--pulls", pulls}, "h14\n"},
		{[]string{"gate", "head", "--pulls", file(t, "[]")}, "\n"},
		{[]string{"gate", "skip", "--pulls", pulls, "--checks", pages, "--pending", "[]"}, "true\n"},
		{[]string{"gate", "skip", "--pulls", pulls, "--checks", pages, "--pending", `["foo"]`}, "false\n"},
		{[]string{"gate", "skip", "--pulls", pulls, "--checks", pages, "--pending", "unknown"}, "false\n"},
		{[]string{"gate", "skip", "--pulls", pulls, "--checks", empty, "--pending", "[]"}, "false\n"},
	}
	for _, tc := range cases {
		if code, out, errOut := run(t, "", tc.args...); code != 0 || out != tc.out {
			t.Errorf("%v: %d %q %q", tc.args, code, out, errOut)
		}
	}
	if code, _, _ := run(t, "", "gate", "skip", "--pulls", pulls); code != 2 {
		t.Errorf("missing flags: %d", code)
	}
	if code, _, _ := run(t, "", "gate", "head", "--pulls", file(t, "{")); code != 1 {
		t.Errorf("bad json: %d", code)
	}
}
