package pin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const own = "ghcr.io/ambrdk/alert-relay"

func appYML(ref string) string {
	return "---\nimage: " + ref + "\nhealth_url: \"http://127.0.0.1:9095/healthz\"\nvolumes:\n  - alert-relay-data\n"
}

func repo(t *testing.T, apps map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range apps {
		dir := filepath.Join(root, "apps", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "app.yml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestParseRef(t *testing.T) {
	for ref, want := range map[string][2]string{
		"ghcr.io/ambrdk/x:v1.2.3": {"ghcr.io/ambrdk/x", "v1.2.3"},
		"localhost:5000/x":        {"localhost:5000/x", ""},
		"localhost:5000/x:1.0":    {"localhost:5000/x", "1.0"},
	} {
		if r, tag := ParseRef(ref); r != want[0] || tag != want[1] {
			t.Errorf("%s: %q %q", ref, r, tag)
		}
	}
}

func TestRead(t *testing.T) {
	cases := map[string]string{
		appYML(own + ":v0.3.0"):             own + ":v0.3.0",
		appYML(`"` + own + `:v0.3.0"`):      own + ":v0.3.0",
		appYML(`'` + own + `:v0.3.0'`):      own + ":v0.3.0",
		"---\nhealth_url: x\n":              "",
		"---\nnested:\n  image: x:v1.0.0\n": "",
		"---\nimage: \"x:v1.0.0'\n":         "",
	}
	for body, want := range cases {
		root := repo(t, map[string]string{"alert-relay": body})
		got, err := Read(root, "alert-relay")
		if want == "" {
			if !errors.Is(err, ErrRule) {
				t.Errorf("%q: got %q err %v", body, got, err)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q: got %q err %v", body, got, err)
		}
	}
	if _, err := Read(t.TempDir(), "alert-relay"); !errors.Is(err, ErrRule) {
		t.Errorf("missing file: %v", err)
	}
}

func TestLine(t *testing.T) {
	if ref, ok := Line([]byte("---\r\nimage: x/y:v1.0.0\r\n")); !ok || ref != "x/y:v1.0.0" {
		t.Fatalf("crlf: %q %v", ref, ok)
	}
	if _, ok := Line([]byte("  image: x/y:v1.0.0\n")); ok {
		t.Fatal("indented line accepted")
	}
}

func check(t *testing.T, current, image, tag string) (bool, error) {
	t.Helper()
	return Check(current, image, tag)
}

func TestCheckOwn(t *testing.T) {
	ok := []struct {
		current, tag string
		bump         bool
	}{
		{own + ":v0.2.3", "v0.2.4", true},
		{own + ":v0.9.0", "v0.10.0", true},
		{own + ":v0.2.4", "v0.2.4", false},
	}
	for _, tc := range ok {
		if bump, err := check(t, tc.current, own, tc.tag); err != nil || bump != tc.bump {
			t.Errorf("%s → %s: bump %v err %v", tc.current, tc.tag, bump, err)
		}
	}
	bad := []struct{ current, image, tag, text string }{
		{own + ":v0.2.5", own, "v0.2.4", "lower than"},
		{own + ":v0.2.3", own, "v1.2", "tag format"},
		{own + ":latest", own, "v0.3.0", "current tag"},
		{own, own, "v0.3.0", "current tag"},
		{"ghcr.io/other/alert-relay:v0.2.3", own, "v0.3.0", "image mismatch"},
	}
	for _, tc := range bad {
		_, err := check(t, tc.current, tc.image, tc.tag)
		if !errors.Is(err, ErrRule) || !strings.Contains(err.Error(), tc.text) {
			t.Errorf("%s → %s: err %v, want %q", tc.current, tc.tag, err, tc.text)
		}
	}
}

func TestCheckForeign(t *testing.T) {
	if bump, err := check(t, "valkey/valkey:9.1.1", "valkey/valkey", "9.1.2"); err != nil || !bump {
		t.Errorf("upgrade: %v %v", bump, err)
	}
	if bump, err := check(t, "valkey/valkey:9.1.2", "valkey/valkey", "9.1.2"); err != nil || bump {
		t.Errorf("same: %v %v", bump, err)
	}
	if _, err := check(t, "valkey/valkey:9.1.2", "valkey/valkey", "9.1.1"); err == nil || !strings.Contains(err.Error(), "lower than") {
		t.Errorf("downgrade: %v", err)
	}
	if _, err := check(t, "valkey/valkey:9.1.2", "valkey/valkey", "latest"); err == nil || !strings.Contains(err.Error(), "tag format") {
		t.Errorf("latest: %v", err)
	}
	if bump, err := check(t, "x/y:stable-a", "x/y", "stable-b"); err != nil || !bump {
		t.Errorf("no number: %v %v", bump, err)
	}
}

func TestSetKeepsQuotes(t *testing.T) {
	body := appYML(`"` + own + `:v0.2.3"`)
	root := repo(t, map[string]string{"alert-relay": body})
	if err := Set(root, "alert-relay", own, "v0.2.4"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "apps", "alert-relay", "app.yml"))
	if string(got) != strings.Replace(body, "v0.2.3", "v0.2.4", 1) {
		t.Fatalf("got %q", got)
	}
}

func TestSetKeepsCRLFAndNoFinalNewline(t *testing.T) {
	body := "---\r\nimage: " + own + ":v0.2.3\r\nhealth_url: x\r\nvolumes: []"
	root := repo(t, map[string]string{"alert-relay": body})
	if err := Set(root, "alert-relay", own, "v0.2.4"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "apps", "alert-relay", "app.yml"))
	if string(got) != strings.Replace(body, "v0.2.3", "v0.2.4", 1) {
		t.Fatalf("got %q", got)
	}
}

func TestSetRefusesTwoImageLines(t *testing.T) {
	root := repo(t, map[string]string{"alert-relay": "---\nimage: " + own + ":v0.2.3\nimage: " + own + ":v0.2.3\n"})
	if err := Set(root, "alert-relay", own, "v0.2.4"); !errors.Is(err, ErrRule) {
		t.Fatalf("err %v", err)
	}
}

func TestSetRejectsBadTag(t *testing.T) {
	root := repo(t, map[string]string{"alert-relay": appYML(own + ":v0.2.3")})
	if err := Set(root, "alert-relay", own, "latest"); !errors.Is(err, ErrRule) {
		t.Fatalf("err %v", err)
	}
}

func patch(old, new, extra string) string {
	return "@@ -1,3 +1,3 @@\n ---\n-image: " + old + "\n+image: " + new + "\n health_url: x" + extra
}

func appFile(old, new string) File {
	return File{Filename: "apps/alert-relay/app.yml", Status: "modified", Patch: patch(old, new, "")}
}

func TestImageOnly(t *testing.T) {
	env := File{Filename: "apps/alert-relay/env.j2", Status: "modified", Patch: "+X=1"}
	cases := []struct {
		name  string
		files []File
		fast  bool
	}{
		{"own bump", []File{appFile(own+":v0.2.3", own+":v0.2.4")}, true},
		{"quoted", []File{appFile(`"`+own+`:v0.2.3"`, `"`+own+`:v0.2.4"`)}, true},
		{"other field", []File{{Filename: "apps/alert-relay/app.yml", Status: "modified", Patch: "@@ -2 +2 @@\n-health_url: a\n+health_url: b"}}, false},
		{"image and field", []File{{Filename: "apps/alert-relay/app.yml", Status: "modified", Patch: patch(own+":v0.2.3", own+":v0.2.4", "\n-volumes: []\n+volumes: [x]")}}, false},
		{"two files", []File{appFile(own+":v0.2.3", own+":v0.2.4"), env}, false},
		{"other repo", []File{appFile(own+":v0.2.3", "docker.io/x/alert-relay:v0.2.4")}, false},
		{"foreign upgrade", []File{appFile("valkey/valkey:9.1.1", "valkey/valkey:9.1.2")}, true},
		{"foreign downgrade", []File{appFile("valkey/valkey:9.1.2", "valkey/valkey:9.1.1")}, false},
		{"foreign suffix upgrade", []File{appFile("grafana/grafana:11.3.0-ubuntu", "grafana/grafana:11.4.0-ubuntu")}, true},
		{"foreign suffix flip", []File{appFile("grafana/grafana:11.3.0-ubuntu", "grafana/grafana:11.3.0-alpine")}, false},
		{"foreign suffix down", []File{appFile("grafana/grafana:11.4.0", "grafana/grafana:11.3.0-ubuntu")}, false},
		{"no number", []File{appFile("x/y:stable-a", "x/y:stable-b")}, false},
		{"number to word", []File{appFile("valkey/valkey:9.1.2", "valkey/valkey:alpine")}, false},
		{"own not semver", []File{appFile(own+":v0.2.3", own+":0.2.4")}, false},
		{"own downgrade", []File{appFile(own+":v0.2.4", own+":v0.2.3")}, false},
		{"latest", []File{appFile(own+":v0.2.3", own+":latest")}, false},
		{"added", []File{{Filename: "apps/alert-relay/app.yml", Status: "added", Patch: patch(own+":v0.2.3", own+":v0.2.4", "")}}, false},
		{"other path", []File{{Filename: "apps/alert-relay/compose.yml", Status: "modified", Patch: patch(own+":v0.2.3", own+":v0.2.4", "")}}, false},
		{"no patch", []File{{Filename: "apps/alert-relay/app.yml", Status: "modified"}}, false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		if got := ImageOnly(tc.files); got != tc.fast {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestVerify(t *testing.T) {
	root := repo(t, map[string]string{
		"alert-relay": appYML(own + ":v0.3.0"),
		"cache":       appYML("valkey/valkey:9.1.2"),
		"loose":       appYML(own + ":latest"),
		"noimage":     "---\nhealth_url: x\n",
		"Bad_Name":    appYML("x/y:1.0.0"),
	})
	errs := Verify(root)
	var text []string
	for _, err := range errs {
		text = append(text, err.Error())
	}
	joined := strings.Join(text, "\n")
	if len(errs) != 3 || !strings.Contains(joined, "loose") || !strings.Contains(joined, "noimage") || !strings.Contains(joined, "Bad_Name") {
		t.Fatalf("errors:\n%s", joined)
	}
}

func TestValidApp(t *testing.T) {
	for name, want := range map[string]bool{"alert-relay": true, "a1": true, "Alert": false, "-a": false, "a b": false, "": false, "../x": false} {
		if ValidApp(name) != want {
			t.Errorf("%q", name)
		}
	}
}
