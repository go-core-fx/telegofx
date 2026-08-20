package telegofx

type Config struct {
	Token string

	// ProxyURL is an optional SOCKS5 proxy address,
	// e.g. "socks5://user:pass@host:port" or "socks5h://host:port".
	// The "socks5h" scheme resolves hostnames via the proxy.
	// Empty value means a direct connection.
	ProxyURL string
}
