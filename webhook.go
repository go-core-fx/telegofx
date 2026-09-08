package telegofx

import (
	"context"
	"errors"
)

// ErrHandlerNotReady is returned by HandleUpdate while the bot is not running
// in webhook mode or has not finished registering its webhook handler.
var ErrHandlerNotReady = errors.New("handler is not ready")

// WebhookHandler receives a raw webhook update payload. The application must
// mount it on its own HTTP(S) server and invoke it with the request body
// after validating the request (including the optional secret token from the
// X-Telegram-Bot-Api-Secret-Token header).
//
// The handler is provided by telego. It returns an error wrapping ctx.Err()
// when ctx is canceled, and it may block while the internal update buffer is
// full. If updates must be processed after the HTTP request ends, invoke the
// handler with a context that outlives the request, for example
// [context.WithoutCancel].
type WebhookHandler func(ctx context.Context, data []byte) error
