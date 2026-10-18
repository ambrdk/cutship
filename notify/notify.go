package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ambrdk/deploy-notify/event"
	"github.com/ambrdk/deploy-notify/release"
	"github.com/ambrdk/deploy-notify/render"
	"github.com/ambrdk/deploy-notify/source"
	"github.com/ambrdk/deploy-notify/store"
	"github.com/ambrdk/notifykit/telegram"
	"github.com/ambrdk/notifykit/thread"
)

type Messenger interface {
	Publish(ctx context.Context, ids thread.IDs, post, comment string) (thread.IDs, error)
}

func Handle(ctx context.Context, events []source.Anchored, st store.Store, m Messenger) error {
	var errs []error
	for _, a := range events {
		if err := handle(ctx, a, st, m); err != nil {
			errs = append(errs, fmt.Errorf("event %s: %w", a.Event.ID, err))
		}
	}
	return errors.Join(errs...)
}

func handle(ctx context.Context, a source.Anchored, st store.Store, m Messenger) error {
	if a.PR == 0 {
		if a.Event.Kind != event.DeployFinished {
			return nil
		}
		s, _ := release.Apply(release.State{}, a.Event)
		_, err := m.Publish(ctx, thread.IDs{}, render.Post(s), render.Comment(s))
		return err
	}
	rec, _, err := st.Load(ctx, a.PR)
	if err != nil {
		return err
	}
	s, changed := release.Apply(rec.State, a.Event)
	if !changed {
		return nil
	}
	ids, err := m.Publish(ctx, rec.IDs, render.Post(s), render.Comment(s))
	if err != nil {
		if ids != rec.IDs && ids.PostID != 0 {
			_ = st.Save(ctx, a.PR, store.Record{State: rec.State, IDs: ids})
		}
		return err
	}
	return st.Save(ctx, a.PR, store.Record{State: s, IDs: ids})
}

type commentWatch struct {
	m          Messenger
	log        *slog.Logger
	discussion bool
}

func WarnWithoutComment(m Messenger, log *slog.Logger, discussion bool) Messenger {
	return commentWatch{m: m, log: log, discussion: discussion}
}

func (w commentWatch) Publish(ctx context.Context, ids thread.IDs, post, comment string) (thread.IDs, error) {
	got, err := w.m.Publish(ctx, ids, post, comment)
	if err == nil && w.discussion && got.PostID != 0 && got.CommentID == 0 {
		w.log.Warn("the post went out without a comment: its copy did not show up in the discussion group in time; check that the bot is an admin of the discussion group", "post_id", got.PostID)
	}
	return got, err
}

type retrying struct {
	m       Messenger
	longest time.Duration
}

func Retry(m Messenger, longest time.Duration) Messenger {
	return retrying{m: m, longest: longest}
}

func (r retrying) Publish(ctx context.Context, ids thread.IDs, post, comment string) (thread.IDs, error) {
	got, err := r.m.Publish(ctx, ids, post, comment)
	var limited *telegram.RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter > r.longest {
		return got, err
	}
	select {
	case <-ctx.Done():
		return got, ctx.Err()
	case <-time.After(limited.RetryAfter):
	}
	return r.m.Publish(ctx, got, post, comment)
}
