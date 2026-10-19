package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/ambrdk/cutship/pin"
)

func fail(stderr io.Writer, err error) int {
	_, _ = fmt.Fprintf(stderr, "::error::%s\n", err)
	return 1
}

func rootFlags(name string, args []string) (string, []string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "")
	if err := fs.Parse(args); err != nil {
		return "", nil, err
	}
	return *root, fs.Args(), nil
}

func pinCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usageError(stderr)
	}
	root, rest, err := rootFlags("pin "+args[0], args[1:])
	if err != nil {
		return usageError(stderr)
	}
	switch {
	case args[0] == "read" && len(rest) == 1:
		ref, err := pin.Read(root, rest[0])
		if err != nil {
			return fail(stderr, err)
		}
		_, _ = fmt.Fprintln(stdout, ref)
	case args[0] == "check" && len(rest) == 3:
		current, err := pin.Read(root, rest[0])
		if err != nil {
			return fail(stderr, err)
		}
		bump, err := pin.Check(current, rest[1], rest[2])
		if err != nil {
			return fail(stderr, err)
		}
		result := "same"
		if bump {
			result = "bump"
		}
		_, _ = fmt.Fprintln(stdout, result)
	case args[0] == "set" && len(rest) == 3:
		if err := pin.Set(root, rest[0], rest[1], rest[2]); err != nil {
			return fail(stderr, err)
		}
	case args[0] == "verify" && len(rest) == 0:
		errs := pin.Verify(root)
		for _, err := range errs {
			fail(stderr, err)
		}
		if len(errs) > 0 {
			return 1
		}
	default:
		return usageError(stderr)
	}
	return 0
}

func laneCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		return usageError(stderr)
	}
	var files []pin.File
	if err := json.NewDecoder(stdin).Decode(&files); err != nil {
		return fail(stderr, fmt.Errorf("read pull request files: %w", err))
	}
	lane := "full"
	if pin.ImageOnly(files) {
		lane = "fast"
	}
	_, _ = fmt.Fprintln(stdout, lane)
	return 0
}
