package config_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ambrdk/cutship/config"
)

const base = `version: 1
forge:
  type: github
deploy_repo: ambrdk/ansible
apps:
  - name: alert-relay
    image: ghcr.io/ambrdk/alert-relay
    pin: { preset: ansible }
notify:
  - type: telegram
    chat_id: "-100123"
`

func load(t *testing.T, name string) (config.Config, []config.Problem) {
	t.Helper()
	return config.Load(filepath.Join("testdata", name))
}

func clean(t *testing.T, problems []config.Problem) {
	t.Helper()
	for _, p := range problems {
		t.Errorf("unexpected problem: %v", p)
	}
}

func find(problems []config.Problem, path string) (config.Problem, bool) {
	for _, p := range problems {
		if p.Path == path {
			return p, true
		}
	}
	return config.Problem{}, false
}

func expect(t *testing.T, problems []config.Problem, path string, words ...string) int {
	t.Helper()
	p, ok := find(problems, path)
	if !ok {
		t.Fatalf("no problem at %s in %v", path, problems)
	}
	for _, w := range words {
		if !strings.Contains(p.Error(), w) {
			t.Errorf("problem %q does not mention %q", p.Error(), w)
		}
	}
	return p.Line
}

func TestParseMinimal(t *testing.T) {
	cfg, problems := load(t, "minimal.yaml")
	clean(t, problems)
	if cfg.Version != 1 || cfg.Forge.Type != "github" || cfg.DeployRepo != "ambrdk/ansible" {
		t.Errorf("top level = %+v", cfg)
	}
	if !reflect.DeepEqual(cfg.Environments, []string{"prod"}) {
		t.Errorf("environments = %v", cfg.Environments)
	}
	want := config.Release{
		Versioning:       "conventional",
		TagFormat:        "v{version}",
		BumpBranchPrefix: "cutship/",
		BumpTitle:        "chore(apps): bump {app} to {tag}",
	}
	if cfg.Release != want {
		t.Errorf("release = %+v, want %+v", cfg.Release, want)
	}
	if cfg.Lanes.RequiredCheck != "lint" {
		t.Errorf("required check = %q", cfg.Lanes.RequiredCheck)
	}
	if len(cfg.Notify) != 0 {
		t.Errorf("notify = %v", cfg.Notify)
	}
	app, ok := cfg.App("alert-relay")
	if !ok || app.Image != "ghcr.io/ambrdk/alert-relay" || app.Pin.Preset != "ansible" {
		t.Errorf("App(alert-relay) = %+v, %v", app, ok)
	}
	if _, ok := cfg.App("api"); ok {
		t.Error("App(api) found")
	}
}

func TestParseFull(t *testing.T) {
	cfg, problems := load(t, "full.yaml")
	clean(t, problems)
	if cfg.Forge.URL != "https://api.github.com" {
		t.Errorf("forge url = %q", cfg.Forge.URL)
	}
	if len(cfg.Apps) != 2 || cfg.Apps[1].Image != "registry.example.com:5000/acme/api" {
		t.Errorf("apps = %+v", cfg.Apps)
	}
	if cfg.Release.BumpBranchPrefix != "bump/" {
		t.Errorf("bump prefix = %q", cfg.Release.BumpBranchPrefix)
	}
	want := []config.Notifier{{Type: "telegram", Mode: "channel", ChatID: "-100123"}}
	if !reflect.DeepEqual(cfg.Notify, want) {
		t.Errorf("notify = %+v", cfg.Notify)
	}
}

func TestParseTelegramModeDefault(t *testing.T) {
	cfg, problems := config.Parse([]byte(base))
	clean(t, problems)
	if cfg.Notify[0].Mode != "channel" {
		t.Errorf("mode = %q", cfg.Notify[0].Mode)
	}
}

func TestParseUnknownField(t *testing.T) {
	_, problems := load(t, "unknown_field.yaml")
	if len(problems) != 2 {
		t.Fatalf("problems = %v", problems)
	}
	if line := expect(t, problems, "environment", `unknown field "environment" at line 5`); line != 5 {
		t.Errorf("line = %d", line)
	}
	if line := expect(t, problems, "apps[0].pin.flie", `unknown field "flie"`); line != 9 {
		t.Errorf("line = %d", line)
	}
}

func TestParseBroken(t *testing.T) {
	for name, data := range map[string]string{
		"empty":    "",
		"list":     "- a\n- b\n",
		"syntax":   "version: 1\nforge: [\n",
		"type":     strings.Replace(base, "version: 1", "version: one", 1),
		"apps map": strings.Replace(base, "apps:\n", "apps: {}\nx:\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, problems := config.Parse([]byte(data)); len(problems) == 0 {
				t.Error("no problems")
			}
		})
	}
	_, problems := config.Parse([]byte(strings.Replace(base, "version: 1", "version: one", 1)))
	if line := expect(t, problems, "version", "cannot unmarshal"); line != 1 {
		t.Errorf("line = %d", line)
	}
}

func TestLoadMissing(t *testing.T) {
	_, problems := config.Load(filepath.Join(t.TempDir(), "cutship.yaml"))
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "cutship.yaml") {
		t.Errorf("problems = %v", problems)
	}
}

func TestValidateDuplicates(t *testing.T) {
	data := `version: 1
forge: { type: github }
deploy_repo: ambrdk/ansible
environments: [prod, staging, prod]
apps:
  - name: alert-relay
    image: ghcr.io/ambrdk/alert-relay
    pin: { preset: ansible }
  - name: api
    image: ghcr.io/ambrdk/api
    pin: { preset: ansible }
  - name: alert-relay
    image: ghcr.io/ambrdk/api
    pin: { preset: ansible }
`
	_, problems := config.Parse([]byte(data))
	if line := expect(t, problems, "apps[2].name", "apps[0]"); line != 12 {
		t.Errorf("line = %d", line)
	}
	if line := expect(t, problems, "apps[2].image", "apps[1]"); line != 13 {
		t.Errorf("line = %d", line)
	}
	expect(t, problems, "environments[2]", "environments[0]")
	if len(problems) != 3 {
		t.Errorf("problems = %v", problems)
	}
}

func TestValidateTagFormat(t *testing.T) {
	for format, ok := range map[string]bool{
		"v{version}":                           true,
		"{version}":                            true,
		"release-{version}":                    true,
		"{version}_final":                      true,
		"release":                              false,
		"v{version}-{version}":                 false,
		"v {version}":                          false,
		"-{version}":                           false,
		".{version}":                           false,
		"v{version}/x":                         false,
		"v{version}.lock":                      false,
		"v..{version}":                         false,
		"{{version}}":                          false,
		"v{major}.{version}":                   false,
		strings.Repeat("v", 130) + "{version}": false,
	} {
		t.Run(format, func(t *testing.T) {
			data := base + "release:\n  tag_format: \"" + format + "\"\n"
			_, problems := config.Parse([]byte(data))
			_, found := find(problems, "release.tag_format")
			if found == ok {
				t.Errorf("tag_format %q: problems %v", format, problems)
			}
		})
	}
}

func TestValidateUnsupported(t *testing.T) {
	for name, c := range map[string]struct{ from, to, path string }{
		"gitlab":     {"type: github", "type: gitlab", "forge.type"},
		"slack":      {"type: telegram\n    chat_id: \"-100123\"", "type: slack\n    channel: C0123", "notify[0].type"},
		"topics":     {"type: telegram", "type: telegram\n    mode: topics\n    topic_id: 42", "notify[0].mode"},
		"helm":       {"preset: ansible", "preset: helm", "apps[0].pin.preset"},
		"kustomize":  {"preset: ansible", "preset: kustomize", "apps[0].pin.preset"},
		"compose":    {"preset: ansible", "preset: compose", "apps[0].pin.preset"},
		"file path":  {"preset: ansible", "file: values.yaml, path: image.tag", "apps[0].pin"},
		"tag prefix": {"    pin:", "    tag_prefix: api-\n    pin:", "apps[0].tag_prefix"},
	} {
		t.Run(name, func(t *testing.T) {
			data := strings.Replace(base, c.from, c.to, 1)
			if data == base {
				t.Fatal("replacement did not apply")
			}
			_, problems := config.Parse([]byte(data))
			expect(t, problems, c.path, "not supported", "planned")
			if len(problems) != 1 {
				t.Errorf("problems = %v", problems)
			}
		})
	}
}

func TestValidateNames(t *testing.T) {
	for name, c := range map[string]struct{ from, to, path string }{
		"app upper":       {"name: alert-relay", "name: Alert-Relay", "apps[0].name"},
		"app underscore":  {"name: alert-relay", "name: alert_relay", "apps[0].name"},
		"app dash first":  {"name: alert-relay", "name: -alert", "apps[0].name"},
		"app empty":       {"name: alert-relay", "name: \"\"", "apps[0].name"},
		"env upper":       {"apps:", "environments: [Prod]\napps:", "environments[0]"},
		"deploy repo":     {"deploy_repo: ambrdk/ansible", "deploy_repo: ansible", "deploy_repo"},
		"deploy repo url": {"deploy_repo: ambrdk/ansible", "deploy_repo: https://github.com/ambrdk/ansible", "deploy_repo"},
		"image tag":       {"image: ghcr.io/ambrdk/alert-relay", "image: ghcr.io/ambrdk/alert-relay:v1.0.0", "apps[0].image"},
		"image digest":    {"image: ghcr.io/ambrdk/alert-relay", "image: ghcr.io/ambrdk/alert-relay@sha256:abc", "apps[0].image"},
		"image upper":     {"image: ghcr.io/ambrdk/alert-relay", "image: ghcr.io/ambrdk/Alert", "apps[0].image"},
		"image empty":     {"image: ghcr.io/ambrdk/alert-relay", "image: \"\"", "apps[0].image"},
	} {
		t.Run(name, func(t *testing.T) {
			data := strings.Replace(base, c.from, c.to, 1)
			if data == base {
				t.Fatal("replacement did not apply")
			}
			_, problems := config.Parse([]byte(data))
			expect(t, problems, c.path)
			if len(problems) != 1 {
				t.Errorf("problems = %v", problems)
			}
		})
	}
}

func TestValidateRules(t *testing.T) {
	for name, c := range map[string]struct{ from, to, path string }{
		"version 2":         {"version: 1", "version: 2", "version"},
		"version missing":   {"version: 1\n", "", "version"},
		"forge missing":     {"forge:\n  type: github\n", "", "forge.type"},
		"forge unknown":     {"type: github", "type: bitbucket", "forge.type"},
		"forge url":         {"type: github", "type: github\n  url: api.github.com", "forge.url"},
		"no apps":           {"apps:\n  - name: alert-relay\n    image: ghcr.io/ambrdk/alert-relay\n    pin: { preset: ansible }\n", "", "apps"},
		"no pin":            {"    pin: { preset: ansible }\n", "", "apps[0].pin"},
		"preset and file":   {"preset: ansible", "preset: ansible, file: a.yml, path: image", "apps[0].pin"},
		"file without path": {"preset: ansible", "file: a.yml", "apps[0].pin"},
		"preset unknown":    {"preset: ansible", "preset: chef", "apps[0].pin.preset"},
		"versioning":        {"notify:", "release:\n  versioning: semver\nnotify:", "release.versioning"},
		"bump prefix slash": {"notify:", "release:\n  bump_branch_prefix: bump\nnotify:", "release.bump_branch_prefix"},
		"bump prefix dots":  {"notify:", "release:\n  bump_branch_prefix: a..b/\nnotify:", "release.bump_branch_prefix"},
		"bump prefix dot":   {"notify:", "release:\n  bump_branch_prefix: .bump/\nnotify:", "release.bump_branch_prefix"},
		"bump title tag":    {"notify:", "release:\n  bump_title: bump {app}\nnotify:", "release.bump_title"},
		"bump title app":    {"notify:", "release:\n  bump_title: bump to {tag}\nnotify:", "release.bump_title"},
		"chat id":           {"    chat_id: \"-100123\"\n", "", "notify[0].chat_id"},
		"notify type":       {"type: telegram", "type: discord", "notify[0].type"},
		"notify no type":    {"  - type: telegram\n    chat_id", "  - chat_id", "notify[0].type"},
		"mode unknown":      {"type: telegram", "type: telegram\n    mode: group", "notify[0].mode"},
		"topic in channel":  {"type: telegram", "type: telegram\n    topic_id: 42", "notify[0].topic_id"},
		"slack channel":     {"type: telegram", "type: telegram\n    channel: C0123", "notify[0].channel"},
	} {
		t.Run(name, func(t *testing.T) {
			data := strings.Replace(base, c.from, c.to, 1)
			if data == base {
				t.Fatal("replacement did not apply")
			}
			_, problems := config.Parse([]byte(data))
			expect(t, problems, c.path)
			if len(problems) != 1 {
				t.Errorf("problems = %v", problems)
			}
		})
	}
}

func TestReleaseTagRoundTrip(t *testing.T) {
	v := config.Release{TagFormat: "v{version}"}
	plain := config.Release{TagFormat: "{version}"}
	if got := v.Tag("1.2.3"); got != "v1.2.3" {
		t.Errorf("Tag = %q", got)
	}
	if got := plain.Tag("1.2.3"); got != "1.2.3" {
		t.Errorf("Tag = %q", got)
	}
	if got := (config.Release{}).Tag("1.2.3"); got != "v1.2.3" {
		t.Errorf("zero Release Tag = %q", got)
	}
	for _, c := range []struct {
		rel  config.Release
		tag  string
		want string
		ok   bool
	}{
		{v, "v1.2.3", "1.2.3", true},
		{v, "v10.0.20", "10.0.20", true},
		{v, "1.2.3", "", false},
		{v, "v1", "", false},
		{v, "v1.2", "", false},
		{v, "v1.2.3-rc.1", "", false},
		{v, "v01.2.3", "", false},
		{v, "latest", "", false},
		{plain, "1.2.3", "1.2.3", true},
		{plain, "v1.2.3", "", false},
		{config.Release{TagFormat: "release-{version}-final"}, "release-0.1.0-final", "0.1.0", true},
		{config.Release{TagFormat: "release-{version}-final"}, "release-0.1.0", "", false},
	} {
		got, ok := c.rel.Version(c.tag)
		if got != c.want || ok != c.ok {
			t.Errorf("%q.Version(%q) = %q, %v; want %q, %v", c.rel.TagFormat, c.tag, got, ok, c.want, c.ok)
		}
	}
}
