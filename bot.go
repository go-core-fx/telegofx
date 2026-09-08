package telegofx

import (
	"context"
	"fmt"
	"sync"

	"github.com/mymmrac/telego"
	"go.uber.org/zap"
)

type Bot struct {
	*telego.Bot

	config Config

	updates chan telego.Update

	mu      sync.RWMutex
	handler telego.WebhookHandler

	logger *zap.Logger
}

// New creates a new Bot from the given configuration and options.
func New(config Config, options []telego.BotOption, logger *zap.Logger) (*Bot, error) {
	options = append(options, telego.WithLogger(&zapLogger{logger}))

	if config.ProxyURL != "" {
		client, err := NewSocks5Client(config.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("create socks5 client: %w", err)
		}
		options = append(options, telego.WithFastHTTPClient(client))
	}

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

		logger: logger,
	}, nil
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
