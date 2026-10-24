package telegofx_test

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-core-fx/telegofx"
	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	"go.uber.org/zap"
)

// TestRouterRecoversHandlerPanicByDefault pins that NewRouter registers the
// panic recovery WITHOUT the application asking for it: the first dispatch
// panics inside the handler and comes back as an error carrying the panic
// value and the goroutine stack; the second dispatch reaches the same
// handler, proving the process and the routes survived the panic. The test
// never calls Use - the default registration is the subject.
func TestRouterRecoversHandlerPanicByDefault(t *testing.T) {
	t.Parallel()

	cfg := telegofx.Config{Token: testToken, Mode: telegofx.ModePolling}

	bot, err := telegofx.New(cfg, &http.Client{Timeout: time.Second}, nil, zap.NewNop())
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}

	router, err := telegofx.NewRouter(cfg, bot)
	if err != nil {
		t.Fatalf("create router: %v", err)
	}

	var calls atomic.Int32
	second := make(chan struct{})
	router.Handle(func(_ *th.Context, _ telego.Update) error {
		if calls.Add(1) == 1 {
			panic("boom in handler")
		}
		close(second)

		return nil
	})

	err = router.BaseGroup().HandleUpdate(context.Background(), bot.Bot, panicTestUpdate(1))
	if err == nil {
		t.Fatal("first dispatch error = nil, want the recovered panic")
	}
	if !strings.Contains(err.Error(), "boom in handler") {
		t.Errorf("first dispatch error = %v, want it to carry the panic value", err)
	}
	if !strings.Contains(err.Error(), "goroutine") {
		t.Errorf("first dispatch error = %v, want it to carry a stack trace", err)
	}

	err = router.BaseGroup().HandleUpdate(context.Background(), bot.Bot, panicTestUpdate(2))
	if err != nil {
		t.Fatalf("second dispatch error = %v, want nil", err)
	}

	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("handler was not reached after the recovered panic")
	}

	if got := calls.Load(); got != 2 {
		t.Errorf("handler calls = %d, want 2", got)
	}
}

// panicTestUpdate builds the minimal update that matches a predicate-less
// route.
func panicTestUpdate(updateID int) telego.Update {
	return telego.Update{
		UpdateID: updateID,
		Message: &telego.Message{
			MessageID: updateID,
			Chat:      telego.Chat{ID: int64(updateID), Type: "private"},
			From:      &telego.User{ID: int64(updateID)},
			Text:      "/start",
		},
	}
}
