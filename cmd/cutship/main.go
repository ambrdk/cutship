package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `usage: cutship <command> [arguments]

Most commands read cutship.yaml, or the file given with --config; release and notify read it
from the deploy repository through the API.

commands:
  validate [--online]                      check cutship.yaml; --online also checks the deploy repository, token and images
  schema                                   print the JSON Schema of cutship.yaml
  version next --app <app> [--dir .]       print the next release tag, nothing when there is none
  release --app <app> --tag <tag> --deploy-repo <owner/name> [--sha sha]
                                           tag the service and open the bump in the deploy repository
  deploy plan --base <sha> [--head sha] [--env env]
                                           print apps=, skip_ci= and change= for a push to the deploy repository
  deploy start --apps <json> [--sha sha] [--env env]
                                           record the deploy of these apps as started
  deploy finish --apps <json> --result success|failure [--report deploy-report.json] [--sha sha] [--env env]
                                           record the result of the deploy
  notify [--repo owner/name]               add a GitHub pull_request or ci workflow_run event to the release post
  pin read [--root dir] <app>              print the pinned image of an app
  pin check [--root dir] <app> <tag>       print bump or same for a release tag
  pin set [--root dir] <app> <tag>         pin an app to a release tag
  pin verify [--root dir]                  check every app pin against the tag rule
  lane < files.json                        print fast or full for a pull request
  bump name <app> <tag>                    print branch= and title= of a bump pull request
  bump parse <ref>                         print "<app> <tag>" for a bump branch
  bump superseded <app> <tag> < branches   print open bump branches with a lower version
  gate head --pulls <file>                 print the head sha of the merged bump pull request
  gate skip --pulls <file> --checks <file> --pending <json|unknown>
                                           print true when the deploy can skip ci
`

func main() {
	os.Exit(dispatch(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func dispatch(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usageError(stderr)
	}
	switch args[0] {
	case "validate":
		return validateCommand(args[1:], stderr)
	case "schema":
		return schemaCommand(args[1:], stdout, stderr)
	case "version":
		return versionCommand(args[1:], stdout, stderr)
	case "release":
		return releaseCommand(args[1:], stdout, stderr)
	case "deploy":
		return deployCommand(args[1:], stdout, stderr)
	case "notify":
		return notifyCommand(args[1:], stderr)
	case "pin":
		return pinCommand(args[1:], stdout, stderr)
	case "lane":
		return laneCommand(args[1:], stdin, stdout, stderr)
	case "bump":
		return bumpCommand(args[1:], stdin, stdout, stderr)
	case "gate":
		return gateCommand(args[1:], stdout, stderr)
	}
	return usageError(stderr)
}

func usageError(stderr io.Writer) int {
	_, _ = fmt.Fprint(stderr, usage)
	return 2
}
