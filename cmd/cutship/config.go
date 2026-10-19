package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ambrdk/cutship/check"
	"github.com/ambrdk/cutship/config"
)

func configFlags(name string, args []string) (string, []string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("config", "cutship.yaml", "")
	if err := fs.Parse(args); err != nil {
		return "", nil, err
	}
	return *path, fs.Args(), nil
}

func validateCommand(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("config", "cutship.yaml", "")
	online := fs.Bool("online", false, "")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return usageError(stderr)
	}
	cfg, ok := loadConfig(*path, stderr)
	if !ok {
		return 1
	}
	if !*online {
		return 0
	}
	local, err := os.ReadFile(*path)
	if err != nil {
		return fail(stderr, err)
	}
	token := os.Getenv("CUTSHIP_DEPLOY_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	f, err := openForge(cfg, token)
	if err != nil {
		return fail(stderr, err)
	}
	code := 0
	for _, finding := range check.Online(context.Background(), f, newRegistry(), cfg, local) {
		text := finding.Message
		if finding.Path != "" {
			text = finding.Path + ": " + text
		}
		_, _ = fmt.Fprintf(stderr, "::%s file=%s::%s\n", finding.Level, *path, text)
		if finding.Level == check.Error {
			code = 1
		}
	}
	return code
}

func loadConfig(path string, stderr io.Writer) (config.Config, bool) {
	cfg, problems := config.Load(path)
	for _, p := range problems {
		reportProblem(stderr, path, p)
	}
	return cfg, len(problems) == 0
}

func reportProblem(stderr io.Writer, file string, p config.Problem) {
	where := "file=" + file
	if p.Line > 0 {
		where += fmt.Sprintf(",line=%d", p.Line)
	}
	text := p.Message
	if p.Path != "" {
		text = p.Path + ": " + text
	}
	_, _ = fmt.Fprintf(stderr, "::error %s::%s\n", where, text)
}

func schemaCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		return usageError(stderr)
	}
	data, err := config.Schema()
	if err != nil {
		return fail(stderr, err)
	}
	_, _ = fmt.Fprintf(stdout, "%s\n", data)
	return 0
}
