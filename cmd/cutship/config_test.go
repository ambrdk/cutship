package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/forge/fake"
)

const goodConfig = `version: 1
forge:
  type: github
deploy_repo: ambrdk/ansible
apps:
  - name: alert-relay
    image: ghcr.io/ambrdk/alert-relay
    pin: { preset: ansible }
`

func writeConfig(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cutship.yaml")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidateCLI(t *testing.T) {
	good := writeConfig(t, goodConfig)
	if code, out, errOut := run(t, "", "validate", "--config", good); code != 0 || out != "" || errOut != "" {
		t.Fatalf("good: %d %q %q", code, out, errOut)
	}

	bad := writeConfig(t, strings.Replace(goodConfig, "deploy_repo:", "deploy_rep: x\ndeploy_repo:", 1)+"notify:\n  - type: slack\n")
	code, out, errOut := run(t, "", "validate", "--config", bad)
	if code != 1 || out != "" {
		t.Fatalf("bad: %d %q", code, out)
	}
	for _, want := range []string{
		"::error file=" + bad + ",line=4::deploy_rep: unknown field \"deploy_rep\"\n",
		"::error file=" + bad + ",line=11::notify[0].type: slack is not supported yet, planned\n",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr %q\nlacks %q", errOut, want)
		}
	}
	if strings.Count(errOut, "::error") != 2 {
		t.Errorf("stderr %q", errOut)
	}

	missing := filepath.Join(t.TempDir(), "cutship.yaml")
	if code, _, errOut := run(t, "", "validate", "--config", missing); code != 1 || !strings.HasPrefix(errOut, "::error file="+missing+"::") {
		t.Fatalf("missing: %d %q", code, errOut)
	}
}

func TestValidateOnlineCLI(t *testing.T) {
	cfg := writeConfig(t, ambrdkConfig)
	f := fake.New("github-actions[bot]")
	f.Commit("ambrdk/ansible", "main", map[string][]byte{
		"cutship.yaml":             []byte(ambrdkConfig),
		"apps/alert-relay/app.yml": []byte("image: " + own + ":v0.3.0\n"),
	})
	push := true
	f.SetSettings("ambrdk/ansible", forge.RepoSettings{Push: &push, AutoMerge: &push, DeleteBranchOnMerge: &push})
	useForge(t, f)
	useRegistry(t, images{own + ":v0.3.0": "abc"})
	code, out, errOut := run(t, "", "validate", "--config", cfg, "--online")
	if code != 0 || out != "" || !strings.Contains(errOut, "::warning file="+cfg+"::") || !strings.Contains(errOut, "GITHUB_TOKEN") {
		t.Fatalf("warning only: %d %q %q", code, out, errOut)
	}
	useRegistry(t, images{})
	code, _, errOut = run(t, "", "validate", "--config", cfg, "--online")
	if code != 1 || !strings.Contains(errOut, "::error file="+cfg+"::apps[0]: the pinned image") {
		t.Fatalf("missing image: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, "", "validate", "--config", cfg); code != 0 || errOut != "" {
		t.Fatalf("offline: %d %q", code, errOut)
	}
}

func TestValidateCLIDefaultPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cutship.yaml"), []byte(goodConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if code, _, errOut := run(t, "", "validate"); code != 0 {
		t.Fatalf("default path: %d %q", code, errOut)
	}
}

func TestValidateCLIUsage(t *testing.T) {
	for _, args := range [][]string{{"validate", "extra"}, {"validate", "--bogus"}, {"schema", "extra"}} {
		if code, _, errOut := run(t, "", args...); code != 2 || !strings.Contains(errOut, "usage: cutship") {
			t.Errorf("%v: %d %q", args, code, errOut)
		}
	}
}

func TestSchemaCLI(t *testing.T) {
	code, out, errOut := run(t, "", "schema")
	if code != 0 || errOut != "" {
		t.Fatalf("%d %q", code, errOut)
	}
	var s map[string]any
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("not json: %v", err)
	}
	if s["$schema"] == nil || !strings.HasSuffix(out, "}\n") {
		t.Errorf("schema output %q", out)
	}
}
