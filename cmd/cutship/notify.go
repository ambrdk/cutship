package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/cut"
	"github.com/ambrdk/cutship/notify"
	"github.com/ambrdk/cutship/post"
)

func notifyCommand(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("notify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", os.Getenv("GITHUB_REPOSITORY"), "")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return usageError(stderr)
	}
	eventName, eventPath := os.Getenv("GITHUB_EVENT_NAME"), os.Getenv("GITHUB_EVENT_PATH")
	if *repo == "" || eventName == "" || eventPath == "" {
		return fail(stderr, errors.New("GITHUB_REPOSITORY, GITHUB_EVENT_NAME and GITHUB_EVENT_PATH are required"))
	}
	payload, err := os.ReadFile(eventPath)
	if err != nil {
		return fail(stderr, err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	f, err := openForge(config.Config{}, os.Getenv("GITHUB_TOKEN"))
	if err != nil {
		return fail(stderr, err)
	}
	cfg, _, err := cut.LoadConfig(ctx, f, *repo)
	if err != nil {
		return reportError(stderr, err)
	}
	items, err := notify.Parse(ctx, f, cfg, *repo, eventName, payload)
	if err != nil {
		return fail(stderr, err)
	}
	if len(items) == 0 {
		return 0
	}
	warn := func(w string) { _, _ = fmt.Fprintf(stderr, "::warning::%s\n", w) }
	ns, err := notifiersFor(stderr)(cfg)
	if err != nil {
		warn("the event is recorded without a post: " + err.Error())
	}
	if err := notify.Handle(ctx, items, post.New(f, *repo, ns, warn)); err != nil {
		return fail(stderr, err)
	}
	return 0
}
