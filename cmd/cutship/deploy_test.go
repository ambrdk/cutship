package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/forge/fake"
	"github.com/ambrdk/cutship/ledger"
)

func deployWorld(t *testing.T) (*fake.Forge, string, string, string) {
	t.Helper()
	f := fake.New("bot")
	base := f.Commit("ambrdk/ansible", "main", map[string][]byte{"apps/alert-relay/app.yml": []byte("image: " + own + ":v0.3.0\n")})
	head := f.Commit("ambrdk/ansible", "main", map[string][]byte{"apps/alert-relay/app.yml": []byte("image: " + own + ":v0.3.1\n")})
	useForge(t, f)
	t.Setenv("GITHUB_REPOSITORY", "ambrdk/ansible")
	t.Setenv("GITHUB_SHA", head)
	t.Setenv("GITHUB_TOKEN", "token")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_RUN_ID", "77")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	return f, writeConfig(t, ambrdkConfig), base, head
}

func TestDeployCLI(t *testing.T) {
	f, cfg, base, head := deployWorld(t)
	code, out, errOut := run(t, "", "deploy", "plan", "--config", cfg, "--base", base)
	if code != 0 || out != "apps=[\"alert-relay\"]\nskip_ci=false\nchange=0\n" {
		t.Fatalf("plan: %d %q %q", code, out, errOut)
	}
	if code, out, errOut := run(t, "", "deploy", "start", "--config", cfg, "--apps", `["alert-relay"]`); code != 0 || out != "" {
		t.Fatalf("start: %d %q %q", code, out, errOut)
	}
	report := filepath.Join(t.TempDir(), "deploy-report.json")
	if err := os.WriteFile(report, []byte(`{"version":1,"hosts":{"mon-1":{"ok":5,"changed":1}},"failure":null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := run(t, "", "deploy", "finish", "--config", cfg, "--apps", `["alert-relay"]`, "--result", "success", "--report", report); code != 0 {
		t.Fatalf("finish: %d %q", code, errOut)
	}
	got, ok, _ := ledger.LastSuccess(context.Background(), f, "ambrdk/ansible", "prod", "alert-relay")
	if !ok || got.SHA != head || f.DeploymentLog("ambrdk/ansible", got.ID) != "https://github.com/ambrdk/ansible/actions/runs/77" {
		t.Fatalf("%+v %v", got, ok)
	}
	if code, out, _ := run(t, "", "deploy", "plan", "--config", cfg, "--base", head); code != 0 || out != "apps=[]\nskip_ci=false\nchange=0\n" {
		t.Fatalf("after deploy: %d %q", code, out)
	}
	if code, out, _ := run(t, "", "deploy", "plan", "--config", cfg, "--base", strings.Repeat("0", 40)); code != 0 || out != "apps=[]\nskip_ci=false\nchange=0\n" {
		t.Fatalf("zero base: %d %q", code, out)
	}
}

func TestDeployCLIWarnsWithoutTelegramToken(t *testing.T) {
	_, _, _, _ = deployWorld(t)
	cfg := writeConfig(t, ambrdkConfig+"notify:\n  - type: telegram\n    chat_id: \"-100123\"\n")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	code, out, errOut := run(t, "", "deploy", "start", "--config", cfg, "--apps", `["alert-relay"]`)
	if code != 0 || out != "" || !strings.Contains(errOut, "::warning::the deploy goes on without a post: TELEGRAM_BOT_TOKEN is required") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
}

func TestDeployCLIErrors(t *testing.T) {
	f, cfg, base, _ := deployWorld(t)
	for _, args := range [][]string{
		{"deploy"},
		{"deploy", "plan", "--config", cfg},
		{"deploy", "start", "--config", cfg},
		{"deploy", "finish", "--config", cfg, "--apps", `["a"]`},
		{"deploy", "finish", "--config", cfg, "--apps", `["a"]`, "--result", "maybe"},
		{"deploy", "bogus", "--config", cfg},
	} {
		if code, _, _ := run(t, "", args...); code != 2 {
			t.Errorf("%v: code %d", args, code)
		}
	}
	if code, _, errOut := run(t, "", "deploy", "start", "--config", cfg, "--apps", `not json`); code != 1 || !strings.Contains(errOut, "::error::") {
		t.Errorf("bad apps: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, "", "deploy", "start", "--config", cfg, "--apps", `["../x"]`); code != 1 || !strings.Contains(errOut, "../x") {
		t.Errorf("bad app name: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, "", "deploy", "plan", "--config", cfg, "--base", base, "--env", "staging"); code != 1 || !strings.Contains(errOut, "staging") {
		t.Errorf("bad env: %d %q", code, errOut)
	}
	if code, _, errOut := run(t, "", "deploy", "finish", "--config", cfg, "--apps", `["alert-relay"]`, "--result", "success", "--report", filepath.Join(t.TempDir(), "missing.json")); code != 1 || !strings.Contains(errOut, "missing.json") {
		t.Errorf("missing report: %d %q", code, errOut)
	}
	f.FailNext("Subtrees", forge.ErrNotFound)
	if code, _, errOut := run(t, "", "deploy", "plan", "--config", cfg, "--base", base); code != 1 || !strings.Contains(errOut, "list apps/") {
		t.Errorf("forge error: %d %q", code, errOut)
	}
}
