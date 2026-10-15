package fsm_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/go-core-fx/telegofx/fsm"
	"github.com/mymmrac/telego"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// errStorage is the sentinel failing Storage implementations return.
var errStorage = errors.New("storage unavailable")

// mapStorage is a minimal in-memory fsm.Storage for the tests with
// per-operation error switches for the error-path rows.
type mapStorage struct {
	mu        sync.Mutex
	states    map[[2]int64]string
	failGet   bool
	failSet   bool
	failClear bool
}

func newMapStorage() *mapStorage {
	return &mapStorage{
		mu:        sync.Mutex{},
		states:    map[[2]int64]string{},
		failGet:   false,
		failSet:   false,
		failClear: false,
	}
}

// Get implements fsm.Storage.
func (s *mapStorage) Get(_ context.Context, chatID, userID int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failGet {
		return "", errStorage
	}
	return s.states[[2]int64{chatID, userID}], nil
}

// Set implements fsm.Storage.
func (s *mapStorage) Set(_ context.Context, chatID, userID int64, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failSet {
		return errStorage
	}
	s.states[[2]int64{chatID, userID}] = state
	return nil
}

// Clear implements fsm.Storage.
func (s *mapStorage) Clear(_ context.Context, chatID, userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failClear {
		return errStorage
	}
	delete(s.states, [2]int64{chatID, userID})
	return nil
}

// privateUpdate builds a private-chat message update for the chat/user pair.
// The text is fixed fixture data: the state predicates key on the chat and the
// sender only.
func privateUpdate(chatID, userID int64) telego.Update {
	return telego.Update{
		Message: &telego.Message{
			Chat: telego.Chat{ID: chatID, Type: telego.ChatTypePrivate},
			From: &telego.User{ID: userID},
			Text: "hello",
		},
	}
}

// TestStore_stateLifecycle covers the happy path and the idempotency
// transition: set -> get -> clear -> get-missing yields ("", nil), and
// clearing an already-missing state is not an error.
func TestStore_stateLifecycle(t *testing.T) {
	t.Parallel()

	store := fsm.NewStore(newMapStorage(), zap.NewNop())
	ctx := context.Background()

	var (
		err error
		got string
	)

	if err = store.Set(ctx, 100, 200, "wait"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if got, err = store.Get(ctx, 100, 200); err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got != "wait" {
		t.Errorf("GetState = %q, want %q", got, "wait")
	}

	if err = store.Clear(ctx, 100, 200); err != nil {
		t.Fatalf("ClearState: %v", err)
	}
	if got, err = store.Get(ctx, 100, 200); err != nil {
		t.Fatalf("GetState after clear: %v", err)
	}
	if got != "" {
		t.Errorf("GetState after clear = %q, want empty", got)
	}

	if err = store.Clear(ctx, 100, 200); err != nil {
		t.Fatalf("ClearState on missing state must not error: %v", err)
	}
}

// TestStore_keyedByChatAndUser pins the chat+user keying (owner decision;
// aiogram-parity): a state stored for one pair is invisible to any other
// chat or user combination.
func TestStore_keyedByChatAndUser(t *testing.T) {
	t.Parallel()

	store := fsm.NewStore(newMapStorage(), zap.NewNop())
	ctx := context.Background()

	if err := store.Set(ctx, 100, 200, "wait"); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	for _, pair := range [][2]int64{{101, 200}, {100, 201}, {101, 201}} {
		got, err := store.Get(ctx, pair[0], pair[1])
		if err != nil {
			t.Fatalf("GetState(%d, %d): %v", pair[0], pair[1], err)
		}
		if got != "" {
			t.Errorf("GetState(%d, %d) = %q, want empty (chat+user keying)", pair[0], pair[1], got)
		}
	}
}

// TestStore_errorsAreWrapped: every failing Storage call surfaces wrapped
// with the operation prefix, preserving [errors.Is] for the caller.
func TestStore_errorsAreWrapped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	setStore := fsm.NewStore(&mapStorage{
		mu:        sync.Mutex{},
		states:    map[[2]int64]string{},
		failGet:   false,
		failSet:   true,
		failClear: false,
	}, zap.NewNop())
	if err := setStore.Set(ctx, 1, 1, "s"); !errors.Is(err, errStorage) ||
		!strings.Contains(err.Error(), "set fsm state: ") {
		t.Errorf("SetState error = %v, want wrap of errStorage with \"set fsm state: \"", err)
	}

	getStore := fsm.NewStore(&mapStorage{
		mu:        sync.Mutex{},
		states:    map[[2]int64]string{},
		failGet:   true,
		failSet:   false,
		failClear: false,
	}, zap.NewNop())
	if _, err := getStore.Get(ctx, 1, 1); !errors.Is(err, errStorage) ||
		!strings.Contains(err.Error(), "get fsm state: ") {
		t.Errorf("GetState error = %v, want wrap of errStorage with \"get fsm state: \"", err)
	}

	clearStore := fsm.NewStore(&mapStorage{
		mu:        sync.Mutex{},
		states:    map[[2]int64]string{},
		failGet:   false,
		failSet:   false,
		failClear: true,
	}, zap.NewNop())
	if err := clearStore.Clear(ctx, 1, 1); !errors.Is(err, errStorage) ||
		!strings.Contains(err.Error(), "clear fsm state: ") {
		t.Errorf("ClearState error = %v, want wrap of errStorage with \"clear fsm state: \"", err)
	}
}

// TestStateFilter_matchesOnlyExactState drives StateFilter over a matrix of
// stored/filter state pairs: the empty state matches a missing key (the
// Storage contract), every unequal pair fails, and distinct state strings
// are all honored.
func TestStateFilter_matchesOnlyExactState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		stored string
		filter string
		want   bool
		primed bool
	}{
		{name: "missing key equals empty state", stored: "", filter: fsm.StateEmpty, want: true, primed: false},
		{name: "empty state vs stored wait", stored: "wait", filter: fsm.StateEmpty, want: false, primed: true},
		{name: "wait matches wait", stored: "wait", filter: "wait", want: true, primed: true},
		{name: "wait_for_contact matches", stored: "start:wait_for_contact",
			filter: "start:wait_for_contact", want: true, primed: true},
		{name: "wait does not match other", stored: "wait", filter: "other", want: false, primed: true},
		{name: "empty does not match wait", stored: "", filter: "wait", want: false, primed: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			storage := newMapStorage()
			if tc.primed {
				if err := storage.Set(context.Background(), 100, 200, tc.stored); err != nil {
					t.Fatalf("prime state: %v", err)
				}
			} else if tc.stored != "" {
				t.Fatalf("test case bug: unprimed case must not carry a stored value")
			}

			pred := fsm.StateEqual(fsm.NewStore(storage, zap.NewNop()), tc.filter)
			got := pred(context.Background(), privateUpdate(100, 200))
			if got != tc.want {
				t.Errorf("StateFilter(%q) over stored %q = %v, want %v", tc.filter, tc.stored, got, tc.want)
			}
		})
	}
}

// TestStateFilter_guardsNoMessageAndNoSender: an update without a message
// (edited_message/channel_post/callback_query) or with a nil sender
// never reaches the storage and never matches.
func TestStateFilter_guardsNoMessageAndNoSender(t *testing.T) {
	t.Parallel()

	pred := fsm.StateEqual(fsm.NewStore(newMapStorage(), zap.NewNop()), fsm.StateEmpty)

	if pred(context.Background(), telego.Update{}) {
		t.Error("update without Message must not match")
	}

	noSender := telego.Update{Message: &telego.Message{
		Chat: telego.Chat{ID: 100, Type: telego.ChatTypePrivate},
		Text: "hello",
	}}
	if pred(context.Background(), noSender) {
		t.Error("message without From must not match")
	}
}

// TestStateFilter_readsFreshStatePerEvaluation: one predicate value rereads
// the store on every evaluation, so set/clear transitions are observed
// without rebuilding the predicate.
func TestStateFilter_readsFreshStatePerEvaluation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storage := newMapStorage()
	pred := fsm.StateEqual(fsm.NewStore(storage, zap.NewNop()), "wait")
	update := privateUpdate(100, 200)

	if pred(ctx, update) {
		t.Fatal("filter must not match before the state is set")
	}
	if err := storage.Set(ctx, 100, 200, "wait"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if !pred(ctx, update) {
		t.Fatal("filter must match after the state is set")
	}
	if err := storage.Clear(ctx, 100, 200); err != nil {
		t.Fatalf("ClearState: %v", err)
	}
	if pred(ctx, update) {
		t.Fatal("filter must not match after the state is cleared")
	}
}

// TestStateFilter_storageErrorDoesNotMatchAndLogs pins the storage-error
// contract: a failing read returns false (predicates return bool only) and
// writes exactly one Error-level log entry carrying chat_id, user_id and the
// wrapped error. StateEmpty is the filter on purpose: an implementation that
// swallowed the error into "" would match here.
func TestStateFilter_storageErrorDoesNotMatchAndLogs(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zap.ErrorLevel)
	storage := newMapStorage()
	storage.failGet = true
	pred := fsm.StateEqual(fsm.NewStore(storage, zap.New(core)), fsm.StateEmpty)

	if pred(context.Background(), privateUpdate(100, 200)) {
		t.Fatal("failing state read must not match, even for the empty state")
	}

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("want exactly 1 log entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.Level != zap.ErrorLevel {
		t.Errorf("log level = %s, want Error", entry.Level)
	}

	fields := entry.ContextMap()
	if chatID, ok := fields["chat_id"].(int64); !ok || chatID != 100 {
		t.Errorf("chat_id field = %v, want int64(100)", fields["chat_id"])
	}
	if userID, ok := fields["user_id"].(int64); !ok || userID != 200 {
		t.Errorf("user_id field = %v, want int64(200)", fields["user_id"])
	}
	msg, ok := fields["error"].(string)
	if !ok {
		t.Fatalf("error field must be a string, got %T", fields["error"])
	}
	if !strings.Contains(msg, "get fsm state: ") || !strings.Contains(msg, "storage unavailable") {
		t.Errorf("error field = %q, want it to contain the operation prefix and the storage error", msg)
	}
}

// TestStatePredicates_rejectChatSentMessages pins the chat-sender guard:
// Telegram reports a fake sender user for messages sent on behalf of a chat,
// so both predicates must ignore them even when the state of that fake user
// would match. Without the guard every such message of the chat would share
// one FSM state.
func TestStatePredicates_rejectChatSentMessages(t *testing.T) {
	t.Parallel()

	const fakeSenderID = int64(1087968824) // GroupAnonymousBot

	ctx := context.Background()
	core, logs := observer.New(zap.ErrorLevel)
	storage := newMapStorage()
	if err := storage.Set(ctx, 100, fakeSenderID, "wait"); err != nil {
		t.Fatalf("prime state: %v", err)
	}

	update := telego.Update{Message: &telego.Message{
		Chat:       telego.Chat{ID: 100, Type: telego.ChatTypeGroup},
		From:       &telego.User{ID: fakeSenderID},
		SenderChat: &telego.Chat{ID: 100, Type: telego.ChatTypeGroup},
		Text:       "hello",
	}}

	store := fsm.NewStore(storage, zap.New(core))

	if fsm.StateEqual(store, "wait")(ctx, update) {
		t.Error("StateEqual must not match a message sent on behalf of a chat")
	}
	if fsm.StatePrefix(store, "wait")(ctx, update) {
		t.Error("StatePrefix must not match a message sent on behalf of a chat")
	}
	if entries := logs.All(); len(entries) != 0 {
		t.Errorf("chat-sent message must not read the state, got %d log entries", len(entries))
	}
}

// TestStatePrefix_matchesOnlyPrefixedState drives StatePrefix over a matrix of
// stored/prefix pairs: a stored state matches only when it starts with the
// prefix, and a missing state never matches - not even for the empty prefix.
func TestStatePrefix_matchesOnlyPrefixedState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		stored string
		prefix string
		want   bool
		primed bool
	}{
		{name: "prefix matches stored state", stored: "start:wait_for_contact",
			prefix: "start:", want: true, primed: true},
		{name: "longer prefix matches stored state", stored: "start:wait_for_contact",
			prefix: "start:wait", want: true, primed: true},
		{name: "exact prefix matches stored state", stored: "start:wait_for_contact",
			prefix: "start:wait_for_contact", want: true, primed: true},
		{name: "prefix does not match other state", stored: "wait",
			prefix: "start:", want: false, primed: true},
		{name: "missing state never matches", stored: "",
			prefix: "start:", want: false, primed: false},
		{name: "missing state never matches empty prefix", stored: "",
			prefix: "", want: false, primed: false},
		{name: "empty prefix matches stored state", stored: "wait",
			prefix: "", want: true, primed: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			storage := newMapStorage()
			if tc.primed {
				if err := storage.Set(context.Background(), 300, 400, tc.stored); err != nil {
					t.Fatalf("prime state: %v", err)
				}
			} else if tc.stored != "" {
				t.Fatalf("test case bug: unprimed case must not carry a stored value")
			}

			pred := fsm.StatePrefix(fsm.NewStore(storage, zap.NewNop()), tc.prefix)
			got := pred(context.Background(), privateUpdate(300, 400))
			if got != tc.want {
				t.Errorf("StatePrefix(%q) over stored %q = %v, want %v", tc.prefix, tc.stored, got, tc.want)
			}
		})
	}
}

// TestStatePrefix_storageErrorDoesNotMatchAndLogs pins the same
// storage-error contract for StatePrefix: a failing read returns false and
// writes exactly one Error-level log entry carrying chat_id, user_id and the
// wrapped error.
func TestStatePrefix_storageErrorDoesNotMatchAndLogs(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zap.ErrorLevel)
	storage := newMapStorage()
	storage.failGet = true
	pred := fsm.StatePrefix(fsm.NewStore(storage, zap.New(core)), "start:")

	if pred(context.Background(), privateUpdate(300, 400)) {
		t.Fatal("failing state read must not match")
	}

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("want exactly 1 log entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.Level != zap.ErrorLevel {
		t.Errorf("log level = %s, want Error", entry.Level)
	}

	fields := entry.ContextMap()
	if chatID, ok := fields["chat_id"].(int64); !ok || chatID != 300 {
		t.Errorf("chat_id field = %v, want int64(300)", fields["chat_id"])
	}
	if userID, ok := fields["user_id"].(int64); !ok || userID != 400 {
		t.Errorf("user_id field = %v, want int64(400)", fields["user_id"])
	}
	msg, ok := fields["error"].(string)
	if !ok {
		t.Fatalf("error field must be a string, got %T", fields["error"])
	}
	if !strings.Contains(msg, "get fsm state: ") || !strings.Contains(msg, "storage unavailable") {
		t.Errorf("error field = %q, want it to contain the operation prefix and the storage error", msg)
	}
}
