package pin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/forge"
)

const (
	own   = "ghcr.io/ambrdk/alert-relay"
	other = "ghcr.io/ambrdk/api"
)

var (
	rel      = config.Release{TagFormat: "v{version}"}
	plain    = config.Release{TagFormat: "{version}"}
	relayApp = config.App{Name: "alert-relay", Image: own, Pin: config.Pin{Preset: "ansible"}}
	apiApp   = config.App{Name: "api", Image: other, Pin: config.Pin{Preset: "ansible"}}
	cfg      = config.Config{Apps: []config.App{relayApp, apiApp}, Release: rel}
)

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
		got, err := Read(root, relayApp)
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
	if _, err := Read(t.TempDir(), relayApp); !errors.Is(err, ErrRule) {
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

func TestPath(t *testing.T) {
	if got := Path(relayApp); got != "apps/alert-relay/app.yml" {
		t.Errorf("Path = %q", got)
	}
}

func TestCheck(t *testing.T) {
	ok := []struct {
		current, tag string
		bump         bool
	}{
		{own + ":v0.2.3", "v0.2.4", true},
		{own + ":v0.9.0", "v0.10.0", true},
		{own + ":v0.2.4", "v0.2.4", false},
	}
	for _, tc := range ok {
		if bump, err := Check(relayApp, rel, tc.current, tc.tag); err != nil || bump != tc.bump {
			t.Errorf("%s → %s: bump %v err %v", tc.current, tc.tag, bump, err)
		}
	}
	bad := []struct{ current, tag, text string }{
		{own + ":v0.2.5", "v0.2.4", "lower than"},
		{own + ":v0.2.3", "v1.2", "tag format"},
		{own + ":v0.2.3", "0.2.4", "tag format"},
		{own + ":v0.2.3", "v0.2.4-rc.1", "tag format"},
		{own + ":v0.2.3", "latest", "tag format"},
		{own + ":latest", "v0.3.0", "current tag"},
		{own, "v0.3.0", "current tag"},
		{"ghcr.io/other/alert-relay:v0.2.3", "v0.3.0", "image mismatch"},
	}
	for _, tc := range bad {
		_, err := Check(relayApp, rel, tc.current, tc.tag)
		if !errors.Is(err, ErrRule) || !strings.Contains(err.Error(), tc.text) {
			t.Errorf("%s → %s: err %v, want %q", tc.current, tc.tag, err, tc.text)
		}
	}
}

func TestCheckPlainFormat(t *testing.T) {
	if bump, err := Check(relayApp, plain, own+":1.2.3", "1.2.4"); err != nil || !bump {
		t.Errorf("plain bump: %v %v", bump, err)
	}
	if _, err := Check(relayApp, plain, own+":1.2.3", "v1.2.4"); !errors.Is(err, ErrRule) || !strings.Contains(err.Error(), "{version}") {
		t.Errorf("v tag in plain format: %v", err)
	}
}

func TestSetKeepsQuotes(t *testing.T) {
	body := appYML(`"` + own + `:v0.2.3"`)
	root := repo(t, map[string]string{"alert-relay": body})
	if err := Set(root, relayApp, rel, "v0.2.4"); err != nil {
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
	if err := Set(root, relayApp, rel, "v0.2.4"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "apps", "alert-relay", "app.yml"))
	if string(got) != strings.Replace(body, "v0.2.3", "v0.2.4", 1) {
		t.Fatalf("got %q", got)
	}
}

func TestSetWritesTheConfiguredImage(t *testing.T) {
	root := repo(t, map[string]string{"alert-relay": appYML("ghcr.io/old/alert-relay:v0.2.3")})
	if err := Set(root, relayApp, rel, "v0.2.4"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Read(root, relayApp); got != own+":v0.2.4" {
		t.Fatalf("got %q", got)
	}
}

func TestSetRefusesTwoImageLines(t *testing.T) {
	root := repo(t, map[string]string{"alert-relay": "---\nimage: " + own + ":v0.2.3\nimage: " + own + ":v0.2.3\n"})
	if err := Set(root, relayApp, rel, "v0.2.4"); !errors.Is(err, ErrRule) {
		t.Fatalf("err %v", err)
	}
}

func TestSetRejectsBadTag(t *testing.T) {
	root := repo(t, map[string]string{"alert-relay": appYML(own + ":v0.2.3")})
	for _, tag := range []string{"latest", "0.2.4", "v0.2"} {
		if err := Set(root, relayApp, rel, tag); !errors.Is(err, ErrRule) {
			t.Fatalf("%s: err %v", tag, err)
		}
	}
}

func patch(old, new, extra string) string {
	return "@@ -1,3 +1,3 @@\n ---\n-image: " + old + "\n+image: " + new + "\n health_url: x" + extra
}

func appFile(old, new string) forge.File {
	return forge.File{Path: "apps/alert-relay/app.yml", Status: "modified", Patch: patch(old, new, "")}
}

func TestImageOnly(t *testing.T) {
	env := forge.File{Path: "apps/alert-relay/env.j2", Status: "modified", Patch: "+X=1"}
	cache := func(old, new string) forge.File {
		return forge.File{Path: "apps/cache/app.yml", Status: "modified", Patch: patch(old, new, "")}
	}
	cases := []struct {
		name  string
		files []forge.File
		fast  bool
	}{
		{"own bump", []forge.File{appFile(own+":v0.2.3", own+":v0.2.4")}, true},
		{"quoted", []forge.File{appFile(`"`+own+`:v0.2.3"`, `"`+own+`:v0.2.4"`)}, true},
		{"other field", []forge.File{{Path: "apps/alert-relay/app.yml", Status: "modified", Patch: "@@ -2 +2 @@\n-health_url: a\n+health_url: b"}}, false},
		{"image and field", []forge.File{{Path: "apps/alert-relay/app.yml", Status: "modified", Patch: patch(own+":v0.2.3", own+":v0.2.4", "\n-volumes: []\n+volumes: [x]")}}, false},
		{"two files", []forge.File{appFile(own+":v0.2.3", own+":v0.2.4"), env}, false},
		{"other repo", []forge.File{appFile(own+":v0.2.3", "docker.io/x/alert-relay:v0.2.4")}, false},
		{"own image of another app", []forge.File{appFile(other+":v0.2.3", other+":v0.2.4")}, false},
		{"foreign upgrade", []forge.File{cache("valkey/valkey:9.1.1", "valkey/valkey:9.1.2")}, true},
		{"foreign downgrade", []forge.File{cache("valkey/valkey:9.1.2", "valkey/valkey:9.1.1")}, false},
		{"foreign suffix upgrade", []forge.File{cache("grafana/grafana:11.3.0-ubuntu", "grafana/grafana:11.4.0-ubuntu")}, true},
		{"foreign suffix flip", []forge.File{cache("grafana/grafana:11.3.0-ubuntu", "grafana/grafana:11.3.0-alpine")}, false},
		{"foreign suffix down", []forge.File{cache("grafana/grafana:11.4.0", "grafana/grafana:11.3.0-ubuntu")}, false},
		{"no number", []forge.File{cache("x/y:stable-a", "x/y:stable-b")}, false},
		{"number to word", []forge.File{cache("valkey/valkey:9.1.2", "valkey/valkey:alpine")}, false},
		{"own not in format", []forge.File{appFile(own+":v0.2.3", own+":0.2.4")}, false},
		{"own prerelease", []forge.File{appFile(own+":v0.2.3", own+":v0.2.4-rc.1")}, false},
		{"own downgrade", []forge.File{appFile(own+":v0.2.4", own+":v0.2.3")}, false},
		{"latest", []forge.File{appFile(own+":v0.2.3", own+":latest")}, false},
		{"added", []forge.File{{Path: "apps/alert-relay/app.yml", Status: "added", Patch: patch(own+":v0.2.3", own+":v0.2.4", "")}}, false},
		{"other path", []forge.File{{Path: "apps/alert-relay/compose.yml", Status: "modified", Patch: patch(own+":v0.2.3", own+":v0.2.4", "")}}, false},
		{"no patch", []forge.File{{Path: "apps/alert-relay/app.yml", Status: "modified"}}, false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		if got := ImageOnly(cfg, tc.files); got != tc.fast {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestImageOnlyPlainFormat(t *testing.T) {
	plainCfg := config.Config{Apps: []config.App{relayApp}, Release: plain}
	if !ImageOnly(plainCfg, []forge.File{appFile(own+":1.2.3", own+":1.2.4")}) {
		t.Error("plain bump not fast")
	}
	if ImageOnly(plainCfg, []forge.File{appFile(own+":1.2.3", own+":v1.2.4")}) {
		t.Error("v tag fast in plain format")
	}
}

func TestVerify(t *testing.T) {
	root := repo(t, map[string]string{
		"alert-relay": appYML(own + ":v0.3.0"),
		"api":         appYML("ghcr.io/elsewhere/api:v1.0.0"),
		"cache":       appYML("valkey/valkey:9.1.2"),
		"loose":       appYML("valkey/valkey:latest"),
		"noimage":     "---\nhealth_url: x\n",
		"Bad_Name":    appYML("x/y:1.0.0"),
	})
	errs := Verify(root, cfg)
	var text []string
	for _, err := range errs {
		text = append(text, err.Error())
	}
	joined := strings.Join(text, "\n")
	if len(errs) != 4 {
		t.Fatalf("errors:\n%s", joined)
	}
	for _, want := range []string{"loose", "noimage", "Bad_Name", "api"} {
		if !strings.Contains(joined, want) {
			t.Errorf("errors do not mention %s:\n%s", want, joined)
		}
	}
}

func TestVerifyOwnTagFormat(t *testing.T) {
	root := repo(t, map[string]string{
		"alert-relay": appYML(own + ":0.3.0"),
		"api":         appYML(other + ":v1.0.0"),
	})
	errs := Verify(root, cfg)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "alert-relay") {
		t.Fatalf("errors %v", errs)
	}
}

func TestVerifyMissingConfiguredApp(t *testing.T) {
	root := repo(t, map[string]string{"alert-relay": appYML(own + ":v0.3.0")})
	errs := Verify(root, cfg)
	if len(errs) != 1 || !errors.Is(errs[0], ErrRule) || !strings.Contains(errs[0].Error(), "apps/api/app.yml") {
		t.Fatalf("errors %v", errs)
	}
}

func TestVerifyWithoutApps(t *testing.T) {
	if errs := Verify(t.TempDir(), config.Config{Release: rel}); len(errs) != 1 || !errors.Is(errs[0], ErrRule) || !strings.Contains(errs[0].Error(), "no apps") {
		t.Fatalf("errors %v", errs)
	}
}

func TestValidApp(t *testing.T) {
	for name, want := range map[string]bool{"alert-relay": true, "a1": true, "Alert": false, "-a": false, "a b": false, "": false, "../x": false} {
		if ValidApp(name) != want {
			t.Errorf("%q", name)
		}
	}
}
