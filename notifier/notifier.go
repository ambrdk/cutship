package notifier

import (
	"context"
	"encoding/json"

	"github.com/ambrdk/cutship/render"
)

type Status string

const (
	OK           Status = "ok"
	Pending      Status = "pending"
	NoCopy       Status = "no_copy"
	NoDiscussion Status = "no_discussion"
	Unthreaded   Status = "unthreaded"
	PostGone     Status = "post_gone"
)

type Notifier interface {
	Key() string
	Publish(ctx context.Context, ref json.RawMessage, v render.View) (json.RawMessage, Status, error)
	Link(ref json.RawMessage) string
}

func Warning(s Status) string {
	switch s {
	case NoCopy:
		return "the post went out without a comment: its copy did not show up in the discussion group in time; check that the bot is an admin of the discussion group"
	case Unthreaded:
		return "the comment went to the discussion group without a reply, because the post's copy never showed up there"
	case NoDiscussion:
		return "the channel has no discussion group or it could not be read, posting without comments"
	case PostGone:
		return "the earlier post is gone, a new one was started"
	}
	return ""
}
