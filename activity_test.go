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

func TestBotLastAPIActivityZeroBeforeAnyCall(t *testing.T) {
	t.Parallel()

	bot, err := telegofx.New(
		telegofx.Config{Token: testToken, Mode: telegofx.ModePolling},
		&http.Client{Timeout: time.Second},
		nil,
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}

	if !bot.LastAPIActivity().IsZero() {
		t.Fatal("no API call has happened yet, want zero time")
	}
}

func TestBotLastAPIActivityRecordsSuccessfulCall(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(getMeResponse))
	}))
	t.Cleanup(server.Close)

	bot, err := telegofx.New(
		telegofx.Config{Token: testToken, Mode: telegofx.ModePolling},
		&http.Client{Timeout: time.Second},
		[]telego.BotOption{telego.WithAPIServer(server.URL)},
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

	if bot.LastAPIActivity().IsZero() {
		t.Fatal("successful call must record the heartbeat")
	}
}

func TestBotLastAPIActivityRecordsFailedCall(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	bot, err := telegofx.New(
		telegofx.Config{Token: testToken, Mode: telegofx.ModePolling},
		&http.Client{Timeout: time.Second},
		[]telego.BotOption{telego.WithAPIServer(server.URL)},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}

	_, err = bot.GetMe(context.Background())
	if err == nil {
		t.Fatal("5xx response must surface as an error")
	}

	if bot.LastAPIActivity().IsZero() {
		t.Fatal("a failed call must still record activity: it completed")
	}
}

func TestBotGetMeHonoursClientTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(time.Millisecond * 600)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(getMeResponse))
	}))
	t.Cleanup(server.Close)

	const timeout = time.Millisecond * 150

	bot, err := telegofx.New(
		telegofx.Config{Token: testToken, Mode: telegofx.ModePolling},
		&http.Client{Timeout: timeout},
		[]telego.BotOption{telego.WithAPIServer(server.URL)},
		zap.NewNop(),
	)
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}

	start := time.Now()
	_, err = bot.GetMe(context.Background())
	if err == nil {
		t.Fatal("request against a hanging server must fail on timeout")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("request took %s, want about %s", elapsed, timeout)
	}

	if bot.LastAPIActivity().IsZero() {
		t.Fatal("a timed-out call must still record activity: it completed with an error")
	}
}
