// Package fsm provides a storage-agnostic conversation-state layer for
// telego bots: a ctx-based Storage contract, a Store facade over it, and
// telegohandler predicates that read the state inside the predicate.
//
// State is keyed by chat+user pair.
package fsm

import "context"

// Storage persists conversation state for chat/user pairs. It is ctx-based
// because telego handlers carry a context that must bound every storage
// call.
type Storage interface {
	// Get returns the stored state for the chat/user pair. A missing
	// state yields ("", nil).
	Get(ctx context.Context, chatID, userID int64) (string, error)

	// Set stores the conversation state for the chat/user pair.
	Set(ctx context.Context, chatID, userID int64, state string) error

	// Clear removes the stored state for the chat/user pair. Clearing
	// a missing state is not an error.
	Clear(ctx context.Context, chatID, userID int64) error
}
