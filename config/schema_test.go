package config_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambrdk/cutship/config"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

func compiled(t *testing.T) *jsonschema.Schema {
	t.Helper()
	data, err := config.Schema()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("cutship.schema.json", doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("cutship.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func instance(t *testing.T, data []byte) any {
	t.Helper()
	var v any
	if err := yaml.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	j, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(j))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func testdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSchemaValidatesExamples(t *testing.T) {
	s := compiled(t)
	for _, name := range []string{"minimal.yaml", "full.yaml"} {
		if err := s.Validate(instance(t, testdata(t, name))); err != nil {
			t.Errorf("%s rejected: %v", name, err)
		}
	}
	rejected := map[string][]byte{
		"unknown_field.yaml": testdata(t, "unknown_field.yaml"),
		"gitlab":             []byte(strings.Replace(base, "type: github", "type: gitlab", 1)),
		"slack":              []byte(strings.Replace(base, "type: telegram", "type: slack", 1)),
		"helm":               []byte(strings.Replace(base, "preset: ansible", "preset: helm", 1)),
		"tag prefix":         []byte(strings.Replace(base, "    pin:", "    tag_prefix: api-\n    pin:", 1)),
		"version 2":          []byte(strings.Replace(base, "version: 1", "version: 2", 1)),
		"no apps":            []byte(strings.Replace(base, "deploy_repo: ambrdk/ansible\n", "deploy_repo: ambrdk/ansible\nx:\n", 1)),
	}
	for name, data := range rejected {
		if err := s.Validate(instance(t, data)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestSchemaAgreesWithParse(t *testing.T) {
	s := compiled(t)
	release := func(field, value string) string {
		return strings.Replace(base, "notify:", "release:\n  "+field+": \""+value+"\"\nnotify:", 1)
	}
	for name, data := range map[string]string{
		"chat id number":     strings.Replace(base, `chat_id: "-100123"`, "chat_id: -100123", 1),
		"chat id empty":      strings.Replace(base, `chat_id: "-100123"`, `chat_id: ""`, 1),
		"mode empty":         strings.Replace(base, "type: telegram", "type: telegram\n    mode: \"\"", 1),
		"versioning empty":   release("versioning", ""),
		"tag format empty":   release("tag_format", ""),
		"tag format dash":    release("tag_format", "-{version}"),
		"tag format lock":    release("tag_format", "v{version}.lock"),
		"tag format dots":    release("tag_format", "a..{version}"),
		"tag format slash":   release("tag_format", "v{version}/x"),
		"tag format twice":   release("tag_format", "{version}{version}"),
		"tag format plain":   release("tag_format", "{version}"),
		"tag format long":    release("tag_format", strings.Repeat("v", 130)+"{version}"),
		"prefix empty":       release("bump_branch_prefix", ""),
		"prefix dots":        release("bump_branch_prefix", "a..b/"),
		"prefix lock":        release("bump_branch_prefix", "a.lock/"),
		"prefix nested":      release("bump_branch_prefix", "deploy/bumps/"),
		"prefix no slash":    release("bump_branch_prefix", "bump"),
		"title empty":        release("bump_title", ""),
		"title no tag":       release("bump_title", "bump {app}"),
		"title ok":           release("bump_title", "{app}: {tag}"),
		"forge url ftp":      strings.Replace(base, "type: github", "type: github\n  url: ftp://x", 1),
		"forge url https":    strings.Replace(base, "type: github", "type: github\n  url: https://ghe.example.com/api/v3", 1),
		"required check":     strings.Replace(base, "notify:", "lanes:\n  required_check: \"\"\nnotify:", 1),
		"image tag":          strings.Replace(base, "image: ghcr.io/ambrdk/alert-relay", "image: ghcr.io/ambrdk/alert-relay:v1", 1),
		"env upper":          strings.Replace(base, "apps:", "environments: [Prod]\napps:", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, problems := config.Parse([]byte(data))
			err := s.Validate(instance(t, []byte(data)))
			if (len(problems) == 0) != (err == nil) {
				t.Errorf("parse problems %v, schema error %v", problems, err)
			}
		})
	}
}

func TestSchemaDescribesFields(t *testing.T) {
	data, err := config.Schema()
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s["$schema"] == nil || s["title"] == nil {
		t.Errorf("schema header: %v %v", s["$schema"], s["title"])
	}
	for _, word := range []string{"TELEGRAM_BOT_TOKEN", "planned", "{version}", "apps/<app>/app.yml"} {
		if !bytes.Contains(data, []byte(word)) {
			t.Errorf("schema does not mention %q", word)
		}
	}
}
