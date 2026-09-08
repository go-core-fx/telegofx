package telegofx

// Mode selects how the bot receives updates. The zero value (empty string)
// means ModePolling.
type Mode string

const (
	// ModePolling receives updates via getUpdates long polling.
	ModePolling Mode = "polling"

	// ModeWebhook receives updates via a webhook. The application must mount
	// the WebhookHandler on its own HTTP(S) server and call DeleteWebhook
	// before switching back to ModePolling.
	ModeWebhook Mode = "webhook"
)

type Config struct {
	// Token is the bot's API token.
	Token string

	// Mode selects the update-delivery mode and defaults to ModePolling.
	//
	// In ModeWebhook the application is responsible for mounting the
	// WebhookHandler on its own HTTP(S) server and validating incoming
	// requests (including the X-Telegram-Bot-Api-Secret-Token header) before
	// invoking it. Registering the endpoint with Telegram's API is also an
	// application concern: use telego directly, e.g. via
	// telego.WithWebhookSet(ctx, &telego.SetWebhookParams{...}) before
	// starting the module, and call DeleteWebhook before switching back to
	// ModePolling.
	Mode Mode

	// ProxyURL is an optional SOCKS5 proxy address,
	// e.g. "socks5://user:pass@host:port" or "socks5h://host:port".
	// The "socks5h" scheme resolves hostnames via the proxy.
	// Empty value means a direct connection.
	ProxyURL string
}
