package telegofx

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	th "github.com/mymmrac/telego/telegohandler"
)

// errHandlerPanic wraps a recovered handler panic so telego's error path
// reports it instead of the process dying.
var errHandlerPanic = errors.New("handler panic")

// panicStackBytes is the buffer size for the stack trace embedded in a
// recovered-panic error.
const panicStackBytes = 8192

type Router struct {
	*th.BotHandler
}

// NewRouter creates the update router for bot with panic recovery
// registered by default.
//
// Registration order is load-bearing: telegohandler is first-match-wins over
// a flat routes slice, and this constructor runs before the application
// registers any Handle call, so the recovery middleware always lands first
// and wraps every route. A panicking handler therefore surfaces as an error
// on telego's error path (logged through the bot logger) while the process
// and the dispatch loop keep running.
func NewRouter(_ Config, bot *Bot) (*Router, error) {
	handler, err := th.NewBotHandler(
		bot.Bot,
		bot.Updates(),
	)
	if err != nil {
		return nil, fmt.Errorf("create handler: %w", err)
	}

	handler.Use(th.PanicRecoveryHandler(recoverHandlerPanic))

	return &Router{
		BotHandler: handler,
	}, nil
}

func (r *Router) Start() error {
	if err := r.BotHandler.Start(); err != nil {
		return fmt.Errorf("start handler: %w", err)
	}

	return nil
}

func (r *Router) Stop(ctx context.Context) error {
	if err := r.BotHandler.StopWithContext(ctx); err != nil {
		return fmt.Errorf("stop handler: %w", err)
	}

	return nil
}

// recoverHandlerPanic converts a recovered handler panic into an error that
// carries both the panic value and the goroutine stack, so the logged error
// still points at the panicking line.
func recoverHandlerPanic(recovered any) error {
	stack := make([]byte, panicStackBytes)
	stack = stack[:runtime.Stack(stack, false)]

	return fmt.Errorf("%w: %v\n%s", errHandlerPanic, recovered, stack)
}
