package main

import (
	"bufio"
	"fmt"
	"io"

	"github.com/ambrdk/cutship/bump"
	"github.com/ambrdk/cutship/pin"
)

var bumpArity = map[string]int{"name": 2, "parse": 1, "superseded": 2}

func bumpCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usageError(stderr)
	}
	arity, known := bumpArity[args[0]]
	path, rest, err := configFlags("bump "+args[0], args[1:])
	if !known || err != nil || len(rest) != arity {
		return usageError(stderr)
	}
	rel, ok := releaseConfig(path, stderr)
	if !ok {
		return 1
	}
	switch args[0] {
	case "name":
		branch, title, err := bump.Name(rel, rest[0], rest[1])
		if err != nil {
			return fail(stderr, err)
		}
		_, _ = fmt.Fprintf(stdout, "branch=%s\ntitle=%s\n", branch, title)
	case "parse":
		if app, tag, ok := bump.Parse(rel, rest[0]); ok {
			_, _ = fmt.Fprintln(stdout, app, tag)
		}
	case "superseded":
		if _, _, err := bump.Name(rel, rest[0], rest[1]); err != nil {
			return fail(stderr, err)
		}
		var branches []string
		sc := bufio.NewScanner(stdin)
		for sc.Scan() {
			branches = append(branches, sc.Text())
		}
		if err := sc.Err(); err != nil {
			return fail(stderr, fmt.Errorf("read branches: %w: %w", err, pin.ErrRule))
		}
		for _, b := range bump.Superseded(rel, rest[0], rest[1], branches) {
			_, _ = fmt.Fprintln(stdout, b)
		}
	}
	return 0
}
