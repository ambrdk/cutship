package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/github"
	"github.com/ambrdk/cutship/notify"
	"github.com/ambrdk/cutship/source"
	"github.com/ambrdk/cutship/store"
	"github.com/ambrdk/notifykit/telegram"
	"github.com/ambrdk/notifykit/thread"
)

const copyWait = 30 * time.Second

func notifyCommand(args []string, stderr io.Writer) int {
	path, rest, err := configFlags("notify", args)
	if err != nil || len(rest) != 0 {
		return usageError(stderr)
	}
	rel, ok := releaseConfig(path, stderr)
	if !ok {
		return 1
	}
	log := slog.New(slog.NewTextHandler(stderr, nil))
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := runNotify(ctx, log, rel); err != nil {
		log.Error("cutship notify failed", "error", err)
		return 1
	}
	return 0
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func runNotify(ctx context.Context, log *slog.Logger, rel config.Release) error {
	repo, eventName, eventPath := os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GITHUB_EVENT_NAME"), os.Getenv("GITHUB_EVENT_PATH")
	token, channel, ghToken := os.Getenv("TELEGRAM_BOT_TOKEN"), os.Getenv("TELEGRAM_CHANNEL_ID"), os.Getenv("GITHUB_TOKEN")
	if repo == "" || eventName == "" || eventPath == "" || token == "" || channel == "" || ghToken == "" {
		return errors.New("GITHUB_REPOSITORY, GITHUB_EVENT_NAME, GITHUB_EVENT_PATH, GITHUB_TOKEN, TELEGRAM_BOT_TOKEN and TELEGRAM_CHANNEL_ID are required")
	}
	payload, err := os.ReadFile(eventPath)
	if err != nil {
		return err
	}
	api := github.New(env("GITHUB_API_URL", "https://api.github.com"), ghToken)
	events, err := source.Parse(ctx, api, rel, repo, eventName, payload)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		log.Info("nothing to post", "event", eventName)
		return nil
	}
	tg, err := telegram.New(token, os.Getenv("TELEGRAM_API_URL"), log)
	if err != nil {
		return err
	}
	th := thread.New(tg, channel, thread.Waiting(tg, copyWait))
	st := store.NewPRComment(api, repo, func(r store.Record) string { return postLink(channel, r.IDs.PostID) })
	return notify.Handle(ctx, events, st, th, log)
}

func postLink(channel string, postID int64) string {
	internal, ok := strings.CutPrefix(channel, "-100")
	if !ok || postID == 0 {
		return "Telegram release thread"
	}
	return fmt.Sprintf("Telegram release thread: https://t.me/c/%s/%d", internal, postID)
}
