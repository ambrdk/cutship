package telegram

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type Problem struct {
	Error   bool
	Message string
}

func chatRef(chat string) any {
	if id, err := strconv.ParseInt(chat, 10, 64); err == nil {
		return id
	}
	return chat
}

func Check(ctx context.Context, token, apiURL, chat string) []Problem {
	fail := func(format string, args ...any) []Problem {
		return []Problem{{Error: true, Message: fmt.Sprintf(format, args...)}}
	}
	opts := []bot.Option{bot.WithSkipGetMe()}
	if apiURL != "" {
		opts = append(opts, bot.WithServerURL(apiURL))
	}
	b, err := bot.New(token, opts...)
	if err != nil {
		return fail("cannot start the telegram client: %v", err)
	}
	me, err := b.GetMe(ctx)
	if err != nil {
		return fail("the bot token does not work: %v", err)
	}
	member, err := b.GetChatMember(ctx, &bot.GetChatMemberParams{ChatID: chatRef(chat), UserID: me.ID})
	if err != nil {
		return fail("the bot @%s cannot see chat %s: %v", me.Username, chat, err)
	}
	switch {
	case member.Owner != nil:
	case member.Administrator != nil && member.Administrator.CanPostMessages && member.Administrator.CanEditMessages:
	case member.Administrator != nil:
		return fail("the bot @%s is an admin of %s but cannot both post and edit messages", me.Username, chat)
	default:
		return fail("the bot @%s is not an admin of %s, it needs to post and edit messages", me.Username, chat)
	}
	info, err := b.GetChat(ctx, &bot.GetChatParams{ChatID: chatRef(chat)})
	if err != nil {
		return fail("cannot read chat %s: %v", chat, err)
	}
	if info.LinkedChatID == 0 {
		return []Problem{{Message: fmt.Sprintf("channel %s has no discussion group, posts go out without the comment with details", chat)}}
	}
	linked, err := b.GetChatMember(ctx, &bot.GetChatMemberParams{ChatID: info.LinkedChatID, UserID: me.ID})
	if err != nil || !admin(linked) {
		return fail("the bot @%s is not an admin of the discussion group %d of %s, it cannot see the copies of its posts", me.Username, info.LinkedChatID, chat)
	}
	return nil
}

func admin(m *models.ChatMember) bool {
	return m != nil && (m.Owner != nil || m.Administrator != nil)
}
