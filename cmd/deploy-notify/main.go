package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ambrdk/deploy-notify/github"
	"github.com/ambrdk/deploy-notify/notify"
	"github.com/ambrdk/deploy-notify/source"
	"github.com/ambrdk/deploy-notify/store"
	"github.com/ambrdk/notifykit/telegram"
	"github.com/ambrdk/notifykit/thread"
)

const copyWait = 30 * time.Second

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(log)
	if len(os.Args) != 2 || os.Args[1] != "handle" {
		fmt.Fprintln(os.Stderr, "usage: deploy-notify handle")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, log); err != nil {
		log.Error("deploy-notify failed", "error", err)
		os.Exit(1)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func run(ctx context.Context, log *slog.Logger) error {
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
	events, err := source.Parse(ctx, api, repo, eventName, payload)
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
