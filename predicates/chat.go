package predicates

import (
	"context"

	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
)

func MessageWithChatType(chatType string) th.Predicate {
	return func(_ context.Context, update telego.Update) bool {
		return update.Message != nil && update.Message.Chat.Type == chatType
	}
}
