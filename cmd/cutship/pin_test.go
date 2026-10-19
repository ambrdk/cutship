package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const own = "ghcr.io/ambrdk/alert-relay"

const ambrdkConfig = `version: 1
forge: { type: github }
deploy_repo: ambrdk/ansible
apps:
  - name: alert-relay
    image: ghcr.io/ambrdk/alert-relay
    pin: { preset: ansible }
release:
  bump_branch_prefix: bump/
`

func deployRepo(t *testing.T, apps map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, ref := range apps {
		dir := filepath.Join(root, "apps", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "app.yml"), []byte("---\nimage: "+ref+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func run(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := dispatch(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestPinCLI(t *testing.T) {
	cfg := writeConfig(t, ambrdkConfig)
	root := deployRepo(t, map[string]string{"alert-relay": own + ":v0.2.3"})
	pin := func(args ...string) (int, string, string) {
		return run(t, "", append([]string{"pin", args[0], "--config", cfg, "--root", root}, args[1:]...)...)
	}
	if code, out, _ := pin("read", "alert-relay"); code != 0 || out != own+":v0.2.3\n" {
		t.Fatalf("read: %d %q", code, out)
	}
	if code, out, _ := pin("check", "alert-relay", "v0.2.4"); code != 0 || out != "bump\n" {
		t.Fatalf("check: %d %q", code, out)
	}
	if code, out, _ := pin("set", "alert-relay", "v0.2.4"); code != 0 || out != "" {
		t.Fatalf("set: %d %q", code, out)
	}
	if code, out, _ := pin("check", "alert-relay", "v0.2.4"); code != 0 || out != "same\n" {
		t.Fatalf("check after set: %d %q", code, out)
	}
	if code, out, errOut := pin("verify"); code != 0 || out != "" || errOut != "" {
		t.Fatalf("verify: %d %q %q", code, out, errOut)
	}
}

func TestPinErrorExit(t *testing.T) {
	cfg := writeConfig(t, ambrdkConfig)
	root := deployRepo(t, map[string]string{"alert-relay": own + ":v0.2.5", "loose": "valkey/valkey:latest"})
	code, _, errOut := run(t, "", "pin", "check", "--config", cfg, "--root", root, "alert-relay", "v0.2.4")
	if code != 1 || !strings.HasPrefix(errOut, "::error::") || !strings.Contains(errOut, "lower than") {
		t.Fatalf("check: %d %q", code, errOut)
	}
	code, _, errOut = run(t, "", "pin", "verify", "--config", cfg, "--root", root)
	if code != 1 || !strings.Contains(errOut, "::error::loose") {
		t.Fatalf("verify: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, "", "pin", "check", "--config", cfg, "--root", root, "api", "v0.2.4"); code != 1 || !strings.Contains(errOut, `app "api" is not in`) {
		t.Fatalf("unknown app: %d %q", code, errOut)
	}
	if code, _, _ := run(t, "", "pin", "check", "--config", cfg, "--root", root, "alert-relay"); code != 2 {
		t.Fatalf("missing args: %d", code)
	}
	if code, _, _ := run(t, "", "pin", "check", "--config", cfg, "--root", root, "alert-relay", own, "v0.2.4"); code != 2 {
		t.Fatalf("old image argument: %d", code)
	}
	if code, _, errOut := run(t, "", "pin", "verify", "--config", filepath.Join(root, "missing.yaml"), "--root", root); code != 1 || !strings.Contains(errOut, "::error file=") {
		t.Fatalf("missing config: %d %q", code, errOut)
	}
}

func TestLaneCLI(t *testing.T) {
	cfg := writeConfig(t, ambrdkConfig)
	fast := `[{"filename":"apps/alert-relay/app.yml","status":"modified","patch":"@@ -1 +1 @@\n-image: ` + own + `:v0.2.3\n+image: ` + own + `:v0.2.4"}]`
	if code, out, _ := run(t, fast, "lane", "--config", cfg); code != 0 || out != "fast\n" {
		t.Fatalf("fast: %d %q", code, out)
	}
	if code, out, _ := run(t, "[]", "lane", "--config", cfg); code != 0 || out != "full\n" {
		t.Fatalf("full: %d %q", code, out)
	}
	if code, _, errOut := run(t, "{", "lane", "--config", cfg); code != 1 || !strings.HasPrefix(errOut, "::error::") {
		t.Fatalf("bad json: %d %q", code, errOut)
	}
	if code, _, _ := run(t, "[]", "lane", "--config", cfg, "extra"); code != 2 {
		t.Fatalf("extra arg: %d", code)
	}
}

func TestPlainTagFormatEndToEnd(t *testing.T) {
	cfg := writeConfig(t, strings.Replace(ambrdkConfig, "release:\n", "release:\n  tag_format: \"{version}\"\n", 1))
	service := gitRepo(t,
		[]string{"commit", "-q", "--allow-empty", "-m", "feat: a"},
		[]string{"tag", "1.2.3"},
		[]string{"commit", "-q", "--allow-empty", "-m", "fix: b"},
	)
	if code, out, errOut := run(t, "", "version", "next", "--config", cfg, "--app", "alert-relay", "--dir", service); code != 0 || out != "1.2.4\n" {
		t.Fatalf("version next: %d %q %q", code, out, errOut)
	}
	root := deployRepo(t, map[string]string{"alert-relay": own + ":1.2.3"})
	if code, out, errOut := run(t, "", "pin", "check", "--config", cfg, "--root", root, "alert-relay", "1.2.4"); code != 0 || out != "bump\n" {
		t.Fatalf("pin check 1.2.4: %d %q %q", code, out, errOut)
	}
	if code, _, errOut := run(t, "", "pin", "check", "--config", cfg, "--root", root, "alert-relay", "v1.2.4"); code != 1 || !strings.Contains(errOut, "tag format") {
		t.Fatalf("pin check v1.2.4: %d %q", code, errOut)
	}
	fast := `[{"filename":"apps/alert-relay/app.yml","status":"modified","patch":"@@ -1 +1 @@\n-image: ` + own + `:1.2.3\n+image: ` + own + `:1.2.4"}]`
	if code, out, _ := run(t, fast, "lane", "--config", cfg); code != 0 || out != "fast\n" {
		t.Fatalf("lane: %d %q", code, out)
	}
	if code, out, _ := run(t, "", "bump", "name", "--config", cfg, "alert-relay", "1.2.4"); code != 0 || out != "branch=bump/alert-relay-1.2.4\ntitle=chore(apps): bump alert-relay to 1.2.4\n" {
		t.Fatalf("bump name: %d %q", code, out)
	}
}
