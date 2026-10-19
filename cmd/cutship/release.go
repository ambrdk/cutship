package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/cut"
	"github.com/ambrdk/cutship/notifier"
	"github.com/ambrdk/cutship/notifier/notifiers"
	"github.com/ambrdk/cutship/registry"
)

var newRegistry = registry.New

var openNotifiers = notifiers.FromConfig

func notifiersFor(stderr io.Writer) func(config.Config) ([]notifier.Notifier, error) {
	log := slog.New(slog.NewTextHandler(stderr, nil))
	return func(cfg config.Config) ([]notifier.Notifier, error) {
		return openNotifiers(cfg, os.Getenv, log)
	}
}

func reportError(stderr io.Writer, err error) int {
	var cfgErr *cut.ConfigError
	if errors.As(err, &cfgErr) {
		for _, p := range cfgErr.Problems {
			reportProblem(stderr, cfgErr.File, p)
		}
		return 1
	}
	return fail(stderr, err)
}

func releaseCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	app := fs.String("app", "", "")
	tag := fs.String("tag", "", "")
	deployRepo := fs.String("deploy-repo", "", "")
	sha := fs.String("sha", os.Getenv("GITHUB_SHA"), "")
	serviceRepo := fs.String("repo", os.Getenv("GITHUB_REPOSITORY"), "")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *app == "" || *tag == "" || *deployRepo == "" || *sha == "" || *serviceRepo == "" {
		return usageError(stderr)
	}
	deployToken := os.Getenv("CUTSHIP_DEPLOY_TOKEN")
	if deployToken == "" {
		return fail(stderr, errors.New("CUTSHIP_DEPLOY_TOKEN is required: the GITHUB_TOKEN of the service cannot open a bump in "+*deployRepo+", and a bump it opened would not start CI"))
	}
	service, err := openForge(config.Config{}, os.Getenv("GITHUB_TOKEN"))
	if err != nil {
		return fail(stderr, err)
	}
	deploy, err := openForge(config.Config{}, deployToken)
	if err != nil {
		return fail(stderr, err)
	}
	res, err := cut.Release(context.Background(), service, deploy, newRegistry(), cut.Input{
		App:         *app,
		Tag:         *tag,
		SHA:         *sha,
		ServiceRepo: *serviceRepo,
		DeployRepo:  *deployRepo,
		Now:         time.Now().UTC(),
		Notifiers:   notifiersFor(stderr),
	})
	for _, w := range res.Warnings {
		_, _ = fmt.Fprintf(stderr, "::warning::%s\n", w)
	}
	if err != nil {
		return reportError(stderr, err)
	}
	_, _ = fmt.Fprintf(stdout, "outcome=%s\ntag=%s\n", res.Outcome, *tag)
	if res.Change.Number != 0 {
		_, _ = fmt.Fprintf(stdout, "pr=%d\nurl=%s\n", res.Change.Number, res.Change.URL)
	}
	return 0
}
