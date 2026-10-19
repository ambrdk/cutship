package check

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ambrdk/cutship/config"
	"github.com/ambrdk/cutship/cut"
	"github.com/ambrdk/cutship/forge"
	"github.com/ambrdk/cutship/forge/github"
	"github.com/ambrdk/cutship/notifier/telegram"
	"github.com/ambrdk/cutship/pin"
	"github.com/ambrdk/cutship/registry"
)

const (
	Error   = "error"
	Warning = "warning"
)

type Finding struct {
	Level   string
	Path    string
	Message string
}

type findings []Finding

func (fs *findings) add(level, path, format string, args ...any) {
	*fs = append(*fs, Finding{Level: level, Path: path, Message: fmt.Sprintf(format, args...)})
}

func Online(ctx context.Context, f forge.Forge, reg registry.Registry, cfg config.Config, local []byte) []Finding {
	var out findings
	repo := cfg.DeployRepo
	branch, err := f.DefaultBranch(ctx, repo)
	if err != nil {
		out.add(Error, "deploy_repo", "cannot read %s with this token: %v", repo, err)
		return out
	}
	remote, err := f.FileAt(ctx, repo, cut.ConfigPath, branch)
	switch {
	case errors.Is(err, forge.ErrNotFound):
		out.add(Error, "", "%s is not on %s of %s, cutship release reads it from there", cut.ConfigPath, branch, repo)
	case err != nil:
		out.add(Error, "", "cannot read %s from %s: %v", cut.ConfigPath, repo, err)
	case !bytes.Equal(remote, local):
		out.add(Warning, "", "the local %s differs from the one on %s of %s, cutship release uses that one", cut.ConfigPath, branch, repo)
	}
	for i, app := range cfg.Apps {
		path := fmt.Sprintf("apps[%d]", i)
		content, err := f.FileAt(ctx, repo, pin.Path(app), branch)
		if err != nil {
			out.add(Error, path, "cannot read %s on %s: %v", pin.Path(app), branch, err)
			continue
		}
		ref, ok := pin.Line(content)
		if !ok {
			out.add(Error, path, "no top-level image in %s", pin.Path(app))
			continue
		}
		if err := pin.VerifyRef(cfg, app, ref); err != nil {
			out.add(Error, path, "%v", err)
			continue
		}
		image, tag := pin.ParseRef(ref)
		if _, err := reg.Revision(ctx, image, tag); errors.Is(err, registry.ErrMissing) {
			out.add(Error, path, "the pinned image %s is not in the registry", ref)
		} else if err != nil {
			out.add(Warning, path, "cannot read the pinned image %s: %v", ref, err)
		}
	}
	if login, err := f.Viewer(ctx); err != nil {
		out.add(Warning, "", "cannot tell who owns the token: %v", err)
	} else if login == github.ActionsBot {
		out.add(Warning, "", "the token is a GITHUB_TOKEN: a bump it opens does not start CI, set CUTSHIP_DEPLOY_TOKEN to a token of a user or an app")
	}
	if s, ok := f.(forge.Settings); !ok {
		out.add(Warning, "", "cannot read the settings of %s on this forge", repo)
	} else if settings, err := s.RepoSettings(ctx, repo); err != nil {
		out.add(Warning, "", "cannot read the settings of %s: %v", repo, err)
	} else {
		switch {
		case settings.Push == nil:
			out.add(Warning, "", "cannot tell whether the token may push to %s", repo)
		case !*settings.Push:
			out.add(Error, "", "the token cannot push to %s, bump branches need write access to contents and pull requests", repo)
		}
		if settings.AutoMerge == nil || !*settings.AutoMerge {
			out.add(Warning, "", "auto-merge is off or unreadable in %s: bumps wait for a manual merge, turn on Allow auto-merge", repo)
		}
		if settings.DeleteBranchOnMerge == nil || !*settings.DeleteBranchOnMerge {
			out.add(Warning, "", "delete branch on merge is off or unreadable in %s: merged bump branches stay", repo)
		}
	}
	if _, ok := f.(forge.Deployments); !ok {
		out.add(Error, "", "this forge has no deployments, cutship deploy needs them for the ledger")
	}
	for i, n := range cfg.Notify {
		path := fmt.Sprintf("notify[%d]", i)
		if n.Type != "telegram" {
			continue
		}
		token := os.Getenv("TELEGRAM_BOT_TOKEN")
		if token == "" {
			out.add(Error, path, "TELEGRAM_BOT_TOKEN is not set, the release posts cannot go out")
			continue
		}
		for _, p := range telegramCheck(ctx, token, os.Getenv("TELEGRAM_API_URL"), n.ChatID) {
			level := Warning
			if p.Error {
				level = Error
			}
			out.add(level, path, "%s", p.Message)
		}
	}
	return out
}

var telegramCheck = telegram.Check
