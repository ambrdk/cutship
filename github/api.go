package github

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrNotFound = errors.New("github: not found")

type API struct {
	base  string
	token string
	http  *http.Client
}

type User struct {
	Login string `json:"login"`
}

type Comment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User User   `json:"user"`
}

type Ref struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type Pull struct {
	Number         int        `json:"number"`
	Title          string     `json:"title"`
	HTMLURL        string     `json:"html_url"`
	MergedAt       *time.Time `json:"merged_at"`
	MergeCommitSHA string     `json:"merge_commit_sha"`
	MergedBy       *User      `json:"merged_by"`
	Head           Ref        `json:"head"`
	Base           Ref        `json:"base"`
}

type Job struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

func New(baseURL, token string) *API {
	return &API{base: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

func (a *API) IssueComments(ctx context.Context, repo string, pr int) ([]Comment, error) {
	var out []Comment
	err := a.pages(ctx, fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100", repo, pr), func(raw []byte) error {
		var page []Comment
		if err := json.Unmarshal(raw, &page); err != nil {
			return err
		}
		out = append(out, page...)
		return nil
	})
	return out, err
}

func (a *API) CreateComment(ctx context.Context, repo string, pr int, body string) (Comment, error) {
	var c Comment
	err := a.json(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, pr), map[string]string{"body": body}, &c)
	return c, err
}

func (a *API) UpdateComment(ctx context.Context, repo string, id int64, body string) error {
	return a.json(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/issues/comments/%d", repo, id), map[string]string{"body": body}, nil)
}

func (a *API) PullsForCommit(ctx context.Context, repo, sha string) ([]Pull, error) {
	var out []Pull
	err := a.json(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/commits/%s/pulls", repo, sha), nil, &out)
	return out, err
}

func (a *API) Pull(ctx context.Context, repo string, n int) (Pull, error) {
	var p Pull
	err := a.json(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d", repo, n), nil, &p)
	return p, err
}

func (a *API) PullsByHead(ctx context.Context, repo, head string) ([]Pull, error) {
	var out []Pull
	err := a.json(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls?state=all&per_page=100&head=%s", repo, url.QueryEscape(head)), nil, &out)
	return out, err
}

func (a *API) PullFiles(ctx context.Context, repo string, n int) ([]string, error) {
	var out []string
	err := a.pages(ctx, fmt.Sprintf("/repos/%s/pulls/%d/files?per_page=100", repo, n), func(raw []byte) error {
		var page []struct {
			Filename string `json:"filename"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return err
		}
		for _, f := range page {
			out = append(out, f.Filename)
		}
		return nil
	})
	return out, err
}

func (a *API) FileAt(ctx context.Context, repo, path, ref string) ([]byte, error) {
	resp, err := a.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repo, path, url.QueryEscape(ref)), nil, "application/vnd.github.raw+json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

func (a *API) RunJobs(ctx context.Context, repo string, runID int64) ([]Job, error) {
	var out []Job
	err := a.pages(ctx, fmt.Sprintf("/repos/%s/actions/runs/%d/jobs?filter=latest&per_page=100", repo, runID), func(raw []byte) error {
		var page struct {
			Jobs []Job `json:"jobs"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return err
		}
		out = append(out, page.Jobs...)
		return nil
	})
	return out, err
}

func (a *API) RunArtifact(ctx context.Context, repo string, runID int64, name string) (map[string][]byte, error) {
	var list struct {
		Artifacts []struct {
			Name        string `json:"name"`
			DownloadURL string `json:"archive_download_url"`
		} `json:"artifacts"`
	}
	if err := a.json(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/actions/runs/%d/artifacts?name=%s", repo, runID, url.QueryEscape(name)), nil, &list); err != nil {
		return nil, err
	}
	for _, art := range list.Artifacts {
		if art.Name != name {
			continue
		}
		resp, err := a.do(ctx, http.MethodGet, art.DownloadURL, nil, "")
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		return unzip(raw)
	}
	return nil, fmt.Errorf("artifact %s of run %d: %w", name, runID, ErrNotFound)
}

func unzip(raw []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		out[f.Name] = body
	}
	return out, nil
}

func (a *API) pages(ctx context.Context, path string, each func([]byte) error) error {
	next := path
	for next != "" {
		resp, err := a.do(ctx, http.MethodGet, next, nil, "")
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return err
		}
		if err := each(raw); err != nil {
			return err
		}
		next = nextLink(resp.Header.Get("Link"))
	}
	return nil
}

func nextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		segs := strings.Split(part, ";")
		if len(segs) < 2 || strings.TrimSpace(segs[1]) != `rel="next"` {
			continue
		}
		return strings.Trim(strings.TrimSpace(segs[0]), "<>")
	}
	return ""
}

func (a *API) json(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	resp, err := a.do(ctx, method, path, body, "")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (a *API) do(ctx context.Context, method, path string, body io.Reader, accept string) (*http.Response, error) {
	target := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		target = a.base + path
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	if accept == "" {
		accept = "application/vnd.github+json"
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if strings.HasPrefix(target, a.base) {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%s %s: %w", method, path, ErrNotFound)
	}
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("github %s %s: %s %s", method, path, strconv.Itoa(resp.StatusCode), strings.TrimSpace(string(raw)))
	}
	return resp, nil
}
