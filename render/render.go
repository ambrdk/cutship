package render

import (
	"embed"
	"html"
	"net/url"
	"strings"
	"text/template"
	"time"

	"github.com/ambrdk/cutship/event"
	"github.com/ambrdk/cutship/release"
)

//go:embed post.tmpl comment.tmpl
var files embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"esc":      html.EscapeString,
	"header":   header,
	"hosts":    hosts,
	"failedAt": failedAt,
	"links":    links,
	"versions": func(s release.State) string { return html.EscapeString(versions(s)) },
	"short":    short,
	"line":     line,
}).ParseFS(files, "*.tmpl"))

func Post(s release.State) string {
	return execute("post.tmpl", s)
}

func Comment(s release.State, skip int) string {
	if skip > 0 && len(s.Timeline) > 1 {
		s.Timeline = append(s.Timeline[:1:1], s.Timeline[1+min(skip, len(s.Timeline)-1):]...)
	}
	return execute("comment.tmpl", s)
}

func execute(name string, s release.State) string {
	var b strings.Builder
	if err := templates.ExecuteTemplate(&b, name, s); err != nil {
		panic(err)
	}
	return strings.TrimRight(b.String(), "\n")
}

func header(s release.State) string {
	name := s.App
	if name == "" {
		name = s.Title
	}
	if name == "" {
		name = "deploy run"
	}
	full := name
	if s.To != "" {
		full += " " + s.To
	}
	name, full = html.EscapeString(name), html.EscapeString(full)
	lasted := s.FinishedAt.Sub(s.StartedAt).Round(time.Second).String()
	switch s.Stage {
	case release.Opened:
		return "📦 <b>Release: " + full + "</b> (waiting for merge)"
	case release.ChecksFailed:
		return "📦 <b>Release: " + full + "</b> (checks failed)"
	case release.Merged:
		return "🔀 <b>Merged: " + full + "</b> (waiting for deploy)"
	case release.Deployed:
		if s.NoChanges {
			return "✅ <b>Deployed: " + full + "</b> (no changes)"
		}
		return "✅ <b>Deployed: " + full + "</b> (took " + lasted + ")"
	case release.Failed:
		return "❌ <b>Deploy failed: " + full + "</b> (after " + lasted + ")"
	case release.Cancelled:
		return "⚪ <b>Deploy cancelled: " + full + "</b>"
	case release.Closed:
		return "⚪ <b>Closed: " + full + "</b> (not merged)"
	default:
		return "🚀 <b>Deploy: " + name + "</b>"
	}
}

func hosts(hs []event.Host) string {
	names := make([]string, 0, len(hs))
	for _, h := range hs {
		names = append(names, html.EscapeString(h.Name))
	}
	return strings.Join(names, ", ")
}

func failedAt(s release.State) string {
	if s.Stage == release.Failed {
		return s.FailedTask
	}
	return ""
}

func links(s release.State) string {
	var parts []string
	if s.PRURL != "" {
		parts = append(parts, `<a href="`+html.EscapeString(s.PRURL)+`">`+html.EscapeString(prLabel(s))+`</a>`)
	}
	if s.RunURL != "" {
		parts = append(parts, `<a href="`+html.EscapeString(s.RunURL)+`">run</a>`)
	}
	return strings.Join(parts, " · ")
}

func prLabel(s release.State) string {
	u, err := url.Parse(s.PRURL)
	if err != nil {
		return "PR"
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) >= 4 && segments[2] == "pull" {
		return segments[1] + "#" + segments[3]
	}
	return "PR"
}

func versions(s release.State) string {
	if s.From == "" {
		return s.To
	}
	return s.From + " → " + s.To
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func line(l release.Line) string {
	out := "[" + l.At.UTC().Format("15:04:05") + " UTC] [" + l.Tag + "]"
	if l.Text != "" {
		out += " " + html.EscapeString(l.Text)
	}
	return out
}
