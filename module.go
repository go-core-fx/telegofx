package telegofx

import (
	"context"
	"fmt"

	"github.com/go-core-fx/logger"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type moduleConfig struct {
	withRouter bool
}

// Option configures Module.
type Option func(*moduleConfig)

// WithRouter includes the router module in the returned Module.
func WithRouter() Option {
	return func(cfg *moduleConfig) {
		cfg.withRouter = true
	}
}

// Module returns an fx module that provides a Bot, runs it according to the
// configured delivery mode, and exposes HandleUpdate as a WebhookHandler.
func Module(opts ...Option) fx.Option {
	cfg := new(moduleConfig)
	for _, opt := range opts {
		opt(cfg)
	}

	options := []fx.Option{
		logger.WithNamedLogger("telegofx"),
		fx.Provide(New),
		fx.Provide(func(b *Bot) WebhookHandler { return b.HandleUpdate }),
		fx.Invoke(func(lc fx.Lifecycle, bot *Bot, logger *zap.Logger, sh fx.Shutdowner) {
			lc.Append(newBotHook(bot, logger, sh))
		}),
	}
	if cfg.withRouter {
		options = append(options, RouterModule())
	}

	return fx.Module(
		"telegofx",
		options...,
	)
}

// RouterModule returns an fx module that provides a Router and runs it.
func RouterModule() fx.Option {
	return fx.Module(
		"router",
		logger.WithNamedLogger("router"),
		fx.Provide(NewRouter),
		fx.Invoke(func(lc fx.Lifecycle, router *Router, logger *zap.Logger, sh fx.Shutdowner) {
			lc.Append(newRouterHook(router, logger, sh))
		}),
	)
}

// newBotHook returns the lifecycle hook that runs the Bot and shuts it down.
func newBotHook(bot *Bot, logger *zap.Logger, sh fx.Shutdowner) fx.Hook {
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	return fx.Hook{
		OnStart: func(_ context.Context) error {
			go func() {
				if err := bot.Run(runCtx); err != nil {
					logger.Error("bot run failed", zap.Error(err))
					if shErr := sh.Shutdown(); shErr != nil {
						logger.Error("shutdown failed", zap.Error(shErr))
					}
				}
				close(done)
			}()

			logger.Info("bot started")
			return nil
		},
		OnStop: func(ctx context.Context) error {
			cancel()
			select {
			case <-done:
			case <-ctx.Done():
				logger.Warn("bot stop timed out")
				return fmt.Errorf("bot stop timed out: %w", ctx.Err())
			}
			logger.Info("bot stopped")
			return nil
		},
	}
}

// newRouterHook returns the lifecycle hook that runs the Router and shuts it
// down.
func newRouterHook(router *Router, logger *zap.Logger, sh fx.Shutdowner) fx.Hook {
	return fx.Hook{
		OnStart: func(_ context.Context) error {
			go func() {
				if err := router.Start(); err != nil {
					logger.Error("router start failed", zap.Error(err))
					if shErr := sh.Shutdown(); shErr != nil {
						logger.Error("shutdown failed", zap.Error(shErr))
					}
				}
			}()

			logger.Info("router started")
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if err := router.Stop(ctx); err != nil {
				return fmt.Errorf("stop router: %w", err)
			}
			logger.Info("router stopped")
			return nil
		},
	}
}
