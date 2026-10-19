package fake

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/ambrdk/cutship/forge"
)

type commit struct {
	parent string
	files  map[string][]byte
}

type repo struct {
	defaultBranch string
	branches      map[string]string
	tags          map[string]string
	changes       []*forge.Change
	changeFiles   map[int][]forge.File
	checks        map[string][]forge.Check
	comments      map[int][]forge.Comment
	pullsByCommit map[string][]int
	autoMerge     map[int]bool
	settings      forge.RepoSettings
}

type Forge struct {
	mu          sync.Mutex
	User        string
	repos       map[string]*repo
	commits     map[string]*commit
	fail        map[string][]error
	deployments []*deploymentRecord
	beforePush  func()
	seq         int
	Calls       []string
}

func New(user string) *Forge {
	return &Forge{User: user, repos: map[string]*repo{}, commits: map[string]*commit{}, fail: map[string][]error{}}
}

func (f *Forge) repo(name string) *repo {
	r, ok := f.repos[name]
	if !ok {
		r = &repo{
			branches:      map[string]string{},
			tags:          map[string]string{},
			changeFiles:   map[int][]forge.File{},
			checks:        map[string][]forge.Check{},
			comments:      map[int][]forge.Comment{},
			pullsByCommit: map[string][]int{},
			autoMerge:     map[int]bool{},
		}
		f.repos[name] = r
	}
	return r
}

func (f *Forge) next(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s%d", prefix, f.seq)
}

func (f *Forge) FailNext(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[method] = append(f.fail[method], err)
}

func (f *Forge) enter(method string) error {
	f.Calls = append(f.Calls, method)
	if errs := f.fail[method]; len(errs) > 0 {
		f.fail[method] = errs[1:]
		return errs[0]
	}
	return nil
}

func (f *Forge) Commit(repoName, branch string, files map[string][]byte) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.commit(repoName, branch, files)
}

func (f *Forge) commit(repoName, branch string, files map[string][]byte) string {
	r := f.repo(repoName)
	if r.defaultBranch == "" {
		r.defaultBranch = branch
	}
	parent := r.branches[branch]
	tree := map[string][]byte{}
	if p, ok := f.commits[parent]; ok {
		maps.Copy(tree, p.files)
	}
	for path, content := range files {
		if content == nil {
			delete(tree, path)
			continue
		}
		tree[path] = content
	}
	sha := f.next("sha")
	f.commits[sha] = &commit{parent: parent, files: tree}
	r.branches[branch] = sha
	return sha
}

func (f *Forge) Tag(repoName, tag, sha string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repo(repoName).tags[tag] = sha
}

func (f *Forge) AddChange(repoName string, ch forge.Change, files ...forge.File) forge.Change {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.repo(repoName)
	ch.Number = len(r.changes) + 1
	if ch.State == "" {
		ch.State = forge.Open
	}
	if ch.URL == "" {
		ch.URL = fmt.Sprintf("https://forge.test/%s/pull/%d", repoName, ch.Number)
	}
	r.changes = append(r.changes, &ch)
	r.changeFiles[ch.Number] = files
	return ch
}

func (f *Forge) LinkCommit(repoName, sha string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.repo(repoName)
	r.pullsByCommit[sha] = append(r.pullsByCommit[sha], n)
}

func (f *Forge) SetChecks(repoName, sha string, checks ...forge.Check) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repo(repoName).checks[sha] = checks
}

func (f *Forge) Change(repoName string, n int) forge.Change {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.repo(repoName)
	if n < 1 || n > len(r.changes) {
		return forge.Change{}
	}
	return *r.changes[n-1]
}

func (f *Forge) Changes(repoName string) []forge.Change {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []forge.Change
	for _, ch := range f.repo(repoName).changes {
		out = append(out, *ch)
	}
	return out
}

func (f *Forge) Tags(repoName string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.repo(repoName).tags)
}

func (f *Forge) CommentsOf(repoName string, n int) []forge.Comment {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.repo(repoName).comments[n])
}

func (f *Forge) AddComment(repoName string, n int, author, body string) forge.Comment {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addComment(repoName, n, author, body)
}

func (f *Forge) addComment(repoName string, n int, author, body string) forge.Comment {
	f.seq++
	c := forge.Comment{ID: int64(f.seq), Author: author, Body: body}
	r := f.repo(repoName)
	r.comments[n] = append(r.comments[n], c)
	return c
}

func (f *Forge) resolve(r *repo, ref string) (string, bool) {
	if sha, ok := r.branches[ref]; ok {
		return sha, true
	}
	if sha, ok := r.tags[ref]; ok {
		return sha, true
	}
	if _, ok := f.commits[ref]; ok {
		return ref, true
	}
	return "", false
}

func (f *Forge) known(name string) (*repo, error) {
	r, ok := f.repos[name]
	if !ok {
		return nil, fmt.Errorf("repository %s: %w", name, forge.ErrNotFound)
	}
	return r, nil
}

func (f *Forge) DefaultBranch(_ context.Context, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("DefaultBranch"); err != nil {
		return "", err
	}
	r, err := f.known(name)
	if err != nil {
		return "", err
	}
	return r.defaultBranch, nil
}

func (f *Forge) FileAt(_ context.Context, name, path, ref string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("FileAt"); err != nil {
		return nil, err
	}
	r, err := f.known(name)
	if err != nil {
		return nil, err
	}
	sha, ok := f.resolve(r, ref)
	if !ok {
		return nil, fmt.Errorf("ref %s: %w", ref, forge.ErrNotFound)
	}
	content, ok := f.commits[sha].files[path]
	if !ok {
		return nil, fmt.Errorf("%s at %s: %w", path, ref, forge.ErrNotFound)
	}
	return slices.Clone(content), nil
}

func (f *Forge) ResolveRef(_ context.Context, name, ref string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("ResolveRef"); err != nil {
		return "", err
	}
	r, err := f.known(name)
	if err != nil {
		return "", err
	}
	sha, ok := f.resolve(r, ref)
	if !ok {
		return "", fmt.Errorf("ref %s: %w", ref, forge.ErrNotFound)
	}
	return sha, nil
}

func (f *Forge) ChangedPaths(_ context.Context, name, base, head string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("ChangedPaths"); err != nil {
		return nil, err
	}
	r, err := f.known(name)
	if err != nil {
		return nil, err
	}
	b, okB := f.resolve(r, base)
	h, okH := f.resolve(r, head)
	if !okB || !okH {
		return nil, fmt.Errorf("compare %s...%s: %w", base, head, forge.ErrNotFound)
	}
	before, after := f.commits[b].files, f.commits[h].files
	var paths []string
	for p, c := range after {
		if old, ok := before[p]; !ok || string(old) != string(c) {
			paths = append(paths, p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func (f *Forge) OpenChanges(_ context.Context, name, headPrefix string) ([]forge.Change, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("OpenChanges"); err != nil {
		return nil, err
	}
	var out []forge.Change
	for _, ch := range f.repo(name).changes {
		if ch.State == forge.Open && strings.HasPrefix(ch.HeadRef, headPrefix) {
			out = append(out, *ch)
		}
	}
	return out, nil
}

func (f *Forge) ChangeByHead(_ context.Context, name, head string) (forge.Change, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("ChangeByHead"); err != nil {
		return forge.Change{}, false, err
	}
	changes := f.repo(name).changes
	for i := len(changes) - 1; i >= 0; i-- {
		if changes[i].HeadRef == head {
			return *changes[i], true, nil
		}
	}
	return forge.Change{}, false, nil
}

func (f *Forge) ChangesForCommit(_ context.Context, name, sha string) ([]forge.Change, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("ChangesForCommit"); err != nil {
		return nil, err
	}
	r := f.repo(name)
	var out []forge.Change
	for _, n := range r.pullsByCommit[sha] {
		out = append(out, *r.changes[n-1])
	}
	return out, nil
}

func (f *Forge) ChangeFiles(_ context.Context, name string, n int) ([]forge.File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("ChangeFiles"); err != nil {
		return nil, err
	}
	return slices.Clone(f.repo(name).changeFiles[n]), nil
}

func (f *Forge) Checks(_ context.Context, name, sha string) ([]forge.Check, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Checks"); err != nil {
		return nil, err
	}
	return slices.Clone(f.repo(name).checks[sha]), nil
}

func (f *Forge) Comments(_ context.Context, name string, n int) ([]forge.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Comments"); err != nil {
		return nil, err
	}
	return slices.Clone(f.repo(name).comments[n]), nil
}

func (f *Forge) Viewer(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("Viewer"); err != nil {
		return "", err
	}
	return f.User, nil
}
