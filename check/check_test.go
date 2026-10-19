package check

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/forge/fake"
	"github.com/ambrdk/cutship/notifier/telegram"
	"github.com/ambrdk/cutship/registry"
)

const (
	deployRepo = "ambrdk/ansible"
	cfgY       = `version: 1
forge: { type: github }
deploy_repo: ambrdk/ansible
apps:
  - name: alert-relay
    image: ghcr.io/ambrdk/alert-relay
    pin: { preset: ansible }
release:
  bump_branch_prefix: bump/
`
)

var ctx = context.Background()

type images map[string]error

func (m images) Revision(_ context.Context, img, tag string) (string, error) {
	err, ok := m[img+":"+tag]
	if !ok {
		return "", fmt.Errorf("%s:%s: %w", img, tag, registry.ErrMissing)
	}
	return "abc", err
}

func yes() *bool { v := true; return &v }
func no() *bool  { v := false; return &v }

func world(t *testing.T) (*fake.Forge, images, config.Config) {
	t.Helper()
	cfg, problems := config.Parse([]byte(cfgY))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	f := fake.New("cutship-bot")
	f.Commit(deployRepo, "main", map[string][]byte{
		"cutship.yaml":             []byte(cfgY),
		"apps/alert-relay/app.yml": []byte("image: ghcr.io/ambrdk/alert-relay:v0.3.0\n"),
	})
	f.SetSettings(deployRepo, forge.RepoSettings{Push: yes(), AutoMerge: yes(), DeleteBranchOnMerge: yes()})
	return f, images{"ghcr.io/ambrdk/alert-relay:v0.3.0": nil}, cfg
}

func levels(findings []Finding) string {
	var out []string
	for _, f := range findings {
		out = append(out, f.Level+" "+f.Path+": "+f.Message)
	}
	return strings.Join(out, "\n")
}

func expect(t *testing.T, findings []Finding, level, word string) {
	t.Helper()
	for _, f := range findings {
		if f.Level == level && strings.Contains(f.Path+": "+f.Message, word) {
			return
		}
	}
	t.Fatalf("no %s mentioning %q in:\n%s", level, word, levels(findings))
}

func TestOnlineClean(t *testing.T) {
	f, reg, cfg := world(t)
	if got := Online(ctx, f, reg, cfg, []byte(cfgY)); len(got) != 0 {
		t.Fatalf("findings:\n%s", levels(got))
	}
}

func TestOnlineLocalConfigDiffers(t *testing.T) {
	f, reg, cfg := world(t)
	expect(t, Online(ctx, f, reg, cfg, []byte(cfgY+"\n")), Warning, "differs")
}

func TestOnlineActionsTokenWarns(t *testing.T) {
	f, reg, cfg := world(t)
	f.User = "github-actions[bot]"
	expect(t, Online(ctx, f, reg, cfg, []byte(cfgY)), Warning, "GITHUB_TOKEN")
}

func TestOnlineNoPush(t *testing.T) {
	f, reg, cfg := world(t)
	f.SetSettings(deployRepo, forge.RepoSettings{Push: no(), AutoMerge: yes(), DeleteBranchOnMerge: yes()})
	expect(t, Online(ctx, f, reg, cfg, []byte(cfgY)), Error, "push")
}

func TestOnlineAutoMergeOff(t *testing.T) {
	f, reg, cfg := world(t)
	f.SetSettings(deployRepo, forge.RepoSettings{Push: yes(), AutoMerge: no()})
	got := Online(ctx, f, reg, cfg, []byte(cfgY))
	expect(t, got, Warning, "auto-merge")
	expect(t, got, Warning, "delete")
}

func TestOnlineImageMissing(t *testing.T) {
	f, _, cfg := world(t)
	expect(t, Online(ctx, f, images{}, cfg, []byte(cfgY)), Error, "ghcr.io/ambrdk/alert-relay:v0.3.0")
	expect(t, Online(ctx, f, images{"ghcr.io/ambrdk/alert-relay:v0.3.0": errors.New("denied")}, cfg, []byte(cfgY)), Warning, "denied")
}

func TestOnlineBadPin(t *testing.T) {
	f, reg, cfg := world(t)
	f.Commit(deployRepo, "main", map[string][]byte{"apps/alert-relay/app.yml": []byte("image: ghcr.io/ambrdk/alert-relay:latest\n")})
	expect(t, Online(ctx, f, reg, cfg, []byte(cfgY)), Error, "apps[0]")
	f.Commit(deployRepo, "main", map[string][]byte{"apps/alert-relay/app.yml": nil})
	expect(t, Online(ctx, f, reg, cfg, []byte(cfgY)), Error, "apps/alert-relay/app.yml")
}

func TestOnlineRepoUnreadable(t *testing.T) {
	f, reg, cfg := world(t)
	cfg.DeployRepo = "ambrdk/missing"
	got := Online(ctx, f, reg, cfg, []byte(cfgY))
	if len(got) != 1 || got[0].Level != Error || got[0].Path != "deploy_repo" {
		t.Fatalf("findings:\n%s", levels(got))
	}
}

func TestOnlineTelegram(t *testing.T) {
	f, reg, cfg := world(t)
	cfg.Notify = []config.Notifier{{Type: "telegram", Mode: "channel", ChatID: "-100123"}}
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	expect(t, Online(ctx, f, reg, cfg, []byte(cfgY)), Error, "notify[0]: TELEGRAM_BOT_TOKEN")
	t.Setenv("TELEGRAM_BOT_TOKEN", "1:x")
	t.Setenv("TELEGRAM_API_URL", "http://bot.test")
	old := telegramCheck
	t.Cleanup(func() { telegramCheck = old })
	var gotURL, gotChat string
	telegramCheck = func(_ context.Context, token, url, chat string) []telegram.Problem {
		gotURL, gotChat = url, chat
		return []telegram.Problem{{Error: true, Message: "not an admin"}, {Message: "no discussion group"}}
	}
	got := Online(ctx, f, reg, cfg, []byte(cfgY))
	expect(t, got, Error, "notify[0]: not an admin")
	expect(t, got, Warning, "notify[0]: no discussion group")
	if gotURL != "http://bot.test" || gotChat != "-100123" {
		t.Fatalf("%q %q", gotURL, gotChat)
	}
}

type readerOnly struct{ forge.Forge }

func TestOnlineNoDeployments(t *testing.T) {
	f, reg, cfg := world(t)
	expect(t, Online(ctx, readerOnly{f}, reg, cfg, []byte(cfgY)), Error, "deployments")
}
