package telegofx

import (
	"context"
	"time"

	"github.com/go-core-fx/healthfx"
)

const (
	// healthProviderName is the name the provider reports to healthfx; the
	// served check key becomes "<healthProviderName>:<healthCheckName>".
	healthProviderName = "telegram"

	// healthCheckName keys the activity check inside the provider.
	healthCheckName = "api"

	// staleAfterMultiplier is how many HTTP client timeouts of silence turn
	// the activity check red. A healthy polling loop completes a getUpdates
	// every long-poll cycle (8s) and a failed request completes too
	// (timeout + 8s retry), so three timeouts of total silence means the
	// loop is gone.
	staleAfterMultiplier = 3

	// defaultAPITimeout is the threshold basis when the HTTP client carries
	// no Timeout. New warns about that configuration; the check still needs
	// a finite bound.
	defaultAPITimeout = 30 * time.Second

	// activityNeverValue is the observed value reported while no getUpdates
	// call has completed yet.
	activityNeverValue = -1

	// activityUnit is the observed-value unit of the activity check.
	activityUnit = "seconds"
)

// botHealth reports getUpdates activity as a liveness check for healthfx.
// It fails while no request has completed for staleAfterMultiplier times the
// HTTP client timeout: that silence means the polling loop stalled without
// ever producing an error, which nothing else in the process can see.
type botHealth struct {
	bot *Bot
}

// newBotHealth returns the liveness provider for bot.
func newBotHealth(bot *Bot) *botHealth {
	return &botHealth{bot: bot}
}

// Name implements [healthfx.Provider].
func (h *botHealth) Name() string {
	return healthProviderName
}

// LiveProbe implements [healthfx.Provider]: it reports seconds since the
// last completed getUpdates request. Webhook delivery makes no periodic
// calls, so the check reports nothing there rather than failing on quiet
// traffic.
func (h *botHealth) LiveProbe(_ context.Context) (healthfx.Checks, error) {
	if h.bot.config.Mode == ModeWebhook {
		return healthfx.Checks{}, nil
	}

	timeout := h.bot.requestTimeout
	if timeout <= 0 {
		timeout = defaultAPITimeout
	}
	staleAfter := staleAfterMultiplier * timeout

	last := h.bot.LastAPIActivity()
	observed := int64(activityNeverValue)
	status := healthfx.StatusFail
	if !last.IsZero() {
		age := time.Since(last)
		observed = int64(age / time.Second)
		if age <= staleAfter {
			status = healthfx.StatusPass
		}
	}

	return healthfx.Checks{
		healthCheckName: {
			Description:   "Seconds since the last completed getUpdates (-1 = never)",
			ObservedUnit:  activityUnit,
			ObservedValue: observed,
			Status:        status,
		},
	}, nil
}

// ReadyProbe implements [healthfx.Provider]; polling readiness is covered by
// the liveness probe and needs no separate signal.
func (h *botHealth) ReadyProbe(_ context.Context) (healthfx.Checks, error) {
	return healthfx.Checks{}, nil
}

// StartedProbe implements [healthfx.Provider]; startup completes with the
// fx graph and needs no separate signal.
func (h *botHealth) StartedProbe(_ context.Context) (healthfx.Checks, error) {
	return healthfx.Checks{}, nil
}
