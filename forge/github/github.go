package github

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ambrdk/cutship/forge"
	gh "github.com/google/go-github/v92/github"
)

const ActionsBot = "github-actions[bot]"

type Client struct {
	gh      *gh.Client
	graphql string
}

func New(baseURL, token string) (*Client, error) {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	base := strings.TrimRight(baseURL, "/") + "/"
	c, err := gh.NewClient(gh.WithAuthToken(token), gh.WithURLs(&base, &base))
	if err != nil {
		return nil, err
	}
	graphql := base + "graphql"
	if prefix, ok := strings.CutSuffix(base, "/api/v3/"); ok {
		graphql = prefix + "/api/graphql"
	}
	return &Client{gh: c, graphql: graphql}, nil
}

func repo(full string) (string, string, error) {
	return forge.SplitRepo(full)
}

func statusOf(err error) int {
	var e *gh.ErrorResponse
	if errors.As(err, &e) && e.Response != nil {
		return e.Response.StatusCode
	}
	return 0
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	if statusOf(err) == http.StatusNotFound {
		return errors.Join(forge.ErrNotFound, err)
	}
	return err
}

func change(p *gh.PullRequest) forge.Change {
	c := forge.Change{
		Number:   p.GetNumber(),
		Title:    p.GetTitle(),
		URL:      p.GetHTMLURL(),
		HeadRef:  p.GetHead().GetRef(),
		HeadSHA:  p.GetHead().GetSHA(),
		MergeSHA: p.GetMergeCommitSHA(),
		MergedBy: p.GetMergedBy().GetLogin(),
		Author:   p.GetUser().GetLogin(),
		State:    forge.ChangeState(p.GetState()),
	}
	if p.MergedAt != nil {
		at := p.MergedAt.Time
		c.MergedAt = &at
		c.State = forge.Merged
	}
	return c
}
