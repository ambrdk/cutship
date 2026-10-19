package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ambrdk/cutship/notifier"
	"github.com/ambrdk/cutship/render"
)

var _ notifier.Notifier = (*Notifier)(nil)

type ref struct {
	Post int  `json:"post"`
	Done bool `json:"done"`
}

type Notifier struct {
	mu         sync.Mutex
	key        string
	Views      []render.View
	Refs       []json.RawMessage
	fail       []error
	Status     notifier.Status
	Incomplete bool
	posts      int
}

func New(key string) *Notifier {
	return &Notifier{key: key, Status: notifier.OK}
}

func (n *Notifier) Key() string { return n.key }

func (n *Notifier) FailNext(err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.fail = append(n.fail, err)
}

func (n *Notifier) Publish(_ context.Context, raw json.RawMessage, v render.View) (json.RawMessage, notifier.Status, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Refs = append(n.Refs, raw)
	if len(n.fail) > 0 {
		err := n.fail[0]
		n.fail = n.fail[1:]
		return nil, notifier.OK, err
	}
	n.Views = append(n.Views, v)
	var r ref
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, notifier.OK, err
		}
	}
	if r.Post == 0 {
		n.posts++
		r.Post = n.posts
	}
	r.Done = !n.Incomplete
	out, _ := json.Marshal(r)
	return out, n.Status, nil
}

func (n *Notifier) Done(raw json.RawMessage) bool {
	var r ref
	return len(raw) > 0 && json.Unmarshal(raw, &r) == nil && r.Done
}

func (n *Notifier) Link(raw json.RawMessage) string {
	var r ref
	if len(raw) == 0 || json.Unmarshal(raw, &r) != nil || r.Post == 0 {
		return ""
	}
	return fmt.Sprintf("https://chat.test/%s/%d", n.key, r.Post)
}

func (n *Notifier) Posts() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.posts
}

func (n *Notifier) Published() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.Views)
}
