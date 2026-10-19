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

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/forge"
)

var (
	ErrRule     = errors.New("pin rule")
	appName     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	numeric     = regexp.MustCompile(`^v?(\d+(?:\.\d+)*)`)
	imageLine   = regexp.MustCompile(`^image:[ \t]*(["']?)([^"'\s#]+)(["']?)[ \t]*$`)
	ansibleFile = regexp.MustCompile(`^apps/([a-z0-9][a-z0-9-]*)/app\.yml$`)
)

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

func Path(app config.App) string {
	return "apps/" + app.Name + "/app.yml"
}

func file(root string, app config.App) string {
	return filepath.Join(root, filepath.FromSlash(Path(app)))
}

func Read(root string, app config.App) (string, error) {
	content, err := os.ReadFile(file(root, app))
	if errors.Is(err, os.ErrNotExist) {
		return "", rule("%s not found", Path(app))
	}
	if err != nil {
		return "", err
	}
	ref, ok := Line(content)
	if !ok {
		return "", rule("no top-level image in %s", Path(app))
	}
	return ref, nil
}

func releaseVersion(rel config.Release, tag string) ([]int, bool) {
	v, ok := rel.Version(tag)
	if !ok {
		return nil, false
	}
	return ints(strings.Split(v, ".")), true
}

func foreignVersion(tag string) ([]int, bool) {
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

func ownApp(cfg config.Config, image string) (config.App, bool) {
	for _, a := range cfg.Apps {
		if a.Image == image {
			return a, true
		}
	}
	return config.App{}, false
}

func formatRule(rel config.Release, tag string) error {
	return rule("tag format: '%s' must be %s with MAJOR.MINOR.PATCH", tag, rel.Tag("{version}"))
}

func Check(app config.App, rel config.Release, current, tag string) (bool, error) {
	currentRepo, currentTag := ParseRef(current)
	if currentRepo != app.Image {
		return false, rule("image mismatch: '%s' in %s, '%s' in cutship.yaml", currentRepo, Path(app), app.Image)
	}
	newV, ok := releaseVersion(rel, tag)
	if !ok {
		return false, formatRule(rel, tag)
	}
	oldV, ok := releaseVersion(rel, currentTag)
	if !ok {
		return false, rule("current tag: '%s' in '%s' is not a release tag", currentTag, current)
	}
	switch cmpInts(newV, oldV) {
	case -1:
		return false, rule("tag %s is lower than current %s", tag, currentTag)
	case 0:
		return false, nil
	}
	return true, nil
}

func Set(root string, app config.App, rel config.Release, tag string) error {
	if _, ok := releaseVersion(rel, tag); !ok {
		return formatRule(rel, tag)
	}
	path := file(root, app)
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated, err := Replace(content, app, rel, tag)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, updated, info.Mode().Perm())
}

func Replace(content []byte, app config.App, rel config.Release, tag string) ([]byte, error) {
	if _, ok := releaseVersion(rel, tag); !ok {
		return nil, formatRule(rel, tag)
	}
	lines := strings.SplitAfter(string(content), "\n")
	found := -1
	for i, line := range lines {
		if _, _, ok := parseLine(line); ok {
			if found >= 0 {
				return nil, rule("%s must have exactly one top-level image", Path(app))
			}
			found = i
		}
	}
	if found < 0 {
		return nil, rule("no top-level image in %s", Path(app))
	}
	line := lines[found]
	body := strings.TrimRight(line, "\r\n")
	_, quote, _ := parseLine(line)
	lines[found] = "image: " + quote + app.Image + ":" + tag + quote + line[len(body):]
	return []byte(strings.Join(lines, "")), nil
}

func ImageOnly(cfg config.Config, files []forge.File) bool {
	if len(files) != 1 {
		return false
	}
	f := files[0]
	m := ansibleFile.FindStringSubmatch(f.Path)
	if m == nil || f.Status != "modified" || f.Patch == "" {
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
	version := foreignVersion
	if app, ok := cfg.App(m[1]); ok {
		if app.Image != newRepo {
			return false
		}
		version = func(tag string) ([]int, bool) { return releaseVersion(cfg.Release, tag) }
	} else if _, ok := ownApp(cfg, newRepo); ok {
		return false
	}
	oldV, okOldV := version(oldTag)
	newV, okNewV := version(newTag)
	return okOldV && okNewV && len(oldV) > 0 && len(newV) > 0 && cmpInts(newV, oldV) > 0
}

func Verify(root string, cfg config.Config) []error {
	files, err := filepath.Glob(filepath.Join(root, "apps", "*", "app.yml"))
	if err != nil {
		return []error{err}
	}
	if len(files) == 0 {
		return []error{rule("no apps found under %s", filepath.Join(root, "apps"))}
	}
	sort.Strings(files)
	seen := map[string]bool{}
	var errs []error
	for _, f := range files {
		name := filepath.Base(filepath.Dir(f))
		seen[name] = true
		if !ValidApp(name) {
			errs = append(errs, rule("app name '%s' must match %s", name, appName))
			continue
		}
		app, configured := cfg.App(name)
		if !configured {
			app = config.App{Name: name}
		}
		ref, err := Read(root, app)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := verifyRef(cfg, app, configured, ref); err != nil {
			errs = append(errs, err)
		}
	}
	for _, app := range cfg.Apps {
		if !seen[app.Name] {
			errs = append(errs, rule("%s not found for app %s in cutship.yaml", Path(app), app.Name))
		}
	}
	return errs
}

func verifyRef(cfg config.Config, app config.App, configured bool, ref string) error {
	repo, tag := ParseRef(ref)
	if configured {
		if repo != app.Image {
			return rule("%s: '%s' is not the image %s from cutship.yaml", app.Name, ref, app.Image)
		}
		if _, ok := releaseVersion(cfg.Release, tag); !ok {
			return rule("%s: '%s' is not pinned to a release tag %s", app.Name, ref, cfg.Release.Tag("{version}"))
		}
		return nil
	}
	if owner, ok := ownApp(cfg, repo); ok {
		return rule("%s: pins '%s', the image of app %s", app.Name, ref, owner.Name)
	}
	if _, ok := foreignVersion(tag); !ok {
		return rule("%s: '%s' is not pinned to an exact tag other than latest", app.Name, ref)
	}
	return nil
}
