package main

import (
	"flag"
	"fmt"
	"io"

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
	path, rest, err := configFlags("validate", args)
	if err != nil || len(rest) != 0 {
		return usageError(stderr)
	}
	if _, ok := loadConfig(path, stderr); !ok {
		return 1
	}
	return 0
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
