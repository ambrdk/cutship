package pin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const OwnPrefix = "ghcr.io/ambrdk/"

var (
	ErrRule   = errors.New("pin rule")
	appName   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	semver    = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)
	numeric   = regexp.MustCompile(`^v?(\d+(?:\.\d+)*)`)
	imageLine = regexp.MustCompile(`^image:[ \t]*(["']?)([^"'\s#]+)(["']?)[ \t]*$`)
	appPath   = regexp.MustCompile(`^apps/[a-z0-9][a-z0-9-]*/app\.yml$`)
)

type File struct {
	Filename string `json:"filename"`
	Status   string `json:"status"`
	Patch    string `json:"patch"`
}

type ruleError string

func (e ruleError) Error() string        { return string(e) }
func (e ruleError) Is(target error) bool { return target == ErrRule }

func rule(format string, args ...any) error {
	return ruleError(fmt.Sprintf(format, args...))
}

func ValidApp(name string) bool {
	return appName.MatchString(name)
}

func ParseRef(ref string) (repo, tag string) {
	slash, colon := strings.LastIndex(ref, "/"), strings.LastIndex(ref, ":")
	if colon > slash {
		return ref[:colon], ref[colon+1:]
	}
	return ref, ""
}

func parseLine(line string) (ref, quote string, ok bool) {
	m := imageLine.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
	if m == nil || m[1] != m[3] {
		return "", "", false
	}
	return m[2], m[1], true
}

func Line(content []byte) (string, bool) {
	for _, line := range strings.Split(string(content), "\n") {
		if ref, _, ok := parseLine(line); ok {
			return ref, true
		}
	}
	return "", false
}

func path(root, app string) string {
	return filepath.Join(root, "apps", app, "app.yml")
}

func Read(root, app string) (string, error) {
	content, err := os.ReadFile(path(root, app))
	if errors.Is(err, os.ErrNotExist) {
		return "", rule("apps/%s/app.yml not found", app)
	}
	if err != nil {
		return "", err
	}
	ref, ok := Line(content)
	if !ok {
		return "", rule("no top-level image in apps/%s/app.yml", app)
	}
	return ref, nil
}

func isOwn(repo string) bool {
	return strings.HasPrefix(repo, OwnPrefix)
}

func version(repo, tag string) ([]int, bool) {
	if isOwn(repo) {
		m := semver.FindStringSubmatch(tag)
		if m == nil {
			return nil, false
		}
		return ints(m[1:]), true
	}
	if tag == "" || tag == "latest" {
		return nil, false
	}
	m := numeric.FindStringSubmatch(tag)
	if m == nil {
		return []int{}, true
	}
	return ints(strings.Split(m[1], ".")), true
}

func ints(parts []string) []int {
	out := make([]int, len(parts))
	for i, p := range parts {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

func cmpInts(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

func compare(repo, oldTag, newTag string) int {
	oldV, _ := version(repo, oldTag)
	newV, _ := version(repo, newTag)
	if len(oldV) > 0 && len(newV) > 0 {
		if c := cmpInts(newV, oldV); c != 0 {
			return c
		}
	}
	if oldTag == newTag {
		return 0
	}
	return 1
}

func Check(current, image, tag string) (bool, error) {
	currentRepo, currentTag := ParseRef(current)
	if currentRepo != image {
		return false, rule("image mismatch: '%s' in app.yml, '%s' in release", currentRepo, image)
	}
	if _, ok := version(image, tag); !ok {
		if isOwn(image) {
			return false, rule("tag format: '%s' must be vMAJOR.MINOR.PATCH", tag)
		}
		return false, rule("tag format: '%s' must be an exact tag other than latest", tag)
	}
	if _, ok := version(currentRepo, currentTag); !ok {
		return false, rule("current tag: '%s' in '%s' is not a release tag", currentTag, current)
	}
	switch compare(image, currentTag, tag) {
	case -1:
		return false, rule("tag %s is lower than current %s", tag, currentTag)
	case 0:
		return false, nil
	}
	return true, nil
}

func Set(root, app, image, tag string) error {
	if _, ok := version(image, tag); !ok {
		return rule("tag format: '%s' is not a release tag for %s", tag, image)
	}
	file := path(root, app)
	content, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(content), "\n")
	found := -1
	for i, line := range lines {
		if _, _, ok := parseLine(line); ok {
			if found >= 0 {
				return rule("apps/%s/app.yml must have exactly one top-level image", app)
			}
			found = i
		}
	}
	if found < 0 {
		return rule("no top-level image in apps/%s/app.yml", app)
	}
	line := lines[found]
	body := strings.TrimRight(line, "\r\n")
	_, quote, _ := parseLine(line)
	lines[found] = "image: " + quote + image + ":" + tag + quote + line[len(body):]
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	return os.WriteFile(file, []byte(strings.Join(lines, "")), info.Mode().Perm())
}

func ImageOnly(files []File) bool {
	if len(files) != 1 {
		return false
	}
	f := files[0]
	if !appPath.MatchString(f.Filename) || f.Status != "modified" || f.Patch == "" {
		return false
	}
	var removed, added []string
	for _, line := range strings.Split(f.Patch, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, "-"):
			removed = append(removed, line[1:])
		case strings.HasPrefix(line, "+"):
			added = append(added, line[1:])
		}
	}
	if len(removed) != 1 || len(added) != 1 {
		return false
	}
	oldRef, _, okOld := parseLine(removed[0])
	newRef, _, okNew := parseLine(added[0])
	if !okOld || !okNew {
		return false
	}
	oldRepo, oldTag := ParseRef(oldRef)
	newRepo, newTag := ParseRef(newRef)
	if oldRepo != newRepo {
		return false
	}
	oldV, okOldV := version(oldRepo, oldTag)
	newV, okNewV := version(newRepo, newTag)
	return okOldV && okNewV && len(oldV) > 0 && len(newV) > 0 && cmpInts(newV, oldV) > 0
}

func Verify(root string) []error {
	dirs, err := filepath.Glob(filepath.Join(root, "apps", "*", "app.yml"))
	if err != nil {
		return []error{err}
	}
	if len(dirs) == 0 {
		return []error{rule("no apps found under %s", filepath.Join(root, "apps"))}
	}
	sort.Strings(dirs)
	var errs []error
	for _, file := range dirs {
		app := filepath.Base(filepath.Dir(file))
		if !ValidApp(app) {
			errs = append(errs, rule("app name '%s' must match %s", app, appName))
			continue
		}
		ref, err := Read(root, app)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		repo, tag := ParseRef(ref)
		if _, ok := version(repo, tag); !ok {
			errs = append(errs, rule("%s: '%s' is not pinned to a release tag", app, ref))
		}
	}
	return errs
}
