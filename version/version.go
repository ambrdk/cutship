package version

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/ambrdk/cutship/config"
)

var (
	semver       = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	conventional = regexp.MustCompile(`^([a-z]+)(\([^)]*\))?(!)?: .+`)
)

const (
	none = iota
	patch
	minor
	major
)

func Next(last string, subjects []string) (string, []string) {
	v, ok := parse(last)
	if !ok {
		return "", []string{fmt.Sprintf("last release %q is not MAJOR.MINOR.PATCH, no release", last)}
	}
	var warnings []string
	level := none
	for _, s := range subjects {
		if s == "" {
			continue
		}
		m := conventional.FindStringSubmatch(s)
		if m == nil {
			warnings = append(warnings, "not a conventional commit subject, no release from it: "+s)
			continue
		}
		bump := none
		switch m[1] {
		case "feat":
			bump = minor
		case "fix", "perf":
			bump = patch
		}
		if m[3] != "" {
			bump = major
		}
		level = max(level, bump)
	}
	if level == major && v[0] == 0 {
		level = minor
	}
	switch level {
	case major:
		return fmt.Sprintf("%d.0.0", v[0]+1), warnings
	case minor:
		return fmt.Sprintf("%d.%d.0", v[0], v[1]+1), warnings
	case patch:
		return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]+1), warnings
	}
	return "", warnings
}

func Resolve(dir string, rel config.Release) (string, []string, error) {
	shallow, err := git(dir, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(shallow) == "true" {
		return "", nil, errors.New("the next version needs the full history: check out with fetch-depth: 0")
	}
	head, err := git(dir, "tag", "--points-at", "HEAD")
	if err != nil {
		return "", nil, err
	}
	if tag, _, ok := highest(rel, head); ok {
		return tag, nil, nil
	}
	merged, err := git(dir, "tag", "--merged", "HEAD")
	if err != nil {
		return "", nil, err
	}
	last, revs := "0.0.0", "HEAD"
	if tag, v, ok := highest(rel, merged); ok {
		last, revs = v, "refs/tags/"+tag+"..HEAD"
	}
	log, err := git(dir, "log", "--format=%s", revs)
	if err != nil {
		return "", nil, err
	}
	next, warnings := Next(last, strings.Split(log, "\n"))
	if next == "" {
		return "", warnings, nil
	}
	return rel.Tag(next), warnings, nil
}

func highest(rel config.Release, tags string) (string, string, bool) {
	var bestTag, bestVersion string
	var best [3]int
	for _, tag := range strings.Fields(tags) {
		v, ok := rel.Version(tag)
		if !ok {
			continue
		}
		n, _ := parse(v)
		if bestTag == "" || less(best, n) {
			bestTag, bestVersion, best = tag, v, n
		}
	}
	return bestTag, bestVersion, bestTag != ""
}

func parse(v string) ([3]int, bool) {
	m := semver.FindStringSubmatch(v)
	if m == nil {
		return [3]int{}, false
	}
	var n [3]int
	for i := range n {
		n[i], _ = strconv.Atoi(m[i+1])
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

func git(dir string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
