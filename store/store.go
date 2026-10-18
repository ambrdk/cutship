package store

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	"github.com/ambrdk/deploy-notify/github"
	"github.com/ambrdk/deploy-notify/release"
	"github.com/ambrdk/notifykit/thread"
)

const (
	marker    = "<!-- deploy-notify:v1 "
	anyMarker = "<!-- deploy-notify:"
	markerEnd = " -->"
	botLogin  = "github-actions[bot]"
)

type Record struct {
	State release.State `json:"state"`
	IDs   thread.IDs    `json:"ids"`
}

type Store interface {
	Load(ctx context.Context, pr int) (Record, bool, error)
	Save(ctx context.Context, pr int, r Record) error
}

type Memory struct {
	mu      sync.Mutex
	records map[int]Record
}

func NewMemory() *Memory {
	return &Memory{records: map[int]Record{}}
}

func (m *Memory) Load(_ context.Context, pr int) (Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[pr]
	return r, ok, nil
}

func (m *Memory) Save(_ context.Context, pr int, r Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[pr] = r
	return nil
}

type PRComment struct {
	api      *github.API
	repo     string
	link     func(Record) string
	mu       sync.Mutex
	comments map[int]int64
}

func NewPRComment(api *github.API, repo string, link func(Record) string) *PRComment {
	return &PRComment{api: api, repo: repo, link: link, comments: map[int]int64{}}
}

func (p *PRComment) Load(ctx context.Context, pr int) (Record, bool, error) {
	id, body, err := p.find(ctx, pr)
	if err != nil || id == 0 {
		return Record{}, false, err
	}
	p.remember(pr, id)
	start := strings.Index(body, marker)
	if start < 0 {
		slog.Warn("deploy-notify state has an unknown version, starting a new thread", "pr", pr)
		return Record{}, false, nil
	}
	rest := body[start+len(marker):]
	end := strings.Index(rest, markerEnd)
	var r Record
	if end < 0 || json.Unmarshal([]byte(rest[:end]), &r) != nil {
		slog.Warn("deploy-notify state is unreadable, starting a new thread", "pr", pr)
		return Record{}, false, nil
	}
	return r, true, nil
}

func (p *PRComment) Save(ctx context.Context, pr int, r Record) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	body := p.link(r) + "\n\n" + marker + string(raw) + markerEnd
	id := p.known(pr)
	if id == 0 {
		if id, _, err = p.find(ctx, pr); err != nil {
			return err
		}
	}
	if id != 0 {
		return p.api.UpdateComment(ctx, p.repo, id, body)
	}
	c, err := p.api.CreateComment(ctx, p.repo, pr, body)
	if err == nil {
		p.remember(pr, c.ID)
	}
	return err
}

func (p *PRComment) find(ctx context.Context, pr int) (int64, string, error) {
	comments, err := p.api.IssueComments(ctx, p.repo, pr)
	if err != nil {
		return 0, "", err
	}
	for _, c := range comments {
		if c.User.Login == botLogin && strings.Contains(c.Body, anyMarker) {
			return c.ID, c.Body, nil
		}
	}
	return 0, "", nil
}

func (p *PRComment) known(pr int) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.comments[pr]
}

func (p *PRComment) remember(pr int, id int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.comments[pr] = id
}
