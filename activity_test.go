package telegofx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-core-fx/telegofx"
	"github.com/mymmrac/telego"
	"go.uber.org/zap"
)

const testToken = "123456:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// getMeResponse is the Bot API envelope a fake server answers for getMe.
const getMeResponse = `{"ok":true,"result":{"id":424242,"is_bot":true,` +
	`"first_name":"test","username":"test_bot"}}`

// getUpdatesResponse is the Bot API envelope a fake server answers for
// getUpdates: an empty update list.
const getUpdatesResponse = `{"ok":true,"result":[]}`

func TestBotLastAPIActivityZeroBeforeAnyCall(t *testing.T) {
	t.Parallel()

	bot, err := telegofx.New(
		telegofx.Config{Token: testToken, Mode: telegofx.ModePolling},
		[]telegofx.Option{telegofx.WithHTTPClient(&http.Client{Timeout: time.Second})},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}

	if !bot.LastAPIActivity().IsZero() {
		t.Fatal("no API call has happened yet, want zero time")
	}
}

// TestBotNewWithoutHTTPClientUsesDefault pins that WithHTTPClient is
// optional: New falls back to a plain default client instead of failing.
func TestBotNewWithoutHTTPClientUsesDefault(t *testing.T) {
	t.Parallel()

	bot, err := telegofx.New(
		telegofx.Config{Token: testToken, Mode: telegofx.ModePolling},
		nil,
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("new without options must fall back to the default client: %v", err)
	}

	if !bot.LastAPIActivity().IsZero() {
		t.Fatal("no API call has happened yet, want zero time")
	}
}

// TestBotLastAPIActivityRecordsSuccessfulPoll drives one completed
// getUpdates through the fake server: the receive-loop heartbeat must set.
func TestBotLastAPIActivityRecordsSuccessfulPoll(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(getUpdatesResponse))
	}))
	t.Cleanup(server.Close)

	bot, err := telegofx.New(
		telegofx.Config{Token: testToken, Mode: telegofx.ModePolling},
		[]telegofx.Option{
			telegofx.WithHTTPClient(&http.Client{Timeout: time.Second}),
			telegofx.WithBotOptions(telego.WithAPIServer(server.URL)),
		},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}

	_, err = bot.GetUpdates(context.Background(), &telego.GetUpdatesParams{})
	if err != nil {
		t.Fatalf("get updates: %v", err)
	}

	if bot.LastAPIActivity().IsZero() {
		t.Fatal("successful getUpdates must record the heartbeat")
	}
}

// TestBotLastAPIActivityRecordsFailedPoll pins that a getUpdates which
// completes with an error still records: external outages fail fast and
// must keep the health check green.
func TestBotLastAPIActivityRecordsFailedPoll(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	bot, err := telegofx.New(
		telegofx.Config{Token: testToken, Mode: telegofx.ModePolling},
		[]telegofx.Option{
			telegofx.WithHTTPClient(&http.Client{Timeout: time.Second}),
			telegofx.WithBotOptions(telego.WithAPIServer(server.URL)),
		},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}

	_, err = bot.GetUpdates(context.Background(), &telego.GetUpdatesParams{})
	if err == nil {
		t.Fatal("5xx response must surface as an error")
	}

	if bot.LastAPIActivity().IsZero() {
		t.Fatal("a failed getUpdates must still record activity: it completed")
	}
}

// TestBotLastAPIActivityNotRecordedUntilBodyCompletes pins the
// body-completion semantics: a server that flushes headers and then stalls
// the body must not refresh the heartbeat while getUpdates is still blocked
// reading it - otherwise a wedged receive loop would keep liveness green.
func TestBotLastAPIActivityNotRecordedUntilBodyCompletes(t *testing.T) {
	t.Parallel()

	headersSent := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"resu`))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(headersSent)
		<-r.Context().Done() // hold the body open until the client gives up
	}))
	t.Cleanup(server.Close)

	const timeout = time.Millisecond * 600

	bot, err := telegofx.New(
		telegofx.Config{Token: testToken, Mode: telegofx.ModePolling},
		[]telegofx.Option{
			telegofx.WithHTTPClient(&http.Client{Timeout: timeout}),
			telegofx.WithBotOptions(telego.WithAPIServer(server.URL)),
		},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, getErr := bot.GetUpdates(context.Background(), &telego.GetUpdatesParams{})
		errCh <- getErr
	}()

	// Every wait is bounded: header arrival, the settle window, and the
	// request outcome, so a timing surprise fails the test instead of
	// hanging it.
	select {
	case <-headersSent:
	case getErr := <-errCh:
		t.Fatalf("request failed before headers arrived: %v", getErr)
	case <-time.After(2 * timeout):
		t.Fatal("headers never arrived within the deadline")
	}

	// Bounded settle: give a header-based recorder time to (wrongly) record
	// while the body is still pending; the window is a quarter of the client
	// timeout, leaving three quarters of margin for header arrival.
	settleDeadline := time.Now().Add(timeout / 4)
	for {
		if !bot.LastAPIActivity().IsZero() {
			t.Fatal("headers alone must not record: the response body has not completed")
		}
		if time.Now().After(settleDeadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case getErr := <-errCh:
		if getErr == nil {
			t.Fatal("stalled body must fail with the client timeout")
		}
	case <-time.After(2 * timeout):
		t.Fatal("GetUpdates never returned after the client timeout")
	}

	if bot.LastAPIActivity().IsZero() {
		t.Fatal("the timed-out body completion must record")
	}
}

// TestBotLastAPIActivityIgnoresNonPollingCalls pins the receive-loop scope:
// a completed getMe (like any outbound send) must NOT refresh the
// heartbeat, or busy outbound traffic would mask a stalled polling loop.
func TestBotLastAPIActivityIgnoresNonPollingCalls(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(getMeResponse))
	}))
	t.Cleanup(server.Close)

	bot, err := telegofx.New(
		telegofx.Config{Token: testToken, Mode: telegofx.ModePolling},
		[]telegofx.Option{
			telegofx.WithHTTPClient(&http.Client{Timeout: time.Second}),
			telegofx.WithBotOptions(telego.WithAPIServer(server.URL)),
		},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}

	me, err := bot.GetMe(context.Background())
	if err != nil {
		t.Fatalf("get me: %v", err)
	}
	if me.Username != "test_bot" {
		t.Fatalf("username = %q, want the fake identity", me.Username)
	}

	if !bot.LastAPIActivity().IsZero() {
		t.Fatal("non-polling getMe must not record: the heartbeat tracks getUpdates only")
	}
}

// TestBotGetUpdatesHonoursClientTimeout pins that the client Timeout bounds
// the long-poll request end to end and that the timed-out request still
// records: it completed with an error.
func TestBotGetUpdatesHonoursClientTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(time.Millisecond * 600)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(getUpdatesResponse))
	}))
	t.Cleanup(server.Close)

	const timeout = time.Millisecond * 150

	bot, err := telegofx.New(
		telegofx.Config{Token: testToken, Mode: telegofx.ModePolling},
		[]telegofx.Option{
			telegofx.WithHTTPClient(&http.Client{Timeout: timeout}),
			telegofx.WithBotOptions(telego.WithAPIServer(server.URL)),
		},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}

	start := time.Now()
	_, err = bot.GetUpdates(context.Background(), &telego.GetUpdatesParams{})
	if err == nil {
		t.Fatal("request against a hanging server must fail on timeout")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("request took %s, want about %s", elapsed, timeout)
	}

	if bot.LastAPIActivity().IsZero() {
		t.Fatal("a timed-out getUpdates must still record activity: it completed with an error")
	}
}
