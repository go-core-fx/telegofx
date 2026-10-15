package fsm

import (
	"context"
	"strings"

	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	"go.uber.org/zap"
)

// StateEmpty is the state of a chat that has no stored FSM state. It doubles
// as the empty-string sentinel a Storage returns for a missing state.
const StateEmpty = ""

// StateEqual matches updates whose chat/user pair is in the given FSM
// state. The state is read INSIDE the predicate on every evaluation: an
// update without a message or without a sender never matches and never
// touches the store (edited_message, channel_post and callback_query create
// no FSM state).
//
// Updates sent on behalf of a chat (Message.SenderChat set) never match:
// Telegram then reports a fake sender user, so all such messages of that
// chat would otherwise share one FSM state.
func StateEqual(store *Store, state string) th.Predicate {
	return func(ctx context.Context, update telego.Update) bool {
		if !hasUserSender(update) {
			return false
		}

		chatID, userID := update.Message.Chat.ID, update.Message.From.ID
		got, err := store.Get(ctx, chatID, userID)
		if err != nil {
			store.logger.Error("get fsm state",
				zap.Int64("chat_id", chatID),
				zap.Int64("user_id", userID),
				zap.Error(err),
			)
			return false
		}
		return got == state
	}
}

// StatePrefix matches updates whose chat/user pair is in an FSM state that
// starts with the given prefix. The state is read INSIDE the predicate on
// every evaluation, and a missing state never matches: an empty prefix
// matches every state but the empty one.
//
// Updates sent on behalf of a chat (Message.SenderChat set) never match:
// Telegram then reports a fake sender user, so all such messages of that
// chat would otherwise share one FSM state.
func StatePrefix(store *Store, prefix string) th.Predicate {
	return func(ctx context.Context, update telego.Update) bool {
		if !hasUserSender(update) {
			return false
		}

		chatID, userID := update.Message.Chat.ID, update.Message.From.ID
		got, err := store.Get(ctx, chatID, userID)
		if err != nil {
			store.logger.Error("get fsm state",
				zap.Int64("chat_id", chatID),
				zap.Int64("user_id", userID),
				zap.Error(err),
			)
			return false
		}
		return got != StateEmpty && strings.HasPrefix(got, prefix)
	}
}

// hasUserSender reports whether the update carries a message sent by a real
// user: a message with a sender that is not a chat acting on its own behalf.
// Telegram reports a fake sender user for messages sent on behalf of a chat,
// so those messages are excluded to keep the state per real user.
func hasUserSender(update telego.Update) bool {
	return update.Message != nil && update.Message.From != nil && update.Message.SenderChat == nil
}
