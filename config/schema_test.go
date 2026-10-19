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
