package telegofx

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mymmrac/telego"
	"go.uber.org/zap"
)

type Bot struct {
	*telego.Bot

	config Config

	updates chan telego.Update

	mu      sync.RWMutex
	handler telego.WebhookHandler

	activity *activityTransport

	requestTimeout time.Duration

	logger *zap.Logger
}

// Option configures New.
type Option func(*options)

// options carries the bot-scoped dependencies New assembles from the
// Option values: the HTTP client and any extra telego bot options.
type options struct {
	client     *http.Client
	botOptions []telego.BotOption
}

// WithHTTPClient supplies the HTTP client the bot's API traffic rides. It is
// optional: without it New falls back to a plain default client (no proxy,
// no timeout). The client carries the proxy routing and the per-request
// timeout that keep a stalled Telegram request from hanging the polling
// loop (a client without a Timeout is only warned about, never mutated).
func WithHTTPClient(client *http.Client) Option {
	return func(o *options) {
		o.client = client
	}
}

// WithBotOptions passes additional telego bot options through to
// telego.NewBot. They apply after telegofx's own options, so a caller or
// client option among them (telego.WithAPICaller, telego.WithHTTPClient,
// telego.WithFastHTTPClient) replaces what telegofx installed and hides
// traffic from Bot.LastAPIActivity and the WithHealth check.
func WithBotOptions(opts ...telego.BotOption) Option {
	return func(o *options) {
		o.botOptions = append(o.botOptions, opts...)
	}
}

// New creates a new Bot from the given configuration and options.
//
// Options own the bot's HTTP surface: WithHTTPClient supplies the optional
// client, WithBotOptions passes extra telego options. telegofx installs its
// own client first - a shallow copy of the supplied one whose transport
// records getUpdates completions for the WithHealth liveness check - then
// the caller's options, and the zap logger last, so the caller's options
// take precedence.
func New(config Config, opts []Option, logger *zap.Logger) (*Bot, error) {
	o := new(options)
	for _, opt := range opts {
		opt(o)
	}
	if o.client == nil {
		o.client = &http.Client{}
	}
	if o.client.Timeout == 0 {
		logger.Warn("http client has no Timeout, a stalled Bot API request can hang the bot")
	}

	activity := newActivityTransport(o.client.Transport)
	wrapped := *o.client
	wrapped.Transport = activity

	telegoOpts := []telego.BotOption{telego.WithHTTPClient(&wrapped)}
	telegoOpts = append(telegoOpts, o.botOptions...)
	telegoOpts = append(telegoOpts, telego.WithLogger(&zapLogger{logger}))

	bot, err := telego.NewBot(config.Token, telegoOpts...)
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
		requestTimeout: o.client.Timeout,

		logger: logger,
	}, nil
}

// LastAPIActivity returns the time of the last completed getUpdates request -
// success or failure - or the zero time while polling has not completed one
// yet (including before the bot starts polling). A request that stalls
// forever never completes and never updates this timestamp. Non-polling API
// calls (sendMessage, getMe, ...) do not refresh it: the heartbeat tracks
// the receive loop, not outbound traffic.
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
