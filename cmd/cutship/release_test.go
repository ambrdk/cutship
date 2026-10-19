package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ambrdk/cutship/forge/fake"
	"github.com/ambrdk/cutship/registry"
)

type images map[string]string

func (m images) Revision(_ context.Context, img, tag string) (string, error) {
	rev, ok := m[img+":"+tag]
	if !ok {
		return "", fmt.Errorf("%s:%s: %w", img, tag, registry.ErrMissing)
	}
	return rev, nil
}

func useRegistry(t *testing.T, reg registry.Registry) {
	t.Helper()
	old := newRegistry
	newRegistry = func() registry.Registry { return reg }
	t.Cleanup(func() { newRegistry = old })
}

func releaseWorld(t *testing.T) (*fake.Forge, string) {
	t.Helper()
	f := fake.New("bot")
	sha := f.Commit("ambrdk/alert-relay", "main", map[string][]byte{"main.go": []byte("package main")})
	f.Commit("ambrdk/ansible", "main", map[string][]byte{
		"cutship.yaml":             []byte(ambrdkConfig),
		"apps/alert-relay/app.yml": []byte("---\nimage: " + own + ":v0.3.0\n"),
	})
	useForge(t, f)
	useRegistry(t, images{own + ":v0.3.1": sha})
	t.Setenv("GITHUB_REPOSITORY", "ambrdk/alert-relay")
	t.Setenv("GITHUB_SHA", sha)
	t.Setenv("GITHUB_TOKEN", "service-token")
	t.Setenv("CUTSHIP_DEPLOY_TOKEN", "deploy-token")
	return f, sha
}

func TestReleaseCLIOutputs(t *testing.T) {
	f, sha := releaseWorld(t)
	code, out, errOut := run(t, "", "release", "--app", "alert-relay", "--tag", "v0.3.1", "--deploy-repo", "ambrdk/ansible")
	if code != 0 || out != "outcome=bump\ntag=v0.3.1\npr=1\nurl=https://forge.test/ambrdk/ansible/pull/1\n" {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	if f.Tags("ambrdk/alert-relay")["v0.3.1"] != sha {
		t.Fatal("tag missing")
	}
	code, out, _ = run(t, "", "release", "--app", "alert-relay", "--tag", "v0.3.1", "--deploy-repo", "ambrdk/ansible")
	if code != 0 || out != "outcome=bump\ntag=v0.3.1\npr=1\nurl=https://forge.test/ambrdk/ansible/pull/1\n" {
		t.Fatalf("rerun: %d %q", code, out)
	}
}

func TestReleaseCLIErrors(t *testing.T) {
	f, _ := releaseWorld(t)
	if code, _, errOut := run(t, "", "release", "--app", "alert-relay", "--tag", "v0.3.2", "--deploy-repo", "ambrdk/ansible"); code != 1 || !strings.Contains(errOut, "::error::image ghcr.io/ambrdk/alert-relay:v0.3.2 not found") {
		t.Fatalf("missing image: %d %q", code, errOut)
	}
	f.Commit("ambrdk/ansible", "main", map[string][]byte{"cutship.yaml": []byte("version: 2\n")})
	if code, _, errOut := run(t, "", "release", "--app", "alert-relay", "--tag", "v0.3.1", "--deploy-repo", "ambrdk/ansible"); code != 1 || !strings.Contains(errOut, "::error file=ambrdk/ansible:cutship.yaml") {
		t.Fatalf("bad config: %d %q", code, errOut)
	}
	for _, args := range [][]string{
		{"release", "--tag", "v0.3.1", "--deploy-repo", "ambrdk/ansible"},
		{"release", "--app", "alert-relay", "--deploy-repo", "ambrdk/ansible"},
		{"release", "--app", "alert-relay", "--tag", "v0.3.1"},
		{"release", "--app", "alert-relay", "--tag", "v0.3.1", "--deploy-repo", "ambrdk/ansible", "extra"},
	} {
		if code, _, _ := run(t, "", args...); code != 2 {
			t.Errorf("%v: code %d", args, code)
		}
	}
}

func TestReleaseCLITelegramWithoutTokenWarns(t *testing.T) {
	f, _ := releaseWorld(t)
	f.Commit("ambrdk/ansible", "main", map[string][]byte{"cutship.yaml": []byte(ambrdkConfig + "notify:\n  - type: telegram\n    chat_id: \"-100123\"\n")})
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	code, out, errOut := run(t, "", "release", "--app", "alert-relay", "--tag", "v0.3.1", "--deploy-repo", "ambrdk/ansible")
	if code != 0 || !strings.HasPrefix(out, "outcome=bump\n") || !strings.Contains(errOut, "::warning::the release goes on without a post: TELEGRAM_BOT_TOKEN is required") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
}

func TestReleaseCLIMissingDeployToken(t *testing.T) {
	releaseWorld(t)
	t.Setenv("CUTSHIP_DEPLOY_TOKEN", "")
	code, _, errOut := run(t, "", "release", "--app", "alert-relay", "--tag", "v0.3.1", "--deploy-repo", "ambrdk/ansible")
	if code != 1 || !strings.Contains(errOut, "CUTSHIP_DEPLOY_TOKEN") {
		t.Fatalf("%d %q", code, errOut)
	}
}
