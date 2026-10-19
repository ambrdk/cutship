package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ambrdk/cutship/notifier"
	"github.com/ambrdk/cutship/render"
	"github.com/ambrdk/notifykit/telegram"
	"github.com/ambrdk/notifykit/thread"
)

const copyWait = 30 * time.Second

type publisher interface {
	Publish(ctx context.Context, ids thread.IDs, p thread.Post) (thread.Outcome, error)
}

type Channel struct {
	chat string
	th   publisher
}

func NewChannel(chat string, th publisher) *Channel {
	return &Channel{chat: chat, th: th}
}

func Open(token, apiURL, chat string, log *slog.Logger) (*Channel, error) {
	tg, err := telegram.New(token, apiURL, log)
	if err != nil {
		return nil, err
	}
	return NewChannel(chat, thread.New(tg, chat, thread.Waiting(tg, copyWait))), nil
}

func (c *Channel) Key() string {
	return "telegram:" + c.chat
}

func status(s thread.Status) notifier.Status {
	switch s {
	case thread.CopyPending:
		return notifier.Pending
	case thread.NoCopy:
		return notifier.NoCopy
	case thread.NoDiscussion:
		return notifier.NoDiscussion
	case thread.Unthreaded:
		return notifier.Unthreaded
	case thread.PostGone:
		return notifier.PostGone
	}
	return notifier.OK
}

func (c *Channel) Publish(ctx context.Context, ref json.RawMessage, v render.View) (json.RawMessage, notifier.Status, error) {
	var ids thread.IDs
	if len(ref) > 0 {
		if err := json.Unmarshal(ref, &ids); err != nil {
			return nil, notifier.OK, fmt.Errorf("read the telegram message ids: %w", err)
		}
	}
	out, err := c.th.Publish(ctx, ids, thread.Post{
		Text:    Post(v),
		Comment: func(skip int) string { return Comment(v, skip) },
		Loud:    v.Loud,
		Final:   v.Final,
	})
	var raw json.RawMessage
	if out.IDs.PostID != 0 {
		raw, _ = json.Marshal(out.IDs)
	}
	return raw, status(out.Status), err
}

func (c *Channel) Done(ref json.RawMessage) bool {
	var ids thread.IDs
	return len(ref) > 0 && json.Unmarshal(ref, &ids) == nil && ids.PostID != 0 && ids.CommentID != 0
}

func (c *Channel) Link(ref json.RawMessage) string {
	var ids thread.IDs
	if len(ref) == 0 || json.Unmarshal(ref, &ids) != nil || ids.PostID == 0 {
		return ""
	}
	internal, ok := strings.CutPrefix(c.chat, "-100")
	if !ok {
		return ""
	}
	return fmt.Sprintf("https://t.me/c/%s/%d", internal, ids.PostID)
}
