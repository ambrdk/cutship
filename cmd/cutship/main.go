package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `usage: cutship <command> [arguments]

commands:
  notify                                   post release and deploy events to Telegram
  pin read [--root dir] <app>              print the pinned image of an app
  pin check [--root dir] <app> <image> <tag>
                                           print bump or same for a release tag
  pin set [--root dir] <app> <image> <tag> pin an app to a release tag
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
	case "notify":
		if len(args) != 1 {
			return usageError(stderr)
		}
		return notifyCommand(stderr)
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
