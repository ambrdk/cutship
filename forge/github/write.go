package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/ambrdk/cutship/forge"
	gh "github.com/google/go-github/v92/github"
)

func (c *Client) refSHA(ctx context.Context, owner, name, ref string) (string, bool, error) {
	r, _, err := c.gh.Git.GetRef(ctx, owner, name, ref)
	if statusOf(err) == http.StatusNotFound {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return r.GetObject().GetSHA(), true, nil
}

func (c *Client) tagTarget(ctx context.Context, owner, name, tag string) (string, bool, error) {
	r, _, err := c.gh.Git.GetRef(ctx, owner, name, "tags/"+tag)
	if statusOf(err) == http.StatusNotFound {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	obj := r.GetObject()
	if obj.GetType() != "tag" {
		return obj.GetSHA(), true, nil
	}
	annotated, _, err := c.gh.Git.GetTag(ctx, owner, name, obj.GetSHA())
	if err != nil {
		return "", false, err
	}
	return annotated.GetObject().GetSHA(), true, nil
}

func (c *Client) CreateTag(ctx context.Context, full, tag, sha string) error {
	owner, name, err := repo(full)
	if err != nil {
		return err
	}
	check := func() (bool, error) {
		target, ok, err := c.tagTarget(ctx, owner, name, tag)
		if err != nil || !ok {
			return false, err
		}
		if target != sha {
			return true, fmt.Errorf("%s in %s points at %s, not %s: %w", tag, full, target, sha, forge.ErrTagElsewhere)
		}
		return true, nil
	}
	if done, err := check(); done || err != nil {
		return err
	}
	_, _, err = c.gh.Git.CreateRef(ctx, owner, name, gh.CreateRef{Ref: "refs/tags/" + tag, SHA: sha})
	if statusOf(err) == http.StatusUnprocessableEntity {
		if done, cerr := check(); done || cerr != nil {
			return cerr
		}
	}
	return err
}

func (c *Client) PushChange(ctx context.Context, full string, req forge.ChangeRequest) (forge.Change, error) {
	owner, name, err := repo(full)
	if err != nil {
		return forge.Change{}, err
	}
	baseSHA, ok, err := c.refSHA(ctx, owner, name, "heads/"+req.Base)
	if err != nil {
		return forge.Change{}, err
	}
	if !ok {
		return forge.Change{}, fmt.Errorf("branch %s in %s: %w", req.Base, full, forge.ErrNotFound)
	}
	base, _, err := c.gh.Git.GetCommit(ctx, owner, name, baseSHA)
	if err != nil {
		return forge.Change{}, err
	}
	paths := make([]string, 0, len(req.Files))
	for p := range req.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	entries := make([]*gh.TreeEntry, 0, len(paths))
	for _, p := range paths {
		entries = append(entries, &gh.TreeEntry{Path: new(p), Mode: new("100644"), Type: new("blob"), Content: new(string(req.Files[p]))})
	}
	tree, _, err := c.gh.Git.CreateTree(ctx, owner, name, base.GetTree().GetSHA(), entries)
	if err != nil {
		return forge.Change{}, err
	}
	head, exists, err := c.refSHA(ctx, owner, name, "heads/"+req.Branch)
	if err != nil {
		return forge.Change{}, err
	}
	same := false
	if exists {
		current, _, err := c.gh.Git.GetCommit(ctx, owner, name, head)
		if err != nil {
			return forge.Change{}, err
		}
		same = current.GetTree().GetSHA() == tree.GetSHA()
	}
	if !same {
		commit, _, err := c.gh.Git.CreateCommit(ctx, owner, name, gh.Commit{
			Message: new(req.Message),
			Tree:    &gh.Tree{SHA: tree.SHA},
			Parents: []*gh.Commit{{SHA: new(baseSHA)}},
		}, nil)
		if err != nil {
			return forge.Change{}, err
		}
		head = commit.GetSHA()
		if exists {
			_, _, err = c.gh.Git.UpdateRef(ctx, owner, name, "heads/"+req.Branch, gh.UpdateRef{SHA: head, Force: new(true)})
		} else {
			_, _, err = c.gh.Git.CreateRef(ctx, owner, name, gh.CreateRef{Ref: "refs/heads/" + req.Branch, SHA: head})
		}
		if err != nil {
			return forge.Change{}, err
		}
	}
	current, found, err := c.ChangeByHead(ctx, full, req.Branch)
	if err != nil {
		return forge.Change{}, err
	}
	if found && current.State == forge.Open {
		current.HeadSHA = head
		return current, nil
	}
	pr, _, err := c.gh.PullRequests.Create(ctx, owner, name, gh.CreatePullRequest{
		Title: new(req.Title),
		Head:  req.Branch,
		Base:  req.Base,
		Body:  new(""),
	})
	if err != nil {
		return forge.Change{}, err
	}
	return change(pr), nil
}

func (c *Client) CloseChange(ctx context.Context, full string, n int, deleteBranch bool) error {
	owner, name, err := repo(full)
	if err != nil {
		return err
	}
	pr, _, err := c.gh.PullRequests.Get(ctx, owner, name, n)
	if err != nil {
		return wrap(err)
	}
	if _, _, err := c.gh.PullRequests.Edit(ctx, owner, name, n, &gh.PullRequest{State: new("closed")}); err != nil {
		return err
	}
	if !deleteBranch {
		return nil
	}
	_, err = c.gh.Git.DeleteRef(ctx, owner, name, "heads/"+pr.GetHead().GetRef())
	if code := statusOf(err); code == http.StatusNotFound || code == http.StatusUnprocessableEntity {
		return nil
	}
	return err
}

func (c *Client) CreateComment(ctx context.Context, full string, n int, body string) (forge.Comment, error) {
	owner, name, err := repo(full)
	if err != nil {
		return forge.Comment{}, err
	}
	cm, _, err := c.gh.Issues.CreateComment(ctx, owner, name, n, gh.IssueCommentRequest{Body: body})
	if err != nil {
		return forge.Comment{}, wrap(err)
	}
	return forge.Comment{ID: cm.GetID(), Author: cm.GetUser().GetLogin(), Body: cm.GetBody()}, nil
}

func (c *Client) UpdateComment(ctx context.Context, full string, id int64, body string) error {
	owner, name, err := repo(full)
	if err != nil {
		return err
	}
	_, _, err = c.gh.Issues.UpdateComment(ctx, owner, name, id, gh.IssueCommentRequest{Body: body})
	return wrap(err)
}

const enableAutoMerge = `mutation($id: ID!) { enablePullRequestAutoMerge(input: {pullRequestId: $id, mergeMethod: SQUASH}) { clientMutationId } }`

func (c *Client) AutoMerge(ctx context.Context, full string, n int) error {
	owner, name, err := repo(full)
	if err != nil {
		return err
	}
	pr, _, err := c.gh.PullRequests.Get(ctx, owner, name, n)
	if err != nil {
		return wrap(err)
	}
	req, err := c.gh.NewRequest(ctx, http.MethodPost, c.graphql, map[string]any{
		"query":     enableAutoMerge,
		"variables": map[string]string{"id": pr.GetNodeID()},
	})
	if err != nil {
		return err
	}
	var out struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if _, err := c.gh.Do(req, &out); err != nil {
		return err
	}
	if len(out.Errors) == 0 {
		return nil
	}
	msg := out.Errors[0].Message
	switch {
	case strings.Contains(msg, "clean status"):
		_, _, err := c.gh.PullRequests.Merge(ctx, owner, name, n, "", &gh.PullRequestOptions{MergeMethod: "squash"})
		return err
	case strings.Contains(msg, "not allowed"):
		return fmt.Errorf("auto-merge of %s#%d: %s: turn on Allow auto-merge in the repository settings", full, n, msg)
	}
	return errors.New("auto-merge of " + full + ": " + msg)
}
