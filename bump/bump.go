package bump

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/ambrdk/cutship/pin"
)

var (
	branch = regexp.MustCompile(`^bump/([a-z0-9][a-z0-9-]*)-(v\d+\.\d+\.\d+)$`)
	semver = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)
)

func Name(app, tag string) (string, string, error) {
	if !pin.ValidApp(app) || !semver.MatchString(tag) {
		return "", "", fmt.Errorf("bump name: app '%s' must be a valid app name and tag '%s' must be vMAJOR.MINOR.PATCH: %w", app, tag, pin.ErrRule)
	}
	return "bump/" + app + "-" + tag, "chore(apps): bump " + app + " to " + tag, nil
}

func Parse(ref string) (string, string, bool) {
	m := branch.FindStringSubmatch(ref)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

func Superseded(app, tag string, branches []string) []string {
	current, ok := numbers(tag)
	if !ok {
		return nil
	}
	var out []string
	for _, b := range branches {
		other, version, ok := Parse(b)
		if !ok || other != app {
			continue
		}
		if v, _ := numbers(version); less(v, current) {
			out = append(out, b)
		}
	}
	return out
}

func numbers(tag string) ([3]int, bool) {
	m := semver.FindStringSubmatch(tag)
	if m == nil {
		return [3]int{}, false
	}
	var v [3]int
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v, true
}

func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
