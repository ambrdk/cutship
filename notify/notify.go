package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ambrdk/cutship/event"
	"github.com/ambrdk/cutship/release"
	"github.com/ambrdk/cutship/render"
	"github.com/ambrdk/cutship/source"
	"github.com/ambrdk/cutship/store"
	"github.com/ambrdk/notifykit/thread"
)

type Messenger interface {
	Publish(ctx context.Context, ids thread.IDs, p thread.Post) (thread.Outcome, error)
}

func Handle(ctx context.Context, events []source.Anchored, st store.Store, m Messenger, log *slog.Logger) error {
	var errs []error
	for _, a := range events {
		if err := handle(ctx, a, st, m, log); err != nil {
			errs = append(errs, fmt.Errorf("event %s: %w", a.Event.ID, err))
		}
	}
	return errors.Join(errs...)
}

func handle(ctx context.Context, a source.Anchored, st store.Store, m Messenger, log *slog.Logger) error {
	if a.PR == 0 {
		if a.Event.Kind != event.DeployFinished {
			return nil
		}
		s, _ := release.Apply(release.State{}, a.Event)
		out, err := m.Publish(ctx, thread.IDs{}, post(s))
		report(log, out)
		return err
	}
	rec, _, err := st.Load(ctx, a.PR)
	if err != nil {
		return err
	}
	s, changed := release.Apply(rec.State, a.Event)
	if !changed && (rec.IDs.PostID == 0 || rec.IDs.CommentID != 0) {
		return nil
	}
	out, err := m.Publish(ctx, rec.IDs, post(s))
	if err != nil {
		if out.IDs != rec.IDs && out.IDs.PostID != 0 {
			_ = st.Save(ctx, a.PR, store.Record{State: rec.State, IDs: out.IDs})
		}
		return err
	}
	report(log, out)
	return st.Save(ctx, a.PR, store.Record{State: s, IDs: out.IDs})
}

func post(s release.State) thread.Post {
	return thread.Post{Text: render.Post(s), Comment: func(skip int) string { return render.Comment(s, skip) }}
}

func report(log *slog.Logger, out thread.Outcome) {
	switch out.Status {
	case thread.NoCopy:
		log.Warn("the post went out without a comment: its copy did not show up in the discussion group in time; check that the bot is an admin of the discussion group", "post_id", out.IDs.PostID)
	case thread.Unthreaded:
		log.Warn("the comment went to the discussion group without a reply, because the post's copy never showed up there", "post_id", out.IDs.PostID)
	case thread.NoDiscussion:
		log.Warn("the channel has no discussion group or it could not be read, posting without comments", "post_id", out.IDs.PostID)
	}
}
