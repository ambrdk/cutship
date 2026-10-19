package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ambrdk/cutship/gate"
)

func readJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := json.NewDecoder(f).Decode(v); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

func parsePending(text string) []string {
	var pending []string
	if err := json.Unmarshal([]byte(text), &pending); err != nil || pending == nil {
		return nil
	}
	return pending
}

func gateCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "head" && args[0] != "skip") {
		return usageError(stderr)
	}
	fs := flag.NewFlagSet("gate "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	pullsPath := fs.String("pulls", "", "")
	checksPath := fs.String("checks", "", "")
	pending := fs.String("pending", "", "")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *pullsPath == "" {
		return usageError(stderr)
	}
	if args[0] == "skip" && (*checksPath == "" || *pending == "") {
		return usageError(stderr)
	}
	var pulls []gate.Pull
	if err := readJSON(*pullsPath, &pulls); err != nil {
		return fail(stderr, err)
	}
	if args[0] == "head" {
		_, _ = fmt.Fprintln(stdout, gate.Head(pulls))
		return 0
	}
	var pages []struct {
		CheckRuns []gate.CheckRun `json:"check_runs"`
	}
	if err := readJSON(*checksPath, &pages); err != nil {
		return fail(stderr, err)
	}
	var runs []gate.CheckRun
	for _, p := range pages {
		runs = append(runs, p.CheckRuns...)
	}
	_, _ = fmt.Fprintln(stdout, gate.Skip(pulls, runs, parsePending(*pending)))
	return 0
}
