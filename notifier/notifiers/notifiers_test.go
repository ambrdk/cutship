package notifiers

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/ambrdk/cutship/config"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestFromConfigEmpty(t *testing.T) {
	got, err := FromConfig(config.Config{}, env(nil), quiet)
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}

func TestFromConfigNeedsToken(t *testing.T) {
	cfg := config.Config{Notify: []config.Notifier{{Type: "telegram", Mode: "channel", ChatID: "-100123"}}}
	if _, err := FromConfig(cfg, env(nil), quiet); err == nil || !strings.Contains(err.Error(), "TELEGRAM_BOT_TOKEN") {
		t.Fatalf("err = %v", err)
	}
	got, err := FromConfig(cfg, env(map[string]string{"TELEGRAM_BOT_TOKEN": "1:x", "TELEGRAM_API_URL": "http://127.0.0.1:1"}), quiet)
	if err != nil || len(got) != 1 || got[0].Key() != "telegram:-100123" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestFromConfigRejectsPlanned(t *testing.T) {
	for _, n := range []config.Notifier{{Type: "slack", Channel: "C1"}, {Type: "telegram", Mode: "topics", ChatID: "-1"}} {
		if _, err := FromConfig(config.Config{Notify: []config.Notifier{n}}, env(map[string]string{"TELEGRAM_BOT_TOKEN": "1:x"}), quiet); err == nil {
			t.Errorf("%+v accepted", n)
		}
	}
}
