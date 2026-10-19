package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/ambrdk/cutship/config"
	"github.com/invopop/jsonschema"
)

func main() {
	if _, err := fmt.Fprint(os.Stdout, reference()); err != nil {
		os.Exit(1)
	}
}

const header = "# cutship.yaml reference\n\n" +
	"`cutship.yaml` lives in the root of the deploy repository. This page is generated from the JSON Schema " +
	"(`go run ./internal/docgen > docs/config.md`); `cutship schema` prints the same schema for editors, " +
	"and `cutship validate` checks a file against the same rules with line numbers.\n\n" +
	"| Field | Type | Required | Description |\n|---|---|---|---|\n"

func reference() string {
	var b strings.Builder
	b.WriteString(header)
	walk(&b, config.SchemaDocument(), "")
	return b.String()
}

func walk(b *strings.Builder, s *jsonschema.Schema, prefix string) {
	if s.Properties == nil {
		return
	}
	for pair := s.Properties.Oldest(); pair != nil; pair = pair.Next() {
		name, p := pair.Key, pair.Value
		path := prefix + name
		required := "no"
		if slices.Contains(s.Required, name) {
			required = "yes"
		}
		fmt.Fprintf(b, "| `%s` | %s | %s | %s |\n", path, typeOf(p), required, describe(p))
		switch {
		case p.Type == "object":
			walk(b, p, path+".")
		case p.Type == "array" && p.Items != nil && p.Items.Type == "object":
			walk(b, p.Items, path+"[].")
		}
	}
}

func typeOf(p *jsonschema.Schema) string {
	switch {
	case p.Type == "array" && p.Items != nil && p.Items.Type == "object":
		return "list of objects"
	case p.Type == "array" && p.Items != nil:
		return "list of " + p.Items.Type + "s"
	case p.Type != "":
		return p.Type
	}
	var types []string
	for _, alt := range p.AnyOf {
		if alt.Type != "" && !slices.Contains(types, alt.Type) {
			types = append(types, alt.Type)
		}
	}
	if len(types) == 0 {
		return "string"
	}
	return strings.Join(types, " or ")
}

func describe(p *jsonschema.Schema) string {
	text := p.Description
	var allowed []string
	for _, v := range p.Enum {
		if s := fmt.Sprint(v); s != "" {
			allowed = append(allowed, "`"+s+"`")
		}
	}
	if len(allowed) > 0 {
		text += " Allowed now: " + strings.Join(allowed, ", ") + "."
	}
	return strings.NewReplacer("|", "\\|", "<", "&lt;", ">", "&gt;").Replace(text)
}
