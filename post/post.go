package post

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/ambrdk/cutship/event"
	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/notifier"
	"github.com/ambrdk/cutship/release"
	"github.com/ambrdk/cutship/render"
	"github.com/ambrdk/cutship/state"
)

const migratedTelegram = "telegram"

type Target struct {
	Change forge.Change
	App    string
	Env    string
	Tag    string
}

type Publisher struct {
	st   *state.Store
	ns   []notifier.Notifier
	warn func(string)
}

func New(f forge.Forge, repo string, ns []notifier.Notifier, warn func(string)) *Publisher {
	p := &Publisher{ns: ns, warn: warn}
	p.st = state.New(f, repo, p.link)
	return p
}

func (p *Publisher) Store() *state.Store {
	return p.st
}

func (p *Publisher) link(r state.Record) string {
	for _, n := range p.ns {
		if l := n.Link(r.Messages[n.Key()]); l != "" {
			return "Release thread: " + l
		}
	}
	return "cutship release " + r.App + " " + r.Tag
}

func (p *Publisher) migrate(r *state.Record) {
	raw, ok := r.Messages[migratedTelegram]
	if !ok {
		return
	}
	for _, n := range p.ns {
		if strings.HasPrefix(n.Key(), migratedTelegram+":") {
			if _, has := r.Messages[n.Key()]; !has {
				r.Messages[n.Key()] = raw
			}
			delete(r.Messages, migratedTelegram)
			return
		}
	}
}

func (p *Publisher) Apply(ctx context.Context, t Target, events ...event.Event) (state.Record, error) {
	changed := false
	rec, err := p.st.Update(ctx, t.Change, t.App, func(r *state.Record) {
		if r.Env == "" {
			r.Env = t.Env
		}
		if r.Tag == "" {
			r.Tag = t.Tag
		}
		p.migrate(r)
		for _, e := range events {
			var c bool
			r.State, c = release.Apply(r.State, e)
			changed = changed || c
		}
	})
	if err != nil {
		return state.Record{}, err
	}
	view := render.Build(rec.State)
	refs := map[string]json.RawMessage{}
	for _, n := range p.ns {
		old := rec.Messages[n.Key()]
		if !changed && n.Done(old) {
			continue
		}
		ref, status, err := n.Publish(ctx, old, view)
		if err != nil {
			p.warn(fmt.Sprintf("%s: the release post was not updated: %v", n.Key(), err))
		}
		if w := notifier.Warning(status); w != "" {
			p.warn(n.Key() + ": " + w)
		}
		if len(ref) > 0 && !bytes.Equal(ref, old) {
			refs[n.Key()] = ref
		}
	}
	if len(refs) == 0 {
		return rec, nil
	}
	return p.st.Update(ctx, t.Change, t.App, func(r *state.Record) {
		if r.Messages == nil {
			r.Messages = map[string]json.RawMessage{}
		}
		maps.Copy(r.Messages, refs)
	})
}
