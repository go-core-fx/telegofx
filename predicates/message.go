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

func MessageWithContact() th.Predicate {
	return func(_ context.Context, update telego.Update) bool {
		return update.Message != nil && update.Message.Contact != nil
	}
}

func MessageWithUsersShared() th.Predicate {
	return func(_ context.Context, update telego.Update) bool {
		return update.Message != nil && update.Message.UsersShared != nil
	}
}
