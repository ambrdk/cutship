package github

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/ambrdk/cutship/forge"
	gh "github.com/google/go-github/v92/github"
)

const (
	perPage      = 100
	compareLimit = 300
)

func (c *Client) DefaultBranch(ctx context.Context, full string) (string, error) {
	owner, name, err := repo(full)
	if err != nil {
		return "", err
	}
	r, _, err := c.gh.Repositories.Get(ctx, owner, name)
	if err != nil {
		return "", wrap(err)
	}
	return r.GetDefaultBranch(), nil
}

func (c *Client) RepoSettings(ctx context.Context, full string) (forge.RepoSettings, error) {
	owner, name, err := repo(full)
	if err != nil {
		return forge.RepoSettings{}, err
	}
	r, _, err := c.gh.Repositories.Get(ctx, owner, name)
	if err != nil {
		return forge.RepoSettings{}, wrap(err)
	}
	s := forge.RepoSettings{AutoMerge: r.AllowAutoMerge, DeleteBranchOnMerge: r.DeleteBranchOnMerge}
	if r.Permissions != nil {
		s.Push = r.Permissions.Push
	}
	return s, nil
}

func (c *Client) FileAt(ctx context.Context, full, path, ref string) ([]byte, error) {
	owner, name, err := repo(full)
	if err != nil {
		return nil, err
	}
	file, _, _, err := c.gh.Repositories.GetContents(ctx, owner, name, path, &gh.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		return nil, wrap(err)
	}
	if file == nil {
		return nil, fmt.Errorf("%s in %s is a directory", path, full)
	}
	content, err := file.GetContent()
	if err != nil {
		return nil, err
	}
	return []byte(content), nil
}

func (c *Client) ResolveRef(ctx context.Context, full, ref string) (string, error) {
	owner, name, err := repo(full)
	if err != nil {
		return "", err
	}
	sha, _, err := c.gh.Repositories.GetCommitSHA1(ctx, owner, name, ref, "")
	return strings.TrimSpace(sha), wrap(err)
}

func (c *Client) ChangedPaths(ctx context.Context, full, base, head string) ([]string, error) {
	owner, name, err := repo(full)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	opts := &gh.ListOptions{PerPage: perPage}
	for {
		cmp, resp, err := c.gh.Repositories.CompareCommits(ctx, owner, name, base, head, opts)
		if err != nil {
			return nil, wrap(err)
		}
		if len(cmp.Files) >= compareLimit {
			return nil, fmt.Errorf("%s..%s in %s: %w, the compare API lists at most %d", base, head, full, forge.ErrTooMany, compareLimit)
		}
		for _, f := range cmp.Files {
			seen[f.GetFilename()] = true
			if prev := f.GetPreviousFilename(); prev != "" {
				seen[prev] = true
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

func (c *Client) pulls(ctx context.Context, full string, opts *gh.PullRequestListOptions) ([]forge.Change, error) {
	owner, name, err := repo(full)
	if err != nil {
		return nil, err
	}
	opts.PerPage = perPage
	var out []forge.Change
	for {
		page, resp, err := c.gh.PullRequests.List(ctx, owner, name, opts)
		if err != nil {
			return nil, wrap(err)
		}
		for _, p := range page {
			out = append(out, change(p))
		}
		if resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

func (c *Client) OpenChanges(ctx context.Context, full, headPrefix string) ([]forge.Change, error) {
	all, err := c.pulls(ctx, full, &gh.PullRequestListOptions{State: "open"})
	if err != nil {
		return nil, err
	}
	var out []forge.Change
	for _, ch := range all {
		if strings.HasPrefix(ch.HeadRef, headPrefix) {
			out = append(out, ch)
		}
	}
	return out, nil
}

func (c *Client) ChangeByHead(ctx context.Context, full, head string) (forge.Change, bool, error) {
	owner, _, err := repo(full)
	if err != nil {
		return forge.Change{}, false, err
	}
	all, err := c.pulls(ctx, full, &gh.PullRequestListOptions{State: "all", Head: owner + ":" + head, Sort: "created", Direction: "desc"})
	if err != nil || len(all) == 0 {
		return forge.Change{}, false, err
	}
	return all[0], true, nil
}

func (c *Client) ChangesForCommit(ctx context.Context, full, sha string) ([]forge.Change, error) {
	owner, name, err := repo(full)
	if err != nil {
		return nil, err
	}
	opts := &gh.ListOptions{PerPage: perPage}
	var out []forge.Change
	for {
		page, resp, err := c.gh.PullRequests.ListPullRequestsWithCommit(ctx, owner, name, sha, opts)
		if err != nil {
			return nil, wrap(err)
		}
		for _, p := range page {
			out = append(out, change(p))
		}
		if resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

func (c *Client) ChangeFiles(ctx context.Context, full string, n int) ([]forge.File, error) {
	owner, name, err := repo(full)
	if err != nil {
		return nil, err
	}
	opts := &gh.ListOptions{PerPage: perPage}
	var out []forge.File
	for {
		page, resp, err := c.gh.PullRequests.ListFiles(ctx, owner, name, n, opts)
		if err != nil {
			return nil, wrap(err)
		}
		for _, f := range page {
			out = append(out, forge.File{Path: f.GetFilename(), Status: f.GetStatus(), Patch: f.GetPatch()})
		}
		if resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

func runState(r *gh.CheckRun) forge.CheckState {
	if r.GetStatus() != "completed" {
		return forge.Pending
	}
	switch r.GetConclusion() {
	case "success", "neutral", "skipped":
		return forge.Success
	}
	return forge.Failure
}

func statusState(s *gh.RepoStatus) forge.CheckState {
	switch s.GetState() {
	case "success":
		return forge.Success
	case "pending":
		return forge.Pending
	}
	return forge.Failure
}

func (c *Client) Checks(ctx context.Context, full, sha string) ([]forge.Check, error) {
	owner, name, err := repo(full)
	if err != nil {
		return nil, err
	}
	var out []forge.Check
	runOpts := &gh.ListCheckRunsOptions{ListOptions: gh.ListOptions{PerPage: perPage}}
	for {
		page, resp, err := c.gh.Checks.ListCheckRunsForRef(ctx, owner, name, sha, runOpts)
		if err != nil {
			return nil, wrap(err)
		}
		for _, r := range page.CheckRuns {
			out = append(out, forge.Check{Name: r.GetName(), State: runState(r), CompletedAt: r.GetCompletedAt().Time})
		}
		if resp.NextPage == 0 {
			break
		}
		runOpts.Page = resp.NextPage
	}
	statusOpts := &gh.ListOptions{PerPage: perPage}
	for {
		combined, resp, err := c.gh.Repositories.GetCombinedStatus(ctx, owner, name, sha, statusOpts)
		if err != nil {
			return nil, wrap(err)
		}
		for _, s := range combined.Statuses {
			check := forge.Check{Name: s.GetContext(), State: statusState(s)}
			if check.State != forge.Pending {
				check.CompletedAt = s.GetUpdatedAt().Time
			}
			out = append(out, check)
		}
		if resp.NextPage == 0 {
			return out, nil
		}
		statusOpts.Page = resp.NextPage
	}
}

func (c *Client) Comments(ctx context.Context, full string, n int) ([]forge.Comment, error) {
	owner, name, err := repo(full)
	if err != nil {
		return nil, err
	}
	opts := &gh.IssueListCommentsOptions{ListOptions: gh.ListOptions{PerPage: perPage}}
	var out []forge.Comment
	for {
		page, resp, err := c.gh.Issues.ListComments(ctx, owner, name, n, opts)
		if err != nil {
			return nil, wrap(err)
		}
		for _, cm := range page {
			out = append(out, forge.Comment{ID: cm.GetID(), Author: cm.GetUser().GetLogin(), Body: cm.GetBody()})
		}
		if resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

func (c *Client) Viewer(ctx context.Context) (string, error) {
	u, _, err := c.gh.Users.Get(ctx, "")
	if statusOf(err) == http.StatusForbidden {
		return ActionsBot, nil
	}
	if err != nil {
		return "", err
	}
	return u.GetLogin(), nil
}
