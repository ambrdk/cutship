package bump

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/pin"
)

func Name(rel config.Release, app, tag string) (string, string, error) {
	if !pin.ValidApp(app) {
		return "", "", fmt.Errorf("bump name: app '%s' must be a valid app name: %w", app, pin.ErrRule)
	}
	if _, ok := rel.Version(tag); !ok {
		return "", "", fmt.Errorf("bump name: tag '%s' must be %s with MAJOR.MINOR.PATCH: %w", tag, rel.Tag("{version}"), pin.ErrRule)
	}
	title := strings.NewReplacer("{app}", app, "{tag}", tag).Replace(rel.BumpTitle)
	return rel.BumpBranchPrefix + app + "-" + tag, title, nil
}

func Parse(rel config.Release, ref string) (string, string, bool) {
	rest, ok := strings.CutPrefix(ref, rel.BumpBranchPrefix)
	if !ok || rel.BumpBranchPrefix == "" {
		return "", "", false
	}
	for i := strings.Index(rest, "-"); i >= 0; {
		app, tag := rest[:i], rest[i+1:]
		if _, ok := rel.Version(tag); ok && pin.ValidApp(app) {
			return app, tag, true
		}
		next := strings.Index(rest[i+1:], "-")
		if next < 0 {
			break
		}
		i += next + 1
	}
	return "", "", false
}

func Superseded(rel config.Release, app, tag string, branches []string) []string {
	current, ok := numbers(rel, tag)
	if !ok {
		return nil
	}
	var out []string
	for _, b := range branches {
		other, version, ok := Parse(rel, b)
		if !ok || other != app {
			continue
		}
		if v, _ := numbers(rel, version); less(v, current) {
			out = append(out, b)
		}
	}
	return out
}

func numbers(rel config.Release, tag string) ([3]int, bool) {
	v, ok := rel.Version(tag)
	if !ok {
		return [3]int{}, false
	}
	var n [3]int
	for i, part := range strings.Split(v, ".") {
		n[i], _ = strconv.Atoi(part)
	}
	return n, true
}

func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
