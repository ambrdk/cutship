package event

import "time"

type Kind string

const (
	ReleaseOpened  Kind = "release_opened"
	Reopened       Kind = "reopened"
	ChecksFinished Kind = "checks_finished"
	Merged         Kind = "merged"
	ClosedUnmerged Kind = "closed_unmerged"
	DeployStarted  Kind = "deploy_started"
	DeployFinished Kind = "deploy_finished"
)

type Result string

const (
	Success   Result = "success"
	Failure   Result = "failure"
	Cancelled Result = "cancelled"
)

type Host struct {
	Name        string `json:"name"`
	OK          int    `json:"ok"`
	Changed     int    `json:"changed"`
	Failed      int    `json:"failed"`
	Unreachable int    `json:"unreachable"`
}

type Event struct {
	Kind       Kind      `json:"kind"`
	ID         string    `json:"id"`
	At         time.Time `json:"at"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	App        string    `json:"app,omitempty"`
	From       string    `json:"from,omitempty"`
	To         string    `json:"to,omitempty"`
	Image      string    `json:"image,omitempty"`
	Title      string    `json:"title,omitempty"`
	PR         int       `json:"pr,omitempty"`
	Actor      string    `json:"actor,omitempty"`
	Commit     string    `json:"commit,omitempty"`
	RunURL     string    `json:"run_url,omitempty"`
	PRURL      string    `json:"pr_url,omitempty"`
	Attempt    int       `json:"attempt,omitempty"`
	Hosts      []Host    `json:"hosts,omitempty"`
	Result     Result    `json:"result,omitempty"`
	FailedTask string    `json:"failed_task,omitempty"`
	Error      string    `json:"error,omitempty"`
	NoChanges  bool      `json:"no_changes,omitempty"`
}
