package telegofx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-core-fx/healthfx"
	"github.com/go-core-fx/logger"
	"github.com/go-core-fx/telegofx"
	"github.com/mymmrac/telego"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

// healthClientTimeout drives both the client under test and the staleness
// threshold (3x), keeping the suite fast without racing the assertions.
const healthClientTimeout = time.Millisecond * 300

// staleWait exceeds healthClientTimeout*3 after the last completed call, so
// the activity check must have gone red by then.
const staleWait = time.Millisecond * 1200

// buildHealthGraph assembles healthfx + telegofx with the given module
// options and returns the graph's bot and aggregated health service. Only
// construction runs: no lifecycle start, so no polling begins.
func buildHealthGraph(
	t *testing.T,
	config telegofx.Config,
	options ...telegofx.ModuleOption,
) (*telegofx.Bot, *healthfx.Service) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			_, _ = w.Write([]byte(getUpdatesResponse))
			return
		}
		_, _ = w.Write([]byte(getMeResponse))
	}))
	t.Cleanup(server.Close)

	botOptions := []telegofx.Option{
		telegofx.WithHTTPClient(&http.Client{Timeout: healthClientTimeout}),
		telegofx.WithBotOptions(telego.WithAPIServer(server.URL)),
	}

	var bot *telegofx.Bot
	var healthSvc *healthfx.Service

	err := fx.New(
		logger.Module(),
		healthfx.Module(),
		telegofx.Module(options...),
		fx.Supply(config),
		fx.Supply(botOptions),
		fx.Supply(healthfx.Version{}),
		fx.Populate(&bot, &healthSvc),
		fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
	).Err()
	if err != nil {
		t.Fatalf("health graph must resolve: %v", err)
	}

	return bot, healthSvc
}

// activityStatus runs the aggregated liveness check and returns the status
// of the "telegram:api" check ("" when the provider registered nothing).
func activityStatus(t *testing.T, healthSvc *healthfx.Service) healthfx.Status {
	t.Helper()

	detail, ok := healthSvc.CheckLiveness(context.Background()).Checks["telegram:api"]
	if !ok {
		return ""
	}

	return detail.Status
}

func TestHealthWithOptionFailsBeforeAnyCall(t *testing.T) {
	t.Parallel()

	_, healthSvc := buildHealthGraph(t,
		telegofx.Config{Token: testToken, Mode: telegofx.ModePolling},
		telegofx.WithHealth(),
	)

	if status := activityStatus(t, healthSvc); status != healthfx.StatusFail {
		t.Fatalf("telegram:api status = %q before any call, want fail", status)
	}
}

func TestHealthWithOptionPassesAfterCallThenFailsWhenStale(t *testing.T) {
	t.Parallel()

	bot, healthSvc := buildHealthGraph(t,
		telegofx.Config{Token: testToken, Mode: telegofx.ModePolling},
		telegofx.WithHealth(),
	)

	if _, err := bot.GetUpdates(context.Background(), &telego.GetUpdatesParams{}); err != nil {
		t.Fatalf("get updates: %v", err)
	}

	if status := activityStatus(t, healthSvc); status != healthfx.StatusPass {
		t.Fatalf("telegram:api status = %q right after a completed call, want pass", status)
	}

	time.Sleep(staleWait)

	if status := activityStatus(t, healthSvc); status != healthfx.StatusFail {
		t.Fatalf("telegram:api status = %q after %s of silence, want fail", status, staleWait)
	}
}

func TestHealthWithoutOptionRegistersNothing(t *testing.T) {
	t.Parallel()

	_, healthSvc := buildHealthGraph(t,
		telegofx.Config{Token: testToken, Mode: telegofx.ModePolling},
	)

	if status := activityStatus(t, healthSvc); status != "" {
		t.Fatalf("telegram:api status = %q without WithHealth, want no check", status)
	}
}

func TestHealthWebhookModeReportsNothing(t *testing.T) {
	t.Parallel()

	_, healthSvc := buildHealthGraph(t,
		telegofx.Config{Token: testToken, Mode: telegofx.ModeWebhook},
		telegofx.WithHealth(),
	)

	if status := activityStatus(t, healthSvc); status != "" {
		t.Fatalf("telegram:api status = %q in webhook mode, want no check", status)
	}
}
