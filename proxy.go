package telegofx

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpproxy"
)

var (
	errUnsupportedProxyScheme = errors.New("unsupported proxy scheme")
	errEmptyProxyHost         = errors.New("proxy host is empty")
)

// NewSocks5Client returns a fasthttp client that dials through the given
// SOCKS5 proxy. The proxy URL must use the "socks5" or "socks5h" scheme,
// e.g. "socks5://user:pass@host:port". The "socks5h" scheme resolves
// hostnames via the proxy itself.
func NewSocks5Client(proxyURL string) (*fasthttp.Client, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("parse proxy url: %w", err)
	}
	if u.Scheme != "socks5" && u.Scheme != "socks5h" {
		return nil, fmt.Errorf("%w %q, expected socks5 or socks5h", errUnsupportedProxyScheme, u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("%w: %q", errEmptyProxyHost, proxyURL)
	}

	return &fasthttp.Client{
		Dial: fasthttpproxy.FasthttpSocksDialer(proxyURL),
	}, nil
}
