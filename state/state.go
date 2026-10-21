package state

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/release"
)

const (
	Version     = 1
	marker      = "<!-- cutship:v1 "
	anyMarker   = "<!-- cutship:"
	oldMarker   = "<!-- deploy-notify:v1 "
	oldAny      = "<!-- deploy-notify:"
	markerEnd   = " -->"
	oldMessages = "telegram"
)

type Record struct {
	Version  int                        `json:"version"`
	App      string                     `json:"app"`
	Env      string                     `json:"env"`
	Tag      string                     `json:"tag"`
	State    release.State              `json:"state"`
	Messages map[string]json.RawMessage `json:"messages,omitempty"`
}

type Store struct {
	f      forge.Forge
	repo   string
	link   func(Record) string
	mu     sync.Mutex
	author string
}

func New(f forge.Forge, repo string, link func(Record) string) *Store {
	return &Store{f: f, repo: repo, link: link}
}

func (s *Store) self(ctx context.Context) (string, error) {
	s.mu.Lock()
	known := s.author
	s.mu.Unlock()
	if known != "" {
		return known, nil
	}
	login, err := s.f.Viewer(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve the token author: %w", err)
	}
	s.mu.Lock()
	s.author = login
	s.mu.Unlock()
	return login, nil
}

func between(body, start string) (string, bool) {
	i := strings.Index(body, start)
	if i < 0 {
		return "", false
	}
	rest := body[i+len(start):]
	end := strings.Index(rest, markerEnd)
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

func parse(change int, body string) (Record, bool) {
	if raw, ok := between(body, marker); ok {
		var r Record
		if json.Unmarshal([]byte(raw), &r) != nil || r.Version != Version {
			slog.Warn("cutship state is unreadable, starting a new one", "change", change)
			return Record{}, false
		}
		return r, true
	}
	if raw, ok := between(body, oldMarker); ok {
		var old struct {
			State release.State   `json:"state"`
			IDs   json.RawMessage `json:"ids"`
		}
		if json.Unmarshal([]byte(raw), &old) != nil {
			slog.Warn("deploy-notify state is unreadable, starting a new one", "change", change)
			return Record{}, false
		}
		r := Record{Version: Version, App: old.State.App, Tag: old.State.To, State: old.State}
		if len(old.IDs) > 0 && string(old.IDs) != "null" {
			r.Messages = map[string]json.RawMessage{oldMessages: old.IDs}
		}
		return r, true
	}
	slog.Warn("cutship state has an unknown version, starting a new one", "change", change)
	return Record{}, false
}

type found struct {
	id     int64
	body   string
	record Record
	ok     bool
}

func (s *Store) find(ctx context.Context, ch forge.Change, app string) (found, error) {
	self, err := s.self(ctx)
	if err != nil {
		return found{}, err
	}
	change := ch.Number
	comments, err := s.f.Comments(ctx, s.repo, change)
	if err != nil {
		return found{}, err
	}
	var broken, old found
	for _, c := range comments {
		if c.Author != self && (ch.Author == "" || c.Author != ch.Author) {
			continue
		}
		switch {
		case strings.Contains(c.Body, anyMarker):
			r, ok := parse(change, c.Body)
			if !ok {
				if broken.id == 0 {
					broken = found{id: c.ID, body: c.Body}
				}
				continue
			}
			if r.App == app || r.App == "" {
				return found{id: c.ID, body: c.Body, record: r, ok: true}, nil
			}
		case old.id == 0 && strings.Contains(c.Body, oldAny):
			if r, ok := parse(change, c.Body); ok && (r.App == app || r.App == "") {
				old = found{id: c.ID, body: c.Body, record: r, ok: true}
			} else if !ok && broken.id == 0 {
				broken = found{id: c.ID, body: c.Body}
			}
		}
	}
	if old.id != 0 {
		return old, nil
	}
	return broken, nil
}

func (s *Store) Load(ctx context.Context, ch forge.Change, app string) (Record, bool, error) {
	f, err := s.find(ctx, ch, app)
	return f.record, f.ok, err
}

func (s *Store) Update(ctx context.Context, ch forge.Change, app string, apply func(*Record)) (Record, error) {
	change := ch.Number
	f, err := s.find(ctx, ch, app)
	id, r := f.id, f.record
	if err != nil {
		return Record{}, err
	}
	apply(&r)
	r.Version = Version
	if r.App == "" {
		r.App = app
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return Record{}, err
	}
	body := s.link(r) + "\n\n" + marker + string(raw) + markerEnd
	if id != 0 && body == f.body {
		return r, nil
	}
	if id != 0 {
		return r, s.f.UpdateComment(ctx, s.repo, id, body)
	}
	_, err = s.f.CreateComment(ctx, s.repo, change, body)
	return r, err
}
