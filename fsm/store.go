package fsm

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// Store is the fsm facade handlers and predicates share: it delegates to the
// injected Storage, wraps every storage failure with its operation prefix,
// and carries the logger StateFilter writes state-read failures to before
// returning false.
type Store struct {
	storage Storage
	logger  *zap.Logger
}

// NewStore creates a Store over storage. The logger is a constructor
// dependency (fx provides it; tests pass zap.NewNop or an observer core).
func NewStore(storage Storage, logger *zap.Logger) *Store {
	return &Store{
		storage: storage,
		logger:  logger,
	}
}

// Get returns the stored state for the chat/user pair. A missing state
// yields ("", nil).
func (s *Store) Get(ctx context.Context, chatID, userID int64) (string, error) {
	state, err := s.storage.Get(ctx, chatID, userID)
	if err != nil {
		return "", fmt.Errorf("get fsm state: %w", err)
	}
	return state, nil
}

// Set stores the conversation state for the chat/user pair.
func (s *Store) Set(ctx context.Context, chatID, userID int64, state string) error {
	if err := s.storage.Set(ctx, chatID, userID, state); err != nil {
		return fmt.Errorf("set fsm state: %w", err)
	}
	return nil
}

// Clear removes the stored state for the chat/user pair; clearing a
// missing state is not an error.
func (s *Store) Clear(ctx context.Context, chatID, userID int64) error {
	if err := s.storage.Clear(ctx, chatID, userID); err != nil {
		return fmt.Errorf("clear fsm state: %w", err)
	}
	return nil
}

var _ Storage = (*Store)(nil)
