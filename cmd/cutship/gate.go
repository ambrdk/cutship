package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ambrdk/cutship/forge"
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
	if len(args) == 0 {
		return usageError(stderr)
	}
	sub := args[0]
	if sub != "head" && sub != "skip" {
		return usageError(stderr)
	}
	fs := flag.NewFlagSet("gate "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("config", "cutship.yaml", "")
	pullsPath := fs.String("pulls", "", "")
	checksPath := fs.String("checks", "", "")
	pending := fs.String("pending", "", "")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *pullsPath == "" {
		return usageError(stderr)
	}
	if sub == "skip" && (*checksPath == "" || *pending == "") {
		return usageError(stderr)
	}
	cfg, ok := loadConfig(*path, stderr)
	if !ok {
		return 1
	}
	var pulls []struct {
		MergedAt *time.Time `json:"merged_at"`
		Head     struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := readJSON(*pullsPath, &pulls); err != nil {
		return fail(stderr, err)
	}
	changes := make([]forge.Change, 0, len(pulls))
	for _, p := range pulls {
		changes = append(changes, forge.Change{HeadRef: p.Head.Ref, HeadSHA: p.Head.SHA, MergedAt: p.MergedAt})
	}
	if sub == "head" {
		merged, _ := gate.Bump(cfg.Release, changes)
		_, _ = fmt.Fprintln(stdout, merged.HeadSHA)
		return 0
	}
	var pages []struct {
		CheckRuns []struct {
			Name        string     `json:"name"`
			Conclusion  string     `json:"conclusion"`
			CompletedAt *time.Time `json:"completed_at"`
		} `json:"check_runs"`
	}
	if err := readJSON(*checksPath, &pages); err != nil {
		return fail(stderr, err)
	}
	var checks []forge.Check
	for _, p := range pages {
		for _, r := range p.CheckRuns {
			c := forge.Check{Name: r.Name, State: forge.Pending}
			if r.CompletedAt != nil {
				c.CompletedAt, c.State = *r.CompletedAt, forge.Failure
				if r.Conclusion == "success" || r.Conclusion == "neutral" || r.Conclusion == "skipped" {
					c.State = forge.Success
				}
			}
			checks = append(checks, c)
		}
	}
	_, _ = fmt.Fprintln(stdout, gate.Skip(cfg.Release, cfg.Lanes, changes, checks, parsePending(*pending)))
	return 0
}
