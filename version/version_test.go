package version_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/version"
)

func TestNext(t *testing.T) {
	for _, c := range []struct {
		want, last string
		subjects   []string
	}{
		{"0.2.4", "0.2.3", []string{"fix(relay): describe the tracked gauge"}},
		{"0.3.0", "0.2.3", []string{"feat: add x"}},
		{"0.3.0", "0.2.3", []string{"feat!: drop y"}},
		{"2.0.0", "1.2.3", []string{"fix(api)!: change z"}},
		{"1.2.4", "1.2.3", []string{"perf: faster"}},
		{"", "0.2.3", []string{"chore: a", "docs: b", "ci: c"}},
		{"0.3.0", "0.2.3", []string{"fix: a", "feat: b"}},
		{"0.1.0", "0.0.0", []string{"feat: first"}},
		{"0.0.1", "0.0.0", []string{"fix: first"}},
		{"", "0.2.3", []string{"fix:no space"}},
		{"", "0.2.3", nil},
		{"1.3.0", "1.2.9", []string{"feat(cmd): x", "fix: y", "chore: z"}},
		{"2.0.0", "1.2.9", []string{"chore!: drop node 18", "feat: x"}},
		{"0.2.4", "0.2.3", []string{"", "fix: a", ""}},
	} {
		got, _ := version.Next(c.last, c.subjects)
		if got != c.want {
			t.Errorf("Next(%q, %q) = %q, want %q", c.last, c.subjects, got, c.want)
		}
	}
}

func TestNextWarnings(t *testing.T) {
	next, warnings := version.Next("0.2.3", []string{"Merge pull request #1 from ambrdk/x", "fix: a", "chore: b"})
	if next != "0.2.4" {
		t.Errorf("next = %q", next)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "Merge pull request #1") {
		t.Errorf("warnings = %q", warnings)
	}
	if next, warnings := version.Next("v0.2.3", []string{"feat: a"}); next != "" || len(warnings) != 1 {
		t.Errorf("bad last: %q %q", next, warnings)
	}
}

var vFormat = config.Release{TagFormat: "v{version}"}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	for _, kv := range [][2]string{
		{"user.email", "test@example.com"},
		{"user.name", "test"},
		{"commit.gpgsign", "false"},
		{"tag.gpgsign", "false"},
	} {
		git(t, dir, "config", kv[0], kv[1])
	}
	return dir
}

func commit(t *testing.T, dir, subject string) {
	t.Helper()
	git(t, dir, "commit", "-q", "--allow-empty", "-m", subject)
}

func resolve(t *testing.T, dir string, rel config.Release, want string) {
	t.Helper()
	got, _, err := version.Resolve(dir, rel)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

func TestResolveSinceTag(t *testing.T) {
	r := repo(t)
	commit(t, r, "feat: a")
	git(t, r, "tag", "v0.1.0")
	commit(t, r, "fix: b")
	commit(t, r, "docs: c")
	resolve(t, r, vFormat, "v0.1.1")
}

func TestResolveHeadTagged(t *testing.T) {
	r := repo(t)
	commit(t, r, "feat: a")
	git(t, r, "tag", "v0.4.2")
	git(t, r, "tag", "v0.4.10")
	resolve(t, r, vFormat, "v0.4.10")
}

func TestResolveNoTags(t *testing.T) {
	r := repo(t)
	commit(t, r, "chore: init")
	commit(t, r, "feat: first")
	resolve(t, r, vFormat, "v0.1.0")
}

func TestResolveNothing(t *testing.T) {
	r := repo(t)
	commit(t, r, "feat: a")
	git(t, r, "tag", "v0.1.0")
	commit(t, r, "ci: b")
	resolve(t, r, vFormat, "")
}

func TestResolveIgnoresMovingV1(t *testing.T) {
	r := repo(t)
	commit(t, r, "feat: a")
	git(t, r, "tag", "v0.1.0")
	git(t, r, "tag", "v1")
	git(t, r, "tag", "v0.9.0-rc.1")
	commit(t, r, "fix: b")
	resolve(t, r, vFormat, "v0.1.1")
}

func TestResolveHighestTag(t *testing.T) {
	r := repo(t)
	commit(t, r, "feat: a")
	git(t, r, "tag", "v0.9.0")
	commit(t, r, "feat: b")
	git(t, r, "tag", "v0.10.0")
	commit(t, r, "fix: c")
	resolve(t, r, vFormat, "v0.10.1")
}

func TestResolveWarnings(t *testing.T) {
	r := repo(t)
	commit(t, r, "Initial commit")
	commit(t, r, "fix: a")
	got, warnings, err := version.Resolve(r, vFormat)
	if err != nil || got != "v0.0.1" {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "Initial commit") {
		t.Errorf("warnings = %q", warnings)
	}
}

func TestResolveShallow(t *testing.T) {
	src := repo(t)
	commit(t, src, "feat: a")
	commit(t, src, "fix: b")
	dst := filepath.Join(t.TempDir(), "shallow")
	git(t, src, "clone", "-q", "--depth", "1", "file://"+src, dst)
	_, _, err := version.Resolve(dst, vFormat)
	if err == nil || !strings.Contains(err.Error(), "fetch-depth: 0") {
		t.Errorf("err = %v", err)
	}
}

func TestResolvePlainFormat(t *testing.T) {
	plain := config.Release{TagFormat: "{version}"}
	r := repo(t)
	commit(t, r, "feat: a")
	git(t, r, "tag", "0.1.0")
	commit(t, r, "docs: b")
	git(t, r, "tag", "v9.9.9")
	commit(t, r, "fix: c")
	resolve(t, r, plain, "0.1.1")
	git(t, r, "tag", "0.1.1")
	resolve(t, r, plain, "0.1.1")
}

func TestResolveNotARepo(t *testing.T) {
	if _, _, err := version.Resolve(t.TempDir(), vFormat); err == nil {
		t.Error("no error outside a repository")
	}
	if _, _, err := version.Resolve(filepath.Join(t.TempDir(), "missing"), vFormat); err == nil {
		t.Error("no error for a missing directory")
	}
}
