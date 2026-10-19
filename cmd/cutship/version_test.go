package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/ambrdk/cutship/forge/fake"
)

func gitRepo(t *testing.T, steps ...[]string) string {
	t.Helper()
	dir := t.TempDir()
	all := append([][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
		{"config", "tag.gpgsign", "false"},
	}, steps...)
	for _, args := range all {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestVersionCLIFromDeployRepo(t *testing.T) {
	f := fake.New("bot")
	f.Commit("ambrdk/ansible", "main", map[string][]byte{"cutship.yaml": []byte(strings.Replace(ambrdkConfig, "release:\n", "release:\n  tag_format: \"{version}\"\n", 1))})
	useForge(t, f)
	t.Setenv("CUTSHIP_DEPLOY_TOKEN", "deploy-token")
	dir := gitRepo(t,
		[]string{"commit", "-q", "--allow-empty", "-m", "feat: a"},
		[]string{"tag", "1.0.0"},
		[]string{"commit", "-q", "--allow-empty", "-m", "fix: b"},
	)
	code, out, errOut := run(t, "", "version", "next", "--deploy-repo", "ambrdk/ansible", "--app", "alert-relay", "--dir", dir)
	if code != 0 || out != "1.0.1\n" {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	if code, _, _ := run(t, "", "version", "next", "--deploy-repo", "ambrdk/ansible", "--config", "x.yaml", "--app", "alert-relay"); code != 2 {
		t.Fatalf("both sources: %d", code)
	}
}

func TestVersionCLI(t *testing.T) {
	cfg := writeConfig(t, goodConfig)
	dir := gitRepo(t,
		[]string{"commit", "-q", "--allow-empty", "-m", "feat: a"},
		[]string{"tag", "v0.1.0"},
		[]string{"commit", "-q", "--allow-empty", "-m", "Merge branch x"},
		[]string{"commit", "-q", "--allow-empty", "-m", "fix: b"},
	)
	code, out, errOut := run(t, "", "version", "next", "--config", cfg, "--app", "alert-relay", "--dir", dir)
	if code != 0 || out != "v0.1.1\n" {
		t.Fatalf("next: %d %q %q", code, out, errOut)
	}
	if errOut != "::warning::not a conventional commit subject, no release from it: Merge branch x\nlast release v0.1.0, next v0.1.1\n" {
		t.Errorf("stderr %q", errOut)
	}

	quiet := gitRepo(t,
		[]string{"commit", "-q", "--allow-empty", "-m", "feat: a"},
		[]string{"tag", "v0.1.0"},
		[]string{"commit", "-q", "--allow-empty", "-m", "docs: b"},
	)
	if code, out, errOut := run(t, "", "version", "next", "--config", cfg, "--app", "alert-relay", "--dir", quiet); code != 0 || out != "" || errOut != "last release v0.1.0, next none\n" {
		t.Errorf("no release: %d %q %q", code, out, errOut)
	}

	if code, _, errOut := run(t, "", "version", "next", "--config", cfg, "--app", "api", "--dir", dir); code != 1 || !strings.Contains(errOut, `::error::app "api" is not in `) {
		t.Errorf("unknown app: %d %q", code, errOut)
	}
	bad := writeConfig(t, "version: 2\n")
	if code, _, errOut := run(t, "", "version", "next", "--config", bad, "--app", "alert-relay", "--dir", dir); code != 1 || !strings.Contains(errOut, "::error file="+bad) {
		t.Errorf("bad config: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, "", "version", "next", "--config", cfg, "--app", "alert-relay", "--dir", t.TempDir()); code != 1 || !strings.HasPrefix(errOut, "::error::") {
		t.Errorf("not a repo: %d %q", code, errOut)
	}
	for _, args := range [][]string{
		{"version"},
		{"version", "next", "--config", cfg},
		{"version", "next", "--config", cfg, "--app", "alert-relay", "extra"},
		{"version", "bogus", "--app", "alert-relay"},
	} {
		if code, _, _ := run(t, "", args...); code != 2 {
			t.Errorf("%v: code %d", args, code)
		}
	}
}
