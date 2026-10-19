package forge

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
	"time"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrTooMany      = errors.New("too many changed files")
	ErrTagElsewhere = errors.New("tag points at another commit")
)

type ChangeState string

const (
	Open   ChangeState = "open"
	Merged ChangeState = "merged"
	Closed ChangeState = "closed"
)

type Change struct {
	Number   int
	Title    string
	URL      string
	HeadRef  string
	HeadSHA  string
	MergeSHA string
	State    ChangeState
	MergedAt *time.Time
	MergedBy string
	Author   string
}

type File struct {
	Path   string
	Status string
	Patch  string
}

type CheckState string

const (
	Pending CheckState = "pending"
	Success CheckState = "success"
	Failure CheckState = "failure"
)

type Check struct {
	Name        string
	State       CheckState
	CompletedAt time.Time
}

type Comment struct {
	ID     int64
	Author string
	Body   string
}

type Reader interface {
	DefaultBranch(ctx context.Context, repo string) (string, error)
	FileAt(ctx context.Context, repo, path, ref string) ([]byte, error)
	ResolveRef(ctx context.Context, repo, ref string) (string, error)
	ChangedPaths(ctx context.Context, repo, base, head string) ([]string, error)
	Subtrees(ctx context.Context, repo, dir, ref string) (map[string]string, error)
	OpenChanges(ctx context.Context, repo, headPrefix string) ([]Change, error)
	ChangeByHead(ctx context.Context, repo, head string) (Change, bool, error)
	ChangesForCommit(ctx context.Context, repo, sha string) ([]Change, error)
	ChangeFiles(ctx context.Context, repo string, n int) ([]File, error)
	Checks(ctx context.Context, repo, sha string) ([]Check, error)
	Comments(ctx context.Context, repo string, n int) ([]Comment, error)
	Viewer(ctx context.Context) (string, error)
}

type ChangeRequest struct {
	Base    string
	Branch  string
	Title   string
	Message string
	Files   map[string][]byte
}

type Writer interface {
	CreateTag(ctx context.Context, repo, tag, sha string) error
	PushChange(ctx context.Context, repo string, req ChangeRequest) (Change, error)
	CloseChange(ctx context.Context, repo string, n int, deleteBranch bool) error
	CreateComment(ctx context.Context, repo string, n int, body string) (Comment, error)
	UpdateComment(ctx context.Context, repo string, id int64, body string) error
}

type AutoMerger interface {
	AutoMerge(ctx context.Context, repo string, n int) error
}

type Forge interface {
	Reader
	Writer
}

type RepoSettings struct {
	Push                *bool
	AutoMerge           *bool
	DeleteBranchOnMerge *bool
}

type Settings interface {
	RepoSettings(ctx context.Context, repo string) (RepoSettings, error)
}

type DeployState string

const (
	DeployInProgress DeployState = "in_progress"
	DeploySuccess    DeployState = "success"
	DeployFailure    DeployState = "failure"
	DeployInactive   DeployState = "inactive"
)

type Deployment struct {
	ID          int64
	Environment string
	SHA         string
	State       DeployState
	CreatedAt   time.Time
	Payload     []byte
}

type Deployments interface {
	CreateDeployment(ctx context.Context, repo, environment, sha string, payload []byte) (Deployment, error)
	SetDeploymentState(ctx context.Context, repo string, id int64, state DeployState, logURL string) error
	ListDeployments(ctx context.Context, repo, environment string) iter.Seq2[Deployment, error]
}

func SplitRepo(repo string) (string, string, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("repository %q must be owner/name", repo)
	}
	return owner, name, nil
}

func Latest(checks []Check, name string) (Check, bool) {
	var best Check
	found := false
	for _, c := range checks {
		if c.Name != name {
			continue
		}
		if !found || c.State == Pending || (best.State != Pending && c.CompletedAt.After(best.CompletedAt)) {
			best, found = c, true
		}
	}
	return best, found
}
