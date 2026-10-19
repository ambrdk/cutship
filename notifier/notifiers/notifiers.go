package notifiers

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/notifier"
	"github.com/ambrdk/cutship/notifier/telegram"
)

func FromConfig(cfg config.Config, env func(string) string, log *slog.Logger) ([]notifier.Notifier, error) {
	var out []notifier.Notifier
	for i, n := range cfg.Notify {
		switch {
		case n.Type == "telegram" && n.Mode == "channel":
			token := env("TELEGRAM_BOT_TOKEN")
			if token == "" {
				return nil, errors.New("TELEGRAM_BOT_TOKEN is required for notify[" + fmt.Sprint(i) + "] of type telegram")
			}
			ch, err := telegram.Open(token, env("TELEGRAM_API_URL"), n.ChatID, log)
			if err != nil {
				return nil, fmt.Errorf("notify[%d]: %w", i, err)
			}
			out = append(out, ch)
		default:
			return nil, fmt.Errorf("notify[%d]: %s in mode %q is not supported yet, planned", i, n.Type, n.Mode)
		}
	}
	return out, nil
}
