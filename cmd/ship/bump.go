package main

import (
	"bufio"
	"fmt"
	"io"

	"github.com/ambrdk/ship/bump"
	"github.com/ambrdk/ship/pin"
)

func bumpCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 3 && args[0] == "name":
		branch, title, err := bump.Name(args[1], args[2])
		if err != nil {
			return fail(stderr, err)
		}
		_, _ = fmt.Fprintf(stdout, "branch=%s\ntitle=%s\n", branch, title)
	case len(args) == 2 && args[0] == "parse":
		if app, tag, ok := bump.Parse(args[1]); ok {
			_, _ = fmt.Fprintln(stdout, app, tag)
		}
	case len(args) == 3 && args[0] == "superseded":
		if _, _, err := bump.Name(args[1], args[2]); err != nil {
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
		for _, b := range bump.Superseded(args[1], args[2], branches) {
			_, _ = fmt.Fprintln(stdout, b)
		}
	default:
		return usageError(stderr)
	}
	return 0
}
