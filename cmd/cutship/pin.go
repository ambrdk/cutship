package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/pin"
)

func fail(stderr io.Writer, err error) int {
	_, _ = fmt.Fprintf(stderr, "::error::%s\n", err)
	return 1
}

var pinArity = map[string]int{"read": 1, "check": 2, "set": 2, "verify": 0}

func pinCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usageError(stderr)
	}
	arity, known := pinArity[args[0]]
	fs := flag.NewFlagSet("pin "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("config", "cutship.yaml", "")
	root := fs.String("root", ".", "")
	if !known || fs.Parse(args[1:]) != nil || fs.NArg() != arity {
		return usageError(stderr)
	}
	cfg, ok := loadConfig(*path, stderr)
	if !ok {
		return 1
	}
	rest := fs.Args()
	if args[0] == "verify" {
		errs := pin.Verify(*root, cfg)
		for _, err := range errs {
			fail(stderr, err)
		}
		if len(errs) > 0 {
			return 1
		}
		return 0
	}
	app, ok := cfg.App(rest[0])
	if !ok {
		return fail(stderr, fmt.Errorf("app %q is not in %s", rest[0], *path))
	}
	switch args[0] {
	case "read":
		ref, err := pin.Read(*root, app)
		if err != nil {
			return fail(stderr, err)
		}
		_, _ = fmt.Fprintln(stdout, ref)
	case "check":
		current, err := pin.Read(*root, app)
		if err != nil {
			return fail(stderr, err)
		}
		bump, err := pin.Check(app, cfg.Release, current, rest[1])
		if err != nil {
			return fail(stderr, err)
		}
		result := "same"
		if bump {
			result = "bump"
		}
		_, _ = fmt.Fprintln(stdout, result)
	case "set":
		if err := pin.Set(*root, app, cfg.Release, rest[1]); err != nil {
			return fail(stderr, err)
		}
	}
	return 0
}

func laneCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lane", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("config", "cutship.yaml", "")
	pr := fs.Int("pr", 0, "")
	repo := fs.String("repo", os.Getenv("GITHUB_REPOSITORY"), "")
	if fs.Parse(args) != nil || fs.NArg() != 0 || (*pr != 0 && *repo == "") {
		return usageError(stderr)
	}
	cfg, ok := loadConfig(*path, stderr)
	if !ok {
		return 1
	}
	var files []forge.File
	if *pr != 0 {
		f, err := openForge(cfg, os.Getenv("GITHUB_TOKEN"))
		if err != nil {
			return fail(stderr, err)
		}
		if files, err = f.ChangeFiles(context.Background(), *repo, *pr); err != nil {
			return fail(stderr, fmt.Errorf("read files of pull request %d: %w", *pr, err))
		}
	} else {
		var raw []struct {
			Filename string `json:"filename"`
			Status   string `json:"status"`
			Patch    string `json:"patch"`
		}
		if err := json.NewDecoder(stdin).Decode(&raw); err != nil {
			return fail(stderr, fmt.Errorf("read pull request files: %w", err))
		}
		for _, r := range raw {
			files = append(files, forge.File{Path: r.Filename, Status: r.Status, Patch: r.Patch})
		}
	}
	lane := "full"
	if pin.ImageOnly(cfg, files) {
		lane = "fast"
	}
	_, _ = fmt.Fprintln(stdout, lane)
	return 0
}

func releaseConfig(path string, stderr io.Writer) (config.Release, bool) {
	cfg, ok := loadConfig(path, stderr)
	return cfg.Release, ok
}
