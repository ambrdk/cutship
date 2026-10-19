package deploy

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/ambrdk/cutship/event"
)

type Failure struct {
	Host    string `json:"host"`
	Task    string `json:"task"`
	Message string `json:"message"`
}

type Report struct {
	StartedAt  time.Time
	FinishedAt time.Time
	Hosts      []event.Host
	Failure    *Failure
}

type reportHost struct {
	OK          int `json:"ok"`
	Changed     int `json:"changed"`
	Failed      int `json:"failed"`
	Unreachable int `json:"unreachable"`
}

func (h reportHost) idle() bool {
	return h.OK <= 1 && h.Changed == 0 && h.Failed == 0 && h.Unreachable == 0
}

func ReadReport(data []byte) (Report, error) {
	var raw struct {
		Version    int                   `json:"version"`
		StartedAt  time.Time             `json:"started_at"`
		FinishedAt time.Time             `json:"finished_at"`
		Hosts      map[string]reportHost `json:"hosts"`
		Failure    *Failure              `json:"failure"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Report{}, fmt.Errorf("read the deploy report: %w", err)
	}
	if raw.Version != 1 {
		return Report{}, fmt.Errorf("deploy report version %d is not supported, want 1", raw.Version)
	}
	r := Report{StartedAt: raw.StartedAt, FinishedAt: raw.FinishedAt, Failure: raw.Failure}
	for name, h := range raw.Hosts {
		if h.idle() {
			continue
		}
		r.Hosts = append(r.Hosts, event.Host{Name: name, OK: h.OK, Changed: h.Changed, Failed: h.Failed, Unreachable: h.Unreachable})
	}
	sort.Slice(r.Hosts, func(i, j int) bool { return r.Hosts[i].Name < r.Hosts[j].Name })
	return r, nil
}

func (r Report) NoChanges() bool {
	for _, h := range r.Hosts {
		if h.Changed > 0 {
			return false
		}
	}
	return true
}
