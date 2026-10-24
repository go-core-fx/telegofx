package telegofx

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	ta "github.com/mymmrac/telego/telegoapi"
)

// activityCaller wraps a [ta.Caller] and records the time of the last
// COMPLETED call - success or failure. A call that never returns (the
// stalled-request failure mode) never records, which is exactly the signal
// liveness needs: external outages keep failing fast and stay green, while
// a wedge goes silent. The record backs Bot.LastAPIActivity and the
// healthfx provider registered via WithHealth.
type activityCaller struct {
	inner ta.Caller

	lastNano atomic.Int64
}

// newActivityCaller returns a recording caller delegating to inner.
func newActivityCaller(inner ta.Caller) *activityCaller {
	return &activityCaller{
		inner:    inner,
		lastNano: atomic.Int64{},
	}
}

// Call implements [ta.Caller]: it delegates, records the completion time on
// every outcome, and wraps transport failures.
func (c *activityCaller) Call(ctx context.Context, url string, data *ta.RequestData) (*ta.Response, error) {
	response, err := c.inner.Call(ctx, url, data)
	c.lastNano.Store(time.Now().UnixNano())

	if err != nil {
		return nil, fmt.Errorf("bot api call: %w", err)
	}

	return response, nil
}

// lastActivity returns the time of the last completed call, or the zero
// time while no call has completed yet.
func (c *activityCaller) lastActivity() time.Time {
	nano := c.lastNano.Load()
	if nano == 0 {
		return time.Time{}
	}

	return time.Unix(0, nano)
}
