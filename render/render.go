package render

import (
	"net/url"
	"strings"

	"github.com/ambrdk/cutship/release"
)

type Link struct {
	Label string
	URL   string
}

type View struct {
	State release.State
	Final bool
	Loud  bool
	Links []Link
}

func Build(s release.State) View {
	v := View{State: s}
	switch s.Stage {
	case release.Deployed, release.Cancelled, release.Closed:
		v.Final = true
	case release.Failed:
		v.Final, v.Loud = true, true
	case release.ChecksFailed:
		v.Loud = true
	}
	if s.PRURL != "" {
		v.Links = append(v.Links, Link{Label: changeLabel(s.PRURL), URL: s.PRURL})
	}
	if s.RunURL != "" {
		v.Links = append(v.Links, Link{Label: "run", URL: s.RunURL})
	}
	return v
}

func changeLabel(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "PR"
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) >= 4 && segments[2] == "pull" {
		return segments[1] + "#" + segments[3]
	}
	return "PR"
}
