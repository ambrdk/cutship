package source

import (
	"sort"
	"time"

	"github.com/ambrdk/ship/event"
)

type ReportHost struct {
	OK          int `json:"ok"`
	Changed     int `json:"changed"`
	Failed      int `json:"failed"`
	Unreachable int `json:"unreachable"`
	Skipped     int `json:"skipped"`
}

type Failure struct {
	Host    string `json:"host"`
	Task    string `json:"task"`
	Message string `json:"message"`
}

type Report struct {
	Version    int                   `json:"version"`
	StartedAt  time.Time             `json:"started_at"`
	FinishedAt time.Time             `json:"finished_at"`
	Hosts      map[string]ReportHost `json:"hosts"`
	Failure    *Failure              `json:"failure"`
}

type extraVars struct {
	DeployApps []string `json:"deploy_apps"`
}

func (h ReportHost) idle() bool {
	return h.OK <= 1 && h.Changed == 0 && h.Failed == 0 && h.Unreachable == 0
}

func (r Report) hosts() []event.Host {
	out := make([]event.Host, 0, len(r.Hosts))
	for name, h := range r.Hosts {
		if h.idle() {
			continue
		}
		out = append(out, event.Host{Name: name, OK: h.OK, Changed: h.Changed, Failed: h.Failed, Unreachable: h.Unreachable})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
