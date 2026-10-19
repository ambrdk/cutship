package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/pin"
)

const root = "../.."

func TestConfigReferenceUpToDate(t *testing.T) {
	want := reference()
	got, err := os.ReadFile(filepath.Join(root, "docs", "config.md"))
	if err != nil || string(got) != want {
		t.Fatalf("docs/config.md is out of date, run: go run ./internal/docgen > docs/config.md (%v)", err)
	}
	for _, field := range []string{"`apps[].pin.preset`", "`release.tag_format`", "`notify[].chat_id`", "`lanes.ci_workflow`"} {
		if !strings.Contains(want, field) {
			t.Errorf("reference lacks %s", field)
		}
	}
}

func TestExamplesValidate(t *testing.T) {
	found := 0
	err := filepath.WalkDir(filepath.Join(root, "examples"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.Name() != "cutship.yaml" {
			return err
		}
		found++
		cfg, problems := config.Load(path)
		for _, p := range problems {
			t.Errorf("%s: %v", path, p)
		}
		for _, err := range pin.Verify(filepath.Dir(path), cfg) {
			t.Errorf("%s: %v", path, err)
		}
		return nil
	})
	if err != nil || found == 0 {
		t.Fatalf("examples: %d configs, %v", found, err)
	}
}

func TestDocsNoEmDash(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	files = append(files, filepath.Join(root, "README.md"), filepath.Join(root, "CONTEXT.md"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(string(data), "—–") {
			t.Errorf("%s has an em or en dash", f)
		}
	}
}
