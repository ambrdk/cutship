package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/cut"
	"github.com/ambrdk/cutship/version"
)

func versionCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "next" {
		return usageError(stderr)
	}
	fs := flag.NewFlagSet("version next", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("config", "cutship.yaml", "")
	deployRepo := fs.String("deploy-repo", "", "")
	app := fs.String("app", "", "")
	dir := fs.String("dir", ".", "")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *app == "" {
		return usageError(stderr)
	}
	configSet := false
	fs.Visit(func(f *flag.Flag) { configSet = configSet || f.Name == "config" })
	if configSet && *deployRepo != "" {
		return usageError(stderr)
	}
	var cfg config.Config
	source := *path
	if *deployRepo != "" {
		token := os.Getenv("CUTSHIP_DEPLOY_TOKEN")
		if token == "" {
			token = os.Getenv("GITHUB_TOKEN")
		}
		f, err := openForge(config.Config{}, token)
		if err != nil {
			return fail(stderr, err)
		}
		if cfg, _, err = cut.LoadConfig(context.Background(), f, *deployRepo); err != nil {
			return reportError(stderr, err)
		}
		source = *deployRepo + ":" + cut.ConfigPath
	} else {
		var ok bool
		if cfg, ok = loadConfig(*path, stderr); !ok {
			return 1
		}
	}
	if _, ok := cfg.App(*app); !ok {
		return fail(stderr, fmt.Errorf("app %q is not in %s", *app, source))
	}
	res, err := version.Resolve(*dir, cfg.Release)
	if err != nil {
		return fail(stderr, err)
	}
	for _, w := range res.Warnings {
		_, _ = fmt.Fprintf(stderr, "::warning::%s\n", w)
	}
	_, _ = fmt.Fprintln(stderr, res.Summary())
	if res.Tag != "" {
		_, _ = fmt.Fprintln(stdout, res.Tag)
	}
	return 0
}
