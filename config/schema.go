package config

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"
)

const SchemaID = "https://github.com/ambrdk/cutship/releases/latest/download/cutship.schema.json"

func SchemaDocument() *jsonschema.Schema {
	r := jsonschema.Reflector{DoNotReference: true, ExpandedStruct: true}
	s := r.Reflect(&Config{})
	s.ID = SchemaID
	s.Title = "cutship.yaml"
	s.Description = "Release cycle of the apps deployed from one deploy repository. Lives in the root of that repository."
	return s
}

func Schema() ([]byte, error) {
	data, err := json.MarshalIndent(SchemaDocument(), "", "  ")
	if err != nil {
		return nil, err
	}
	for _, c := range "<>&" {
		data = bytes.ReplaceAll(data, fmt.Appendf(nil, `\u%04x`, c), []byte(string(c)))
	}
	return data, nil
}

func describe(s *jsonschema.Schema, descriptions map[string]string) {
	for name, text := range descriptions {
		if p, ok := s.Properties.Get(name); ok {
			p.Description = text
		}
	}
}

func property(s *jsonschema.Schema, name string) *jsonschema.Schema {
	p, _ := s.Properties.Get(name)
	return p
}

func enum(values ...any) []any {
	return values
}

func zero() *uint64 {
	var n uint64
	return &n
}

func one() *uint64 {
	n := uint64(1)
	return &n
}

func (Config) JSONSchemaExtend(s *jsonschema.Schema) {
	describe(s, map[string]string{
		"version":      "Version of this file format. Only 1 exists.",
		"forge":        "Where the deploy repository and the service repositories live.",
		"deploy_repo":  "Deploy repository as owner/name. Bump pull requests go there.",
		"environments": "Environments the apps are deployed to. Default [prod]. Promotion between environments is planned.",
		"apps":         "Apps whose images this deploy repository pins.",
		"release":      "How releases are tagged and how bump pull requests are named.",
		"lanes":        "When a bump pull request may take the fast lane.",
		"notify":       "Messengers that get one post per release.",
	})
	property(s, "version").Enum = enum(1)
	envs := property(s, "environments")
	envs.Items.Pattern = namePattern.String()
	envs.UniqueItems = true
	property(s, "deploy_repo").Pattern = repoPattern.String()
	property(s, "apps").MinItems = one()
}

func (Forge) JSONSchemaExtend(s *jsonschema.Schema) {
	describe(s, map[string]string{
		"type": "Forge type. gitlab is planned. The token comes from GITHUB_TOKEN.",
		"url":  "API base URL for a self-hosted forge. Empty means the public one.",
	})
	property(s, "type").Enum = enum("github")
	url := property(s, "url")
	url.Format = "uri"
	url.Pattern = `^https?://[^/\s]+`
}

func (App) JSONSchemaExtend(s *jsonschema.Schema) {
	describe(s, map[string]string{
		"name":       "App name, also the name of the bump branch and of the pin preset path.",
		"image":      "Image repository without a tag or digest. Tags of this image must follow release.tag_format.",
		"tag_prefix": "Tag prefix of the app in a monorepo. Not supported yet, planned; must be empty.",
		"pin":        "Where the deploy repository pins the image tag.",
	})
	property(s, "name").Pattern = namePattern.String()
	property(s, "image").Pattern = imagePattern.String()
	property(s, "tag_prefix").MaxLength = zero()
}

func (Pin) JSONSchemaExtend(s *jsonschema.Schema) {
	describe(s, map[string]string{
		"preset": "Known layout. ansible: the image key of apps/<app>/app.yml. helm, kustomize and compose are planned.",
		"file":   "File that holds the pin, relative to the deploy repository. Not supported yet, planned; must be empty.",
		"path":   "YAML path of the pin inside file. Not supported yet, planned; must be empty.",
	})
	property(s, "preset").Enum = enum("ansible")
	property(s, "file").MaxLength = zero()
	property(s, "path").MaxLength = zero()
	s.Required = []string{"preset"}
}

func (Release) JSONSchemaExtend(s *jsonschema.Schema) {
	describe(s, map[string]string{
		"versioning":         "How the next version is chosen. conventional: from conventional commit subjects since the last release tag.",
		"tag_format":         "Release tag of git and of the image, with {version} exactly once. Default v{version}.",
		"bump_branch_prefix": "Prefix of bump branches, ending with /. Default cutship/.",
		"bump_title":         "Title of bump pull requests with {app} and {tag}. Default chore(apps): bump {app} to {tag}.",
	})
	property(s, "versioning").Enum = enum("conventional", "")
	orEmpty(property(s, "tag_format"), &jsonschema.Schema{
		Pattern:   `^(?:[A-Za-z0-9_][A-Za-z0-9_.-]*)?\{version\}[A-Za-z0-9_.-]*$`,
		MaxLength: maxLength(uint64(128 - len("1.2.3") + len(versionPlaceholder))),
		Not:       matchesAny(`\.\.`, `\.lock$`),
	})
	orEmpty(property(s, "bump_branch_prefix"), &jsonschema.Schema{
		Pattern: prefixPattern.String(),
		Not:     matchesAny(`\.\.`, `\.lock/`),
	})
	orEmpty(property(s, "bump_title"), &jsonschema.Schema{
		AllOf: []*jsonschema.Schema{{Pattern: `\{app\}`}, {Pattern: `\{tag\}`}},
	})
}

func orEmpty(p, constraint *jsonschema.Schema) {
	p.AnyOf = []*jsonschema.Schema{{MaxLength: zero()}, constraint}
}

func matchesAny(patterns ...string) *jsonschema.Schema {
	s := &jsonschema.Schema{}
	for _, p := range patterns {
		s.AnyOf = append(s.AnyOf, &jsonschema.Schema{Pattern: p})
	}
	return s
}

func maxLength(n uint64) *uint64 {
	return &n
}

func (Lanes) JSONSchemaExtend(s *jsonschema.Schema) {
	describe(s, map[string]string{
		"required_check": "Check that must be green on the bump pull request before its deploy skips CI. Default lint.",
		"ci_workflow":    "Name of the workflow whose result cutship notify adds to the release post. Default ci.",
	})
}

func (Notifier) JSONSchemaExtend(s *jsonschema.Schema) {
	describe(s, map[string]string{
		"type":     "Messenger. telegram uses the bot token from TELEGRAM_BOT_TOKEN. slack is planned.",
		"mode":     "Telegram mode. channel: a post in a channel and its comment in the discussion group. topics is planned.",
		"chat_id":  "Telegram chat id, -100... for a channel.",
		"topic_id": "Telegram topic id for mode topics, planned.",
		"channel":  "Slack channel id, planned.",
	})
	property(s, "type").Enum = enum("telegram")
	property(s, "mode").Enum = enum("channel", "")
	chat := property(s, "chat_id")
	chat.Type = ""
	chat.AnyOf = []*jsonschema.Schema{{Type: "string", MinLength: one()}, {Type: "integer"}}
	property(s, "topic_id").Enum = enum(0)
	property(s, "channel").MaxLength = zero()
	s.Required = []string{"type", "chat_id"}
}
