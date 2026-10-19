package fake

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/ambrdk/cutship/forge"
)

var (
	_ forge.Forge      = (*Forge)(nil)
	_ forge.AutoMerger = (*Forge)(nil)
	_ forge.Settings   = (*Forge)(nil)
)

func (f *Forge) SetSettings(name string, s forge.RepoSettings) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repo(name).settings = s
}

func (f *Forge) RepoSettings(_ context.Context, name string) (forge.RepoSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("RepoSettings"); err != nil {
		return forge.RepoSettings{}, err
	}
	r, err := f.known(name)
	if err != nil {
		return forge.RepoSettings{}, err
	}
	return r.settings, nil
}

func (f *Forge) CreateTag(_ context.Context, name, tag, sha string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("CreateTag"); err != nil {
		return err
	}
	r := f.repo(name)
	if current, ok := r.tags[tag]; ok {
		if current != sha {
			return fmt.Errorf("%s points at %s: %w", tag, current, forge.ErrTagElsewhere)
		}
		return nil
	}
	r.tags[tag] = sha
	return nil
}

func sameTree(a, b map[string][]byte) bool {
	return maps.EqualFunc(a, b, func(x, y []byte) bool { return string(x) == string(y) })
}

func (f *Forge) PushChange(_ context.Context, name string, req forge.ChangeRequest) (forge.Change, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("PushChange"); err != nil {
		return forge.Change{}, err
	}
	r, err := f.known(name)
	if err != nil {
		return forge.Change{}, err
	}
	baseSHA, ok := r.branches[req.Base]
	if !ok {
		return forge.Change{}, fmt.Errorf("branch %s: %w", req.Base, forge.ErrNotFound)
	}
	tree := maps.Clone(f.commits[baseSHA].files)
	maps.Copy(tree, req.Files)
	head, exists := r.branches[req.Branch]
	if !exists || !sameTree(f.commits[head].files, tree) {
		head = f.next("sha")
		f.commits[head] = &commit{parent: baseSHA, files: tree}
		r.branches[req.Branch] = head
	}
	for i := len(r.changes) - 1; i >= 0; i-- {
		ch := r.changes[i]
		if ch.HeadRef == req.Branch && ch.State == forge.Open {
			ch.HeadSHA = head
			return *ch, nil
		}
	}
	ch := &forge.Change{Number: len(r.changes) + 1, Title: req.Title, HeadRef: req.Branch, HeadSHA: head, State: forge.Open}
	ch.URL = fmt.Sprintf("https://forge.test/%s/pull/%d", name, ch.Number)
	r.changes = append(r.changes, ch)
	return *ch, nil
}

func (f *Forge) CloseChange(_ context.Context, name string, n int, deleteBranch bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("CloseChange"); err != nil {
		return err
	}
	r := f.repo(name)
	if n < 1 || n > len(r.changes) {
		return fmt.Errorf("change %d: %w", n, forge.ErrNotFound)
	}
	ch := r.changes[n-1]
	ch.State = forge.Closed
	if deleteBranch {
		delete(r.branches, ch.HeadRef)
	}
	return nil
}

func (f *Forge) CreateComment(_ context.Context, name string, n int, body string) (forge.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("CreateComment"); err != nil {
		return forge.Comment{}, err
	}
	return f.addComment(name, n, f.User, body), nil
}

func (f *Forge) UpdateComment(_ context.Context, name string, id int64, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("UpdateComment"); err != nil {
		return err
	}
	for _, list := range f.repo(name).comments {
		for i := range list {
			if list[i].ID == id {
				list[i].Body = body
				return nil
			}
		}
	}
	return fmt.Errorf("comment %d: %w", id, forge.ErrNotFound)
}

func (f *Forge) AutoMerge(_ context.Context, name string, n int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("AutoMerge"); err != nil {
		return err
	}
	f.repo(name).autoMerge[n] = true
	return nil
}

func (f *Forge) AutoMergeOn(name string, n int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.repo(name).autoMerge[n]
}

func (f *Forge) Merge(name string, n int, by string, at time.Time) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.repo(name)
	ch := r.changes[n-1]
	tree := maps.Clone(f.commits[ch.HeadSHA].files)
	parent := r.branches[r.defaultBranch]
	sha := f.next("sha")
	f.commits[sha] = &commit{parent: parent, files: tree}
	r.branches[r.defaultBranch] = sha
	ch.State, ch.MergeSHA, ch.MergedBy = forge.Merged, sha, by
	ch.MergedAt = &at
	r.pullsByCommit[sha] = append(r.pullsByCommit[sha], n)
	delete(r.branches, ch.HeadRef)
	return sha
}
