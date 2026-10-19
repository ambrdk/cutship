package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `usage: ship <command> [arguments]

commands:
  notify                                   post release and deploy events to Telegram
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
	}
	return usageError(stderr)
}

func usageError(stderr io.Writer) int {
	_, _ = fmt.Fprint(stderr, usage)
	return 2
}
