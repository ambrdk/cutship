package release

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ambrdk/deploy-notify/event"
)

const keepLast = 30

type Stage string

const (
	Opened       Stage = "opened"
	ChecksFailed Stage = "checks_failed"
	Merged       Stage = "merged"
	Deploying    Stage = "deploying"
	Deployed     Stage = "deployed"
	Failed       Stage = "failed"
	Cancelled    Stage = "cancelled"
	Closed       Stage = "closed"
)

type Line struct {
	At   time.Time `json:"at"`
	Tag  string    `json:"tag"`
	Text string    `json:"text"`
}

type State struct {
	Stage      Stage        `json:"stage,omitempty"`
	App        string       `json:"app,omitempty"`
	From       string       `json:"from,omitempty"`
	To         string       `json:"to,omitempty"`
	Image      string       `json:"image,omitempty"`
	Title      string       `json:"title,omitempty"`
	PR         int          `json:"pr,omitempty"`
	PRURL      string       `json:"pr_url,omitempty"`
	RunURL     string       `json:"run_url,omitempty"`
	MergedBy   string       `json:"merged_by,omitempty"`
	Commit     string       `json:"commit,omitempty"`
	Hosts      []event.Host `json:"hosts,omitempty"`
	FailedTask string       `json:"failed_task,omitempty"`
	StartedAt  time.Time    `json:"started_at,omitzero"`
	FinishedAt time.Time    `json:"finished_at,omitzero"`
	NoChanges  bool         `json:"no_changes,omitempty"`
	Timeline   []Line       `json:"timeline,omitempty"`
	Seen       []string     `json:"seen,omitempty"`
}

func rank(s Stage) int {
	switch s {
	case Opened, ChecksFailed:
		return 1
	case Merged:
		return 2
	case Deploying:
		return 3
	case Deployed, Failed, Cancelled:
		return 4
	default:
		return 0
	}
}

func retryable(s Stage) bool {
	return s == Failed || s == Cancelled
}

func Apply(s State, e event.Event) (State, bool) {
	if slices.Contains(s.Seen, e.ID) {
		return s, false
	}
	s = clone(s)
	s.Seen = append(s.Seen, e.ID)
	fill(&s, e)
	switch e.Kind {
	case event.ReleaseOpened:
		s.add(e.At, "PR", "Bump opened: "+versions(e.From, e.To))
		if s.Stage == "" || s.Stage == Closed {
			s.Stage = Opened
		}
	case event.Reopened:
		s.add(e.At, "REOPENED", "")
		if s.Stage == Closed || s.Stage == "" {
			s.Stage = Opened
		}
	case event.ChecksFinished:
		passed := e.Result == event.Success
		s.add(e.At, "CI", map[bool]string{true: "Checks passed", false: "Checks failed"}[passed])
		if rank(s.Stage) <= 1 && s.Stage != Closed {
			s.Stage = map[bool]Stage{true: Opened, false: ChecksFailed}[passed]
		}
	case event.Merged:
		s.MergedBy = e.Actor
		s.add(e.At, "MERGED", "by "+e.Actor)
		if rank(s.Stage) < 2 {
			s.Stage = Merged
		}
	case event.ClosedUnmerged:
		if rank(s.Stage) < 2 {
			s.add(e.At, "CLOSED", "Not merged")
			s.Stage = Closed
		}
	case event.DeployStarted:
		started(&s, e)
	case event.DeployFinished:
		finished(&s, e)
	}
	s.Timeline = trim(sortLines(s.Timeline))
	return s, true
}

func started(s *State, e event.Event) {
	if retryable(s.Stage) {
		s.add(e.At, "RETRY", fmt.Sprintf("Deploy attempt %d", max(e.Attempt, 2)))
	}
	s.add(e.At, "DEPLOY", "Started")
	if rank(s.Stage) < 3 || retryable(s.Stage) {
		s.Stage = Deploying
		s.StartedAt = e.At
	}
}

func finished(s *State, e event.Event) {
	if e.Result == event.Cancelled {
		s.add(e.At, "CANCELLED", "superseded by next deploy")
		if rank(s.Stage) < 4 {
			s.Stage = Cancelled
		}
		return
	}
	if e.Result == event.Success && retryable(s.Stage) {
		s.add(e.At, "RETRY", fmt.Sprintf("Deploy attempt %d", max(e.Attempt, 2)))
	}
	for _, h := range e.Hosts {
		s.add(e.At, "HOST", hostLine(h))
	}
	if s.StartedAt.IsZero() || (retryable(s.Stage) && e.Result == event.Success) {
		s.StartedAt = e.At
	}
	lasted := e.At.Sub(s.StartedAt).Round(time.Second).String()
	if e.Result == event.Success {
		s.add(e.At, "DONE", "Deployed, took "+lasted)
		s.Stage = Deployed
		s.Hosts = e.Hosts
		s.NoChanges = e.NoChanges
		s.FailedTask = ""
		s.FinishedAt = e.At
		return
	}
	s.add(e.At, "FAIL", failLine(e))
	s.add(e.At, "FAILED", "lasted "+lasted)
	if s.Stage != Deployed {
		s.Stage = Failed
		s.Hosts = e.Hosts
		s.FailedTask = e.FailedTask
		s.FinishedAt = e.At
	}
}

func fill(s *State, e event.Event) {
	set := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	set(&s.App, e.App)
	set(&s.From, e.From)
	set(&s.To, e.To)
	set(&s.Image, e.Image)
	set(&s.Title, e.Title)
	set(&s.PRURL, e.PRURL)
	set(&s.RunURL, e.RunURL)
	set(&s.Commit, e.Commit)
	if e.PR != 0 {
		s.PR = e.PR
	}
}

func (s *State) add(at time.Time, tag, text string) {
	s.Timeline = append(s.Timeline, Line{At: at, Tag: tag, Text: text})
}

func versions(from, to string) string {
	if from == "" {
		return to
	}
	return from + " → " + to
}

func hostLine(h event.Host) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: ok=%d changed=%d", h.Name, h.OK, h.Changed)
	if h.Failed > 0 {
		fmt.Fprintf(&b, " failed=%d", h.Failed)
	}
	if h.Unreachable > 0 {
		fmt.Fprintf(&b, " unreachable=%d", h.Unreachable)
	}
	return b.String()
}

func failLine(e event.Event) string {
	for _, h := range e.Hosts {
		if h.Failed > 0 || h.Unreachable > 0 {
			return h.Name + ": " + e.FailedTask
		}
	}
	return e.FailedTask
}

func sortLines(lines []Line) []Line {
	slices.SortStableFunc(lines, func(a, b Line) int { return a.At.Compare(b.At) })
	return lines
}

func trim(lines []Line) []Line {
	if len(lines) <= keepLast+1 {
		return lines
	}
	return append([]Line{lines[0]}, lines[len(lines)-keepLast:]...)
}

func clone(s State) State {
	s.Hosts = slices.Clone(s.Hosts)
	s.Timeline = slices.Clone(s.Timeline)
	s.Seen = slices.Clone(s.Seen)
	return s
}
