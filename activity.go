package telegofx

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// pollingMethodSuffix is the final path segment telego appends to the API
// URL for the long-poll request (apiURL + "/bot" + token + "/" + method,
// including the /test/ variant), which scopes the activity record to the
// receive loop.
const pollingMethodSuffix = "/getUpdates"

// activityTransport wraps an [http.RoundTripper] and records the time of the
// last COMPLETED getUpdates exchange. Scoping to the polling method keeps
// the heartbeat a receive-loop signal: sends and other Bot API calls
// (sendMessage, getMe, ...) do not refresh it, so a stalled polling loop
// goes red even while outbound traffic keeps flowing. Completion is
// deliberate on both outcomes: a transport failure records immediately,
// while a successful response records only once its body finishes (EOF, read
// error, or close) - headers alone must not refresh the heartbeat, or a
// server that flushes headers and stalls the body would keep liveness green
// while getUpdates never completes. A request that never returns never
// records, which is exactly the signal liveness needs: external outages keep
// failing fast and stay green, while a wedge goes silent. The record backs
// Bot.LastAPIActivity and the healthfx provider registered via WithHealth.
type activityTransport struct {
	inner http.RoundTripper

	lastNano atomic.Int64
}

// newActivityTransport returns a recording transport delegating to inner;
// a nil inner selects [http.DefaultTransport].
func newActivityTransport(inner http.RoundTripper) *activityTransport {
	if inner == nil {
		inner = http.DefaultTransport
	}

	return &activityTransport{
		inner:    inner,
		lastNano: atomic.Int64{},
	}
}

// RoundTrip implements [http.RoundTripper]: a transport failure records the
// completion immediately; a successful getUpdates response hands its body to
// an [activityBody] that records when the body completes, so the record
// tracks the whole exchange rather than the arrival of headers. Transport
// failures are wrapped.
func (t *activityTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.inner.RoundTrip(req)
	poll := strings.HasSuffix(req.URL.Path, pollingMethodSuffix)

	if err != nil {
		if poll {
			t.lastNano.Store(time.Now().UnixNano())
		}

		return nil, fmt.Errorf("round trip: %w", err)
	}

	if poll && resp.Body != nil {
		resp.Body = &activityBody{ReadCloser: resp.Body, transport: t}
	}

	return resp, nil
}

// activityBody records its transport's activity when the response body
// completes: a read error (including [io.EOF]) or Close, whichever happens
// first. Close covers callers that finish without reading to EOF - telego
// returns on 5xx after closing the body unread.
type activityBody struct {
	io.ReadCloser

	transport *activityTransport
}

// Read implements [io.Reader]: a completion (EOF or error) records the
// heartbeat before the outcome surfaces to the caller.
func (b *activityBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == nil {
		return n, nil
	}

	b.transport.lastNano.Store(time.Now().UnixNano())

	// io.ReadAll compares EOF by identity, so it must reach the caller
	// unwrapped; every other error carries context instead.
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}

	return n, fmt.Errorf("activity body read: %w", err)
}

// Close implements [io.Closer]: closing the body means the exchange is done
// even when no read reached EOF first.
func (b *activityBody) Close() error {
	err := b.ReadCloser.Close()
	b.transport.lastNano.Store(time.Now().UnixNano())

	if err != nil {
		return fmt.Errorf("activity body close: %w", err)
	}

	return nil
}

// lastActivity returns the time of the last completed getUpdates request, or
// the zero time while none has completed yet.
func (t *activityTransport) lastActivity() time.Time {
	nano := t.lastNano.Load()
	if nano == 0 {
		return time.Time{}
	}

	return time.Unix(0, nano)
}
