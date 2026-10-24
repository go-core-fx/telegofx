package telegofx

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mymmrac/telego"
	ta "github.com/mymmrac/telego/telegoapi"
	"go.uber.org/zap"
)

type Bot struct {
	*telego.Bot

	config Config

	updates chan telego.Update

	mu      sync.RWMutex
	handler telego.WebhookHandler

	activity *activityCaller

	requestTimeout time.Duration

	logger *zap.Logger
}

// New creates a new Bot from the given configuration and HTTP client and
// options.
//
// The caller owns the client: it carries the proxy routing and the
// per-request timeout that keep a stalled Telegram request from hanging the
// polling loop (a client without a Timeout is only warned about, never
// mutated). telegofx contributes the recording API caller before the
// caller's options, so a caller-supplied client option still wins, and the
// zap logger last.
func New(config Config, httpClient *http.Client, options []telego.BotOption, logger *zap.Logger) (*Bot, error) {
	if httpClient.Timeout == 0 {
		logger.Warn("http client has no Timeout, a stalled Bot API request can hang the bot")
	}

	activity := newActivityCaller(ta.HTTPCaller{Client: httpClient})
	options = append([]telego.BotOption{telego.WithAPICaller(activity)}, options...)
	options = append(options, telego.WithLogger(&zapLogger{logger}))

	bot, err := telego.NewBot(config.Token, options...)
	if err != nil {
		return nil, fmt.Errorf("create bot: %w", err)
	}

	return &Bot{
		Bot: bot,

		config: config,

		updates: make(chan telego.Update),

		mu:      sync.RWMutex{},
		handler: nil,

		activity:       activity,
		requestTimeout: httpClient.Timeout,

		logger: logger,
	}, nil
}

// LastAPIActivity returns the time of the last completed Bot API call -
// success or failure - or the zero time while no call has completed yet
// (including before the bot starts polling). A request that stalls forever
// never completes and never updates this timestamp.
func (b *Bot) LastAPIActivity() time.Time {
	return b.activity.lastActivity()
}

// HandleUpdate delivers a raw webhook update payload to the registered
// handler. It returns ErrHandlerNotReady until the bot is running in webhook
// mode and its handler has been registered, and after the bot has stopped.
func (b *Bot) HandleUpdate(ctx context.Context, data []byte) error {
	b.mu.RLock()
	handler := b.handler
	b.mu.RUnlock()

	if handler == nil {
		return ErrHandlerNotReady
	}

	return handler(ctx, data)
}

// Run starts processing updates using the delivery mode selected in Config.
// For ModeWebhook it registers the webhook handler and closes the channel
// returned by HandlerReady once registration completes.
func (b *Bot) Run(ctx context.Context) error {
	defer close(b.updates)
	defer func() {
		b.mu.Lock()
		b.handler = nil
		b.mu.Unlock()
	}()

	var updates <-chan telego.Update
	var err error
	switch b.config.Mode {
	case ModeWebhook:
		updates, err = b.Bot.UpdatesViaWebhook(ctx, func(handler telego.WebhookHandler) error {
			b.mu.Lock()
			b.handler = handler
			b.mu.Unlock()

			return nil
		})
	case ModePolling:
		fallthrough
	default:
		updates, err = b.Bot.UpdatesViaLongPolling(ctx, nil)
	}

	if err != nil {
		return fmt.Errorf("get updates: %w", err)
	}

	for {
		select {
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			select {
			case b.updates <- update:
			case <-ctx.Done():
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// Updates returns the channel of incoming updates processed by Run.
func (b *Bot) Updates() <-chan telego.Update {
	return b.updates
}
