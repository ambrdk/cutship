package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/ambrdk/cutship/version"
)

func versionCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "next" {
		return usageError(stderr)
	}
	fs := flag.NewFlagSet("version next", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("config", "cutship.yaml", "")
	app := fs.String("app", "", "")
	dir := fs.String("dir", ".", "")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *app == "" {
		return usageError(stderr)
	}
	cfg, ok := loadConfig(*path, stderr)
	if !ok {
		return 1
	}
	if _, ok := cfg.App(*app); !ok {
		return fail(stderr, fmt.Errorf("app %q is not in %s", *app, *path))
	}
	tag, warnings, err := version.Resolve(*dir, cfg.Release)
	for _, w := range warnings {
		_, _ = fmt.Fprintf(stderr, "::warning::%s\n", w)
	}
	if err != nil {
		return fail(stderr, err)
	}
	if tag != "" {
		_, _ = fmt.Fprintln(stdout, tag)
	}
	return 0
}
