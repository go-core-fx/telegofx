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
// update without a message or without a sender never
// matches and never touches the store (edited_message, channel_post and
// callback_query create no FSM state).
func StateEqual(store *Store, state string) th.Predicate {
	return func(ctx context.Context, update telego.Update) bool {
		if update.Message == nil || update.Message.From == nil {
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

func StatePrefix(store *Store, prefix string) th.Predicate {
	return func(ctx context.Context, update telego.Update) bool {
		if update.Message == nil || update.Message.From == nil {
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
