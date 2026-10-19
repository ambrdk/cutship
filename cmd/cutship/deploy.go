package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ambrdk/cutship/deploy"
	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/pin"
)

func deployCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "plan" && args[0] != "start" && args[0] != "finish") {
		return usageError(stderr)
	}
	fs := flag.NewFlagSet("deploy "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("config", "cutship.yaml", "")
	repo := fs.String("repo", os.Getenv("GITHUB_REPOSITORY"), "")
	env := fs.String("env", "", "")
	base := fs.String("base", "", "")
	head := fs.String("head", os.Getenv("GITHUB_SHA"), "")
	sha := fs.String("sha", os.Getenv("GITHUB_SHA"), "")
	appsJSON := fs.String("apps", "", "")
	result := fs.String("result", "", "")
	reportPath := fs.String("report", "", "")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || *repo == "" {
		return usageError(stderr)
	}
	switch args[0] {
	case "plan":
		if *base == "" || *head == "" {
			return usageError(stderr)
		}
	case "start":
		if *appsJSON == "" || *sha == "" {
			return usageError(stderr)
		}
	case "finish":
		if *appsJSON == "" || *sha == "" || (*result != "success" && *result != "failure") {
			return usageError(stderr)
		}
	}
	cfg, ok := loadConfig(*path, stderr)
	if !ok {
		return 1
	}
	if *env == "" {
		*env = cfg.Environments[0]
	}
	f, err := openForge(cfg, os.Getenv("GITHUB_TOKEN"))
	if err != nil {
		return fail(stderr, err)
	}
	d, ok := f.(forge.Deployments)
	if !ok {
		return fail(stderr, errors.New("the forge does not support deployments"))
	}
	ctx := context.Background()
	if args[0] == "plan" {
		if strings.Trim(*base, "0") == "" {
			*base = *head
		}
		plan, err := deploy.MakePlan(ctx, f, d, cfg, *repo, *env, *base, *head)
		if err != nil {
			return fail(stderr, err)
		}
		apps, _ := json.Marshal(append([]string{}, plan.Apps...))
		_, _ = fmt.Fprintf(stdout, "apps=%s\nskip_ci=%t\nchange=%d\n", apps, plan.SkipCI, plan.Change)
		return 0
	}
	var apps []string
	if err := json.Unmarshal([]byte(*appsJSON), &apps); err != nil {
		return fail(stderr, fmt.Errorf("--apps must be a JSON array of app names: %w", err))
	}
	for _, a := range apps {
		if !pin.ValidApp(a) {
			return fail(stderr, fmt.Errorf("app name %q in --apps is not valid", a))
		}
	}
	run := deploy.Run{Repo: *repo, Env: *env, SHA: *sha, Now: time.Now().UTC()}
	run.Warn = func(w string) { _, _ = fmt.Fprintf(stderr, "::warning::%s\n", w) }
	if run.Notifiers, err = notifiersFor(stderr)(cfg); err != nil {
		run.Warn("the deploy goes on without a post: " + err.Error())
	}
	run.ID = os.Getenv("GITHUB_RUN_ID") + "-" + os.Getenv("GITHUB_RUN_ATTEMPT")
	run.Attempt, _ = strconv.Atoi(os.Getenv("GITHUB_RUN_ATTEMPT"))
	if server, id := os.Getenv("GITHUB_SERVER_URL"), os.Getenv("GITHUB_RUN_ID"); server != "" && id != "" {
		run.LogURL = server + "/" + *repo + "/actions/runs/" + id
	}
	if args[0] == "start" {
		if err := deploy.Start(ctx, f, d, cfg, run, apps); err != nil {
			return fail(stderr, err)
		}
		return 0
	}
	var report *deploy.Report
	if *reportPath != "" {
		data, err := os.ReadFile(*reportPath)
		if err != nil {
			return fail(stderr, err)
		}
		r, err := deploy.ReadReport(data)
		if err != nil {
			return fail(stderr, err)
		}
		report = &r
	}
	if err := deploy.Finish(ctx, f, d, cfg, run, apps, *result == "success", report); err != nil {
		return fail(stderr, err)
	}
	return 0
}
