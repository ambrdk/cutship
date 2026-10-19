package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const own = "ghcr.io/ambrdk/alert-relay"

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
	root := deployRepo(t, map[string]string{"alert-relay": own + ":v0.2.3"})
	if code, out, _ := run(t, "", "pin", "read", "--root", root, "alert-relay"); code != 0 || out != own+":v0.2.3\n" {
		t.Fatalf("read: %d %q", code, out)
	}
	if code, out, _ := run(t, "", "pin", "check", "--root", root, "alert-relay", own, "v0.2.4"); code != 0 || out != "bump\n" {
		t.Fatalf("check: %d %q", code, out)
	}
	if code, out, _ := run(t, "", "pin", "set", "--root", root, "alert-relay", own, "v0.2.4"); code != 0 || out != "" {
		t.Fatalf("set: %d %q", code, out)
	}
	if code, out, _ := run(t, "", "pin", "check", "--root", root, "alert-relay", own, "v0.2.4"); code != 0 || out != "same\n" {
		t.Fatalf("check after set: %d %q", code, out)
	}
	if code, out, errOut := run(t, "", "pin", "verify", "--root", root); code != 0 || out != "" || errOut != "" {
		t.Fatalf("verify: %d %q %q", code, out, errOut)
	}
}

func TestPinErrorExit(t *testing.T) {
	root := deployRepo(t, map[string]string{"alert-relay": own + ":v0.2.5", "loose": own + ":latest"})
	code, _, errOut := run(t, "", "pin", "check", "--root", root, "alert-relay", own, "v0.2.4")
	if code != 1 || !strings.HasPrefix(errOut, "::error::") || !strings.Contains(errOut, "lower than") {
		t.Fatalf("check: %d %q", code, errOut)
	}
	code, _, errOut = run(t, "", "pin", "verify", "--root", root)
	if code != 1 || !strings.Contains(errOut, "::error::loose") {
		t.Fatalf("verify: %d %q", code, errOut)
	}
	if code, _, _ := run(t, "", "pin", "check", "--root", root, "alert-relay"); code != 2 {
		t.Fatalf("missing args: %d", code)
	}
}

func TestLaneCLI(t *testing.T) {
	fast := `[{"filename":"apps/alert-relay/app.yml","status":"modified","patch":"@@ -1 +1 @@\n-image: ` + own + `:v0.2.3\n+image: ` + own + `:v0.2.4"}]`
	if code, out, _ := run(t, fast, "lane"); code != 0 || out != "fast\n" {
		t.Fatalf("fast: %d %q", code, out)
	}
	if code, out, _ := run(t, "[]", "lane"); code != 0 || out != "full\n" {
		t.Fatalf("full: %d %q", code, out)
	}
	if code, _, errOut := run(t, "{", "lane"); code != 1 || !strings.HasPrefix(errOut, "::error::") {
		t.Fatalf("bad json: %d %q", code, errOut)
	}
}
