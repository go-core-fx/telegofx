package predicates_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-core-fx/telegofx/predicates"
	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
)

// decodeUpdate builds an update the way Telegram delivers it.
//
// The fixtures are JSON because a telego Update literal would have to spell out
// every field of the struct, and because the wire form is what a bot receives.
func decodeUpdate(t *testing.T, raw string) telego.Update {
	t.Helper()

	var update telego.Update
	if err := json.Unmarshal([]byte(raw), &update); err != nil {
		t.Fatalf("unmarshal update %s: %v", raw, err)
	}

	return update
}

// TestMessageWithReplyToMessage pins the whole truth table of the predicate: a
// message that answers another message is a reply, and nothing else is.
//
// The negative cases are the point. A message without a sender is the shape that
// reaches a handler unguarded, and an update without a message is the shape that
// would panic on an unguarded field access. Both are pinned here so a future
// telego or telegohandler change cannot silently widen the predicate.
func TestMessageWithReplyToMessage(t *testing.T) {
	t.Parallel()

	const replyUpdate = `{"update_id":1,"message":{"message_id":10,` +
		`"from":{"id":42,"is_bot":false,"first_name":"Operator"},` +
		`"chat":{"id":-100,"type":"supergroup"},"text":"answer",` +
		`"reply_to_message":{"message_id":7,"from":{"id":1,"is_bot":true,"first_name":"Bridge"},` +
		`"chat":{"id":-100,"type":"supergroup"},"date":1700000000,"text":"forwarded"}}}`

	const mediaReplyUpdate = `{"update_id":2,"message":{"message_id":10,` +
		`"from":{"id":42,"is_bot":false,"first_name":"Operator"},` +
		`"chat":{"id":42,"type":"private"},` +
		`"photo":[{"file_id":"abc","file_unique_id":"u1","width":90,"height":90}],` +
		`"reply_to_message":{"message_id":7,"chat":{"id":42,"type":"private"},` +
		`"date":1700000000,"text":"forwarded"}}}`

	const botReplyUpdate = `{"update_id":3,"message":{"message_id":10,` +
		`"from":{"id":1,"is_bot":true,"first_name":"Bridge"},` +
		`"chat":{"id":42,"type":"private"},"text":"answer",` +
		`"reply_to_message":{"message_id":7,"chat":{"id":42,"type":"private"},` +
		`"date":1700000000,"text":"forwarded"}}}`

	const plainMessageUpdate = `{"update_id":4,"message":{"message_id":11,` +
		`"from":{"id":42,"is_bot":false,"first_name":"Operator"},` +
		`"chat":{"id":42,"type":"private"},"text":"hello"}}`

	const senderlessUpdate = `{"update_id":5,"message":{"message_id":12,` +
		`"chat":{"id":-100,"type":"channel"},"date":1700000000,"text":"channel post",` +
		`"reply_to_message":{"message_id":7,"chat":{"id":-100,"type":"channel"},` +
		`"date":1700000000,"text":"forwarded"}}}`

	const callbackUpdate = `{"update_id":6,"callback_query":{"id":"cb1",` +
		`"from":{"id":42,"is_bot":false,"first_name":"Operator"},` +
		`"chat_instance":"ci1","data":"noop"}}`

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "a reply to a forwarded message is a reply",
			raw:  replyUpdate,
			want: true,
		},
		{
			name: "a reply carrying no text is still a reply",
			raw:  mediaReplyUpdate,
			want: true,
		},
		{
			name: "a bot answering a message is a reply too",
			raw:  botReplyUpdate,
			want: true,
		},
		{
			name: "a message that answers nothing is not a reply",
			raw:  plainMessageUpdate,
			want: false,
		},
		{
			name: "a message without a sender is not a reply",
			raw:  senderlessUpdate,
			want: false,
		},
		{
			name: "an update without a message is not a reply",
			raw:  `{"update_id":7}`,
			want: false,
		},
		{
			name: "an update carrying another payload is not a reply",
			raw:  callbackUpdate,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			update := decodeUpdate(t, tt.raw)

			got := predicates.MessageWithReplyToMessage()(context.Background(), update)
			if got != tt.want {
				t.Fatalf("MessageWithReplyToMessage() = %t, want %t for %s", got, tt.want, tt.raw)
			}
		})
	}
}

// TestMessageWithReplyToMessageLeavesTheUpdateAlone asserts the predicate only
// reads the update. A router runs every route against the same update, in
// registration order, so a predicate that mutated what it was given would make
// one route's answer depend on the routes before it.
func TestMessageWithReplyToMessageLeavesTheUpdateAlone(t *testing.T) {
	t.Parallel()

	update := decodeUpdate(t, `{"update_id":1,"message":{"message_id":10,`+
		`"from":{"id":42,"is_bot":false,"first_name":"Operator"},`+
		`"chat":{"id":42,"type":"private"},"text":"answer",`+
		`"reply_to_message":{"message_id":7,"chat":{"id":42,"type":"private"},`+
		`"date":1700000000,"text":"forwarded"}}}`)

	predicate := predicates.MessageWithReplyToMessage()

	first := predicate(context.Background(), update)
	second := predicate(context.Background(), update)

	if first != second {
		t.Fatalf("predicate returned %t then %t on the same update, want a stable answer", first, second)
	}

	if !first {
		t.Fatal("predicate = false, want true for a reply a handler must reach")
	}

	if update.Message == nil || update.Message.From == nil || update.Message.ReplyToMessage == nil {
		t.Fatal("predicate changed the update: message, sender or replied-to message went missing")
	}

	if update.Message.Text != "answer" || update.Message.ReplyToMessage.MessageID != 7 {
		t.Fatalf("predicate changed the update: text %q, replied-to id %d",
			update.Message.Text, update.Message.ReplyToMessage.MessageID)
	}
}

// TestMessageWithReplyToMessageAgreesWithAnyMessageWithFrom pins the predicate
// to the telegohandler leaf it composes: it must never match an update that
// AnyMessageWithFrom rejects, on any payload shape.
func TestMessageWithReplyToMessageAgreesWithAnyMessageWithFrom(t *testing.T) {
	t.Parallel()

	const noMessage = `{"update_id":1}`
	const messageWithoutFrom = `{"update_id":2,"message":{"message_id":12,` +
		`"chat":{"id":-100,"type":"channel"},"date":1700000000,"text":"channel post",` +
		`"reply_to_message":{"message_id":7,"chat":{"id":-100,"type":"channel"},` +
		`"date":1700000000,"text":"forwarded"}}}`
	const messageWithFrom = `{"update_id":3,"message":{"message_id":10,` +
		`"from":{"id":42,"is_bot":false,"first_name":"Operator"},` +
		`"chat":{"id":42,"type":"private"},"text":"answer",` +
		`"reply_to_message":{"message_id":7,"chat":{"id":42,"type":"private"},` +
		`"date":1700000000,"text":"forwarded"}}}`

	tests := []struct {
		name string
		raw  string
	}{
		{name: "no message", raw: noMessage},
		{name: "message without a sender", raw: messageWithoutFrom},
		{name: "message with a sender", raw: messageWithFrom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			update := decodeUpdate(t, tt.raw)

			got := predicates.MessageWithReplyToMessage()(context.Background(), update)
			withFrom := th.AnyMessageWithFrom()(context.Background(), update)

			if withFrom && !got {
				t.Fatalf("MessageWithReplyToMessage() = false for an update AnyMessageWithFrom accepts: %s", tt.raw)
			}

			if !withFrom && got {
				t.Fatalf("MessageWithReplyToMessage() = true for an update AnyMessageWithFrom rejects: %s", tt.raw)
			}
		})
	}
}
