package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version      int        `yaml:"version" json:"version"`
	Forge        Forge      `yaml:"forge" json:"forge"`
	DeployRepo   string     `yaml:"deploy_repo" json:"deploy_repo"`
	Environments []string   `yaml:"environments,omitempty" json:"environments,omitempty"`
	Apps         []App      `yaml:"apps" json:"apps"`
	Release      Release    `yaml:"release,omitempty" json:"release,omitempty"`
	Lanes        Lanes      `yaml:"lanes,omitempty" json:"lanes,omitempty"`
	Notify       []Notifier `yaml:"notify,omitempty" json:"notify,omitempty"`
}

type Forge struct {
	Type string `yaml:"type" json:"type"`
	URL  string `yaml:"url,omitempty" json:"url,omitempty"`
}

type App struct {
	Name      string `yaml:"name" json:"name"`
	Image     string `yaml:"image" json:"image"`
	TagPrefix string `yaml:"tag_prefix,omitempty" json:"tag_prefix,omitempty"`
	Pin       Pin    `yaml:"pin" json:"pin"`
}

type Pin struct {
	Preset string `yaml:"preset,omitempty" json:"preset,omitempty"`
	File   string `yaml:"file,omitempty" json:"file,omitempty"`
	Path   string `yaml:"path,omitempty" json:"path,omitempty"`
}

type Release struct {
	Versioning       string `yaml:"versioning,omitempty" json:"versioning,omitempty"`
	TagFormat        string `yaml:"tag_format,omitempty" json:"tag_format,omitempty"`
	BumpBranchPrefix string `yaml:"bump_branch_prefix,omitempty" json:"bump_branch_prefix,omitempty"`
	BumpTitle        string `yaml:"bump_title,omitempty" json:"bump_title,omitempty"`
}

type Lanes struct {
	RequiredCheck string `yaml:"required_check,omitempty" json:"required_check,omitempty"`
	CIWorkflow    string `yaml:"ci_workflow,omitempty" json:"ci_workflow,omitempty"`
}

type Notifier struct {
	Type    string `yaml:"type" json:"type"`
	Mode    string `yaml:"mode,omitempty" json:"mode,omitempty"`
	ChatID  string `yaml:"chat_id,omitempty" json:"chat_id,omitempty"`
	TopicID int64  `yaml:"topic_id,omitempty" json:"topic_id,omitempty"`
	Channel string `yaml:"channel,omitempty" json:"channel,omitempty"`
}

type Problem struct {
	Path    string
	Line    int
	Message string
}

func (p Problem) Error() string {
	s := p.Message
	if p.Path != "" {
		s = p.Path + ": " + s
	}
	if p.Line > 0 {
		s += " at line " + strconv.Itoa(p.Line)
	}
	return s
}

const (
	versionPlaceholder = "{version}"
	defaultTagFormat   = "v" + versionPlaceholder
)

var (
	namePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	repoPattern    = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)
	imagePattern   = regexp.MustCompile(`^(?:[a-zA-Z0-9.-]+(?::[0-9]+)?/)?[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*)*$`)
	tagPattern     = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	prefixPattern  = regexp.MustCompile(`^(?:[A-Za-z0-9_-][A-Za-z0-9._-]*/)+$`)
	semverPattern  = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	yamlLine       = regexp.MustCompile(`^(?:yaml: )?line (\d+): (.*)$`)
	plannedPresets = map[string]bool{"helm": true, "kustomize": true, "compose": true}
)

func Load(path string) (Config, []Problem) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, []Problem{{Message: err.Error()}}
	}
	return Parse(data)
}

func Parse(data []byte) (Config, []Problem) {
	var doc yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
		return Config{}, []Problem{{Message: "config is empty"}}
	} else if err != nil {
		return Config{}, yamlProblems(err, nil)
	}
	if len(doc.Content) == 0 {
		return Config{}, []Problem{{Message: "config is empty"}}
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return Config{}, []Problem{{Line: extra.Line, Message: "config must be one YAML document, found another"}}
	} else if !errors.Is(err, io.EOF) {
		return Config{}, yamlProblems(err, nil)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return Config{}, []Problem{{Line: root.Line, Message: "config must be a mapping"}}
	}
	p := parser{lines: map[string]int{}}
	p.walk(root, reflect.TypeOf(Config{}), "")
	var cfg Config
	if err := root.Decode(&cfg); err != nil {
		p.problems = append(p.problems, yamlProblems(err, p.lines)...)
		return cfg, p.problems
	}
	cfg.defaults()
	p.validate(cfg)
	return cfg, p.problems
}

func (c Config) App(name string) (App, bool) {
	for _, a := range c.Apps {
		if a.Name == name {
			return a, true
		}
	}
	return App{}, false
}

func (r Release) Tag(version string) string {
	return strings.Replace(r.format(), versionPlaceholder, version, 1)
}

func (r Release) Version(tag string) (string, bool) {
	prefix, suffix, _ := strings.Cut(r.format(), versionPlaceholder)
	if !strings.HasPrefix(tag, prefix) || !strings.HasSuffix(tag, suffix) || len(tag) < len(prefix)+len(suffix) {
		return "", false
	}
	v := tag[len(prefix) : len(tag)-len(suffix)]
	if !semverPattern.MatchString(v) {
		return "", false
	}
	return v, true
}

func (r Release) format() string {
	if r.TagFormat == "" {
		return defaultTagFormat
	}
	return r.TagFormat
}

func (c *Config) defaults() {
	if len(c.Environments) == 0 {
		c.Environments = []string{"prod"}
	}
	if c.Release.Versioning == "" {
		c.Release.Versioning = "conventional"
	}
	if c.Release.TagFormat == "" {
		c.Release.TagFormat = defaultTagFormat
	}
	if c.Release.BumpBranchPrefix == "" {
		c.Release.BumpBranchPrefix = "cutship/"
	}
	if c.Release.BumpTitle == "" {
		c.Release.BumpTitle = "chore(apps): bump {app} to {tag}"
	}
	if c.Lanes.RequiredCheck == "" {
		c.Lanes.RequiredCheck = "lint"
	}
	if c.Lanes.CIWorkflow == "" {
		c.Lanes.CIWorkflow = "ci"
	}
	for i := range c.Notify {
		if c.Notify[i].Type == "telegram" && c.Notify[i].Mode == "" {
			c.Notify[i].Mode = "channel"
		}
	}
}

type parser struct {
	lines    map[string]int
	problems []Problem
}

func (p *parser) add(path, format string, args ...any) {
	p.problems = append(p.problems, Problem{Path: path, Line: p.line(path), Message: fmt.Sprintf(format, args...)})
}

func (p *parser) line(path string) int {
	for path != "" {
		if n, ok := p.lines[path]; ok {
			return n
		}
		i := strings.LastIndexAny(path, ".[")
		if i < 0 {
			return 0
		}
		path = path[:i]
	}
	return 0
}

func (p *parser) walk(n *yaml.Node, t reflect.Type, path string) {
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	switch t.Kind() {
	case reflect.Struct:
		if n.Kind != yaml.MappingNode {
			return
		}
		fields := yamlFields(t)
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Value == "<<" {
				p.walkMerge(n.Content[i+1], t, path)
				continue
			}
			child := join(path, key.Value)
			f, ok := fields[key.Value]
			if !ok {
				p.problems = append(p.problems, Problem{Path: child, Line: key.Line, Message: fmt.Sprintf("unknown field %q", key.Value)})
				continue
			}
			p.lines[child] = key.Line
			p.walk(n.Content[i+1], f, child)
		}
	case reflect.Slice:
		if n.Kind != yaml.SequenceNode {
			return
		}
		for i, item := range n.Content {
			child := fmt.Sprintf("%s[%d]", path, i)
			p.lines[child] = item.Line
			p.walk(item, t.Elem(), child)
		}
	}
}

func (p *parser) walkMerge(n *yaml.Node, t reflect.Type, path string) {
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	if n.Kind == yaml.SequenceNode {
		for _, item := range n.Content {
			p.walkMerge(item, t, path)
		}
		return
	}
	p.walk(n, t, path)
}

func yamlFields(t reflect.Type) map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		fields[name] = t.Field(i).Type
	}
	return fields
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func yamlProblems(err error, lines map[string]int) []Problem {
	var messages []string
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		messages = typeErr.Errors
	} else {
		messages = []string{err.Error()}
	}
	problems := make([]Problem, 0, len(messages))
	for _, m := range messages {
		pr := Problem{Message: m}
		if g := yamlLine.FindStringSubmatch(m); g != nil {
			pr.Line, _ = strconv.Atoi(g[1])
			pr.Message = g[2]
			pr.Path = pathAt(lines, pr.Line)
		}
		problems = append(problems, pr)
	}
	return problems
}

func pathAt(lines map[string]int, line int) string {
	best := ""
	for path, n := range lines {
		if n == line && len(path) > len(best) {
			best = path
		}
	}
	return best
}

func planned(what string) string {
	return what + " is not supported yet, planned"
}

func (p *parser) validate(c Config) {
	switch c.Version {
	case 1:
	case 0:
		p.add("version", "is required and must be 1")
	default:
		p.add("version", "must be 1, got %d", c.Version)
	}
	p.validateForge(c.Forge)
	if !repoPattern.MatchString(c.DeployRepo) {
		p.add("deploy_repo", "must be owner/name, got %q", c.DeployRepo)
	}
	p.validateNames("environments", c.Environments)
	p.validateApps(c.Apps)
	p.validateRelease(c.Release)
	for i, n := range c.Notify {
		p.validateNotifier(fmt.Sprintf("notify[%d]", i), n)
	}
}

func (p *parser) validateForge(f Forge) {
	switch f.Type {
	case "github":
	case "gitlab":
		p.add("forge.type", "%s", planned("gitlab"))
	case "":
		p.add("forge.type", "is required, want github")
	default:
		p.add("forge.type", "unknown forge %q, want github", f.Type)
	}
	if f.URL != "" {
		u, err := url.Parse(f.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			p.add("forge.url", "must be an http or https URL, got %q", f.URL)
		}
	}
}

func (p *parser) validateNames(path string, names []string) {
	seen := map[string]int{}
	for i, n := range names {
		at := fmt.Sprintf("%s[%d]", path, i)
		if !namePattern.MatchString(n) {
			p.add(at, "name %q must match %s", n, namePattern)
			continue
		}
		if j, ok := seen[n]; ok {
			p.add(at, "duplicate %q, also at %s[%d]", n, path, j)
			continue
		}
		seen[n] = i
	}
}

func (p *parser) validateApps(apps []App) {
	if len(apps) == 0 {
		p.add("apps", "at least one app is required")
		return
	}
	names := map[string]int{}
	images := map[string]int{}
	for i, a := range apps {
		at := fmt.Sprintf("apps[%d]", i)
		switch j, dup := names[a.Name]; {
		case !namePattern.MatchString(a.Name):
			p.add(at+".name", "name %q must match %s", a.Name, namePattern)
		case dup:
			p.add(at+".name", "duplicate name %q, also at apps[%d]", a.Name, j)
		default:
			names[a.Name] = i
		}
		if msg := imageProblem(a.Image); msg != "" {
			p.add(at+".image", "%s", msg)
		} else if j, dup := images[a.Image]; dup {
			p.add(at+".image", "duplicate image %q, also at apps[%d]", a.Image, j)
		} else {
			images[a.Image] = i
		}
		if a.TagPrefix != "" {
			p.add(at+".tag_prefix", "%s for monorepos", planned("tag_prefix"))
		}
		p.validatePin(at+".pin", a.Pin)
	}
}

func imageProblem(image string) string {
	switch {
	case image == "":
		return "is required"
	case strings.Contains(image, "@"):
		return fmt.Sprintf("image %q must not have a digest", image)
	case strings.Contains(image[strings.LastIndex(image, "/")+1:], ":"):
		return fmt.Sprintf("image %q must not have a tag, cutship sets it", image)
	case !imagePattern.MatchString(image):
		return fmt.Sprintf("image %q is not a valid image repository", image)
	}
	return ""
}

func (p *parser) validatePin(path string, pin Pin) {
	byFile := pin.File != "" || pin.Path != ""
	switch {
	case pin.Preset != "" && byFile:
		p.add(path, "set either preset or file and path")
	case pin.Preset == "" && !byFile:
		p.add(path, "is required: set preset or file and path")
	case byFile && (pin.File == "" || pin.Path == ""):
		p.add(path, "file and path go together")
	case byFile:
		p.add(path, "%s", planned("pin by file and path"))
	case pin.Preset == "ansible":
	case plannedPresets[pin.Preset]:
		p.add(path+".preset", "%s", planned(fmt.Sprintf("preset %q", pin.Preset)))
	default:
		p.add(path+".preset", "unknown preset %q, want ansible", pin.Preset)
	}
}

func (p *parser) validateRelease(r Release) {
	if r.Versioning != "conventional" {
		p.add("release.versioning", "unknown versioning %q, want conventional", r.Versioning)
	}
	if msg := tagFormatProblem(r.TagFormat); msg != "" {
		p.add("release.tag_format", "%s", msg)
	}
	pre := r.BumpBranchPrefix
	if !prefixPattern.MatchString(pre) || strings.Contains(pre, "..") || strings.Contains(pre, ".lock/") {
		p.add("release.bump_branch_prefix", "%q must be a branch name part ending with /", pre)
	}
	for _, v := range []string{"{app}", "{tag}"} {
		if !strings.Contains(r.BumpTitle, v) {
			p.add("release.bump_title", "%q must contain %s", r.BumpTitle, v)
		}
	}
}

func tagFormatProblem(format string) string {
	if strings.Count(format, versionPlaceholder) != 1 {
		return fmt.Sprintf("%q must contain %s exactly once", format, versionPlaceholder)
	}
	sample := strings.Replace(format, versionPlaceholder, "1.2.3", 1)
	if !tagPattern.MatchString(sample) || strings.Contains(sample, "..") || strings.HasSuffix(sample, ".lock") {
		return fmt.Sprintf("%q does not give a valid git and image tag, %q", format, sample)
	}
	return ""
}

func (p *parser) validateNotifier(path string, n Notifier) {
	switch n.Type {
	case "telegram":
	case "slack":
		p.add(path+".type", "%s", planned("slack"))
		return
	case "":
		p.add(path+".type", "is required, want telegram")
		return
	default:
		p.add(path+".type", "unknown notifier %q, want telegram", n.Type)
		return
	}
	switch n.Mode {
	case "channel":
		if n.TopicID != 0 {
			p.add(path+".topic_id", "needs mode topics")
		}
	case "topics":
		p.add(path+".mode", "%s", planned("mode topics"))
	default:
		p.add(path+".mode", "unknown mode %q, want channel", n.Mode)
	}
	if n.ChatID == "" {
		p.add(path+".chat_id", "is required")
	}
	if n.Channel != "" {
		p.add(path+".channel", "is for slack, telegram uses chat_id")
	}
}
