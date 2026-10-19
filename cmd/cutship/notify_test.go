package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/forge/fake"
)

func TestNotifyCLI(t *testing.T) {
	f := fake.New("bot")
	f.Commit("ambrdk/ansible", "main", map[string][]byte{
		"cutship.yaml":             []byte(ambrdkConfig),
		"apps/alert-relay/app.yml": []byte("image: " + own + ":v0.3.0\n"),
	})
	ch, err := f.PushChange(t.Context(), "ambrdk/ansible", forge.ChangeRequest{Base: "main", Branch: "bump/alert-relay-v0.3.1", Title: "bump", Files: map[string][]byte{"apps/alert-relay/app.yml": []byte("image: " + own + ":v0.3.1\n")}})
	if err != nil {
		t.Fatal(err)
	}
	useForge(t, f)
	payload := filepath.Join(t.TempDir(), "event.json")
	body := fmt.Sprintf(`{"action":"closed","pull_request":{"number":%d,"title":"bump","merged":false,"user":{"login":"bot"},"head":{"ref":"bump/alert-relay-v0.3.1","sha":%q},"base":{"sha":"x"}}}`, ch.Number, ch.HeadSHA)
	if err := os.WriteFile(payload, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_REPOSITORY", "ambrdk/ansible")
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_EVENT_PATH", payload)
	t.Setenv("GITHUB_TOKEN", "token")
	if code, out, errOut := run(t, "", "notify"); code != 0 || out != "" {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	comments := f.CommentsOf("ambrdk/ansible", ch.Number)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, `"stage":"closed"`) {
		t.Fatalf("comments %+v", comments)
	}
	t.Setenv("GITHUB_EVENT_PATH", "")
	if code, _, errOut := run(t, "", "notify"); code != 1 || !strings.Contains(errOut, "GITHUB_EVENT_PATH") {
		t.Fatalf("missing env: %d %q", code, errOut)
	}
	if code, _, _ := run(t, "", "notify", "extra"); code != 2 {
		t.Fatalf("extra arg: %d", code)
	}
}
