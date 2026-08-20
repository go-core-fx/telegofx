package telegofx_test

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/go-core-fx/telegofx"
)

const testToken = "123456:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestNewSocks5ClientInvalidScheme(t *testing.T) {
	t.Parallel()

	_, err := telegofx.NewSocks5Client("http://127.0.0.1:1080")
	if err == nil {
		t.Fatal("expected error for non-socks5 scheme")
	}
	if !strings.Contains(err.Error(), "unsupported proxy scheme") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewSocks5ClientEmptyHost(t *testing.T) {
	t.Parallel()

	_, err := telegofx.NewSocks5Client("socks5://:1080")
	if err == nil {
		t.Fatal("expected error for empty proxy host")
	}
}

func TestNewSocks5ClientMalformed(t *testing.T) {
	t.Parallel()

	_, err := telegofx.NewSocks5Client("://bad")
	if err == nil {
		t.Fatal("expected error for malformed proxy url")
	}
}

func TestNewSocks5ClientValid(t *testing.T) {
	t.Parallel()

	client, err := telegofx.NewSocks5Client("socks5://user:pass@127.0.0.1:1080")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if client == nil || client.Dial == nil {
		t.Fatal("expected client with dial function")
	}
}

func TestNewSocks5ClientDialThroughProxy(t *testing.T) {
	t.Parallel()

	server := startTestSocks5Server(t)

	client, err := telegofx.NewSocks5Client("socks5://" + server.addr())
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	const target = "203.0.113.1:443"

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	conn, err := client.Dial(target)
	if err != nil {
		t.Fatalf("dial through proxy: %v", err)
	}
	defer conn.Close()

	select {
	case got := <-server.targets:
		if got != target {
			t.Fatalf("proxy received target %q, want %q", got, target)
		}
	case <-ctx.Done():
		t.Fatal("proxy did not receive request")
	}
}

func TestNewWithProxyURLRejectsInvalidProxy(t *testing.T) {
	t.Parallel()

	const token = testToken

	_, err := telegofx.New(telegofx.Config{Token: token, ProxyURL: "http://127.0.0.1:1080"}, nil, zap.NewNop())
	if err == nil {
		t.Fatal("expected error for invalid proxy url")
	}
	if !strings.Contains(err.Error(), "unsupported proxy scheme") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewWithoutProxyURL(t *testing.T) {
	t.Parallel()

	const token = testToken

	bot, err := telegofx.New(telegofx.Config{Token: token, ProxyURL: ""}, nil, zap.NewNop())
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}
	if bot == nil {
		t.Fatal("expected bot")
	}
}

type testSocks5Server struct {
	ln      net.Listener
	targets chan string
}

func startTestSocks5Server(t *testing.T) *testSocks5Server {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	server := &testSocks5Server{ln: ln, targets: make(chan string, 1)}
	go server.serve()

	t.Cleanup(func() { _ = ln.Close() })

	return server
}

func (s *testSocks5Server) addr() string {
	return s.ln.Addr().String()
}

func (s *testSocks5Server) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			_ = s.handleConn(c)
			_ = c.Close()
		}(conn)
	}
}

func (s *testSocks5Server) handleConn(conn net.Conn) error {
	reader := bufio.NewReader(conn)

	var greeting [2]byte
	if _, err := io.ReadFull(reader, greeting[:]); err != nil {
		return err
	}
	methods := make([]byte, int(greeting[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return err
	}

	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return err
	}

	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return err
	}

	host, err := readSocks5Host(reader, header[3])
	if err != nil {
		return err
	}

	var portBytes [2]byte
	if _, rdErr := io.ReadFull(reader, portBytes[:]); rdErr != nil {
		return rdErr
	}
	port := binary.BigEndian.Uint16(portBytes[:])

	s.targets <- net.JoinHostPort(host, strconv.Itoa(int(port)))

	_, err = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})

	return err
}

func readSocks5Host(reader *bufio.Reader, addressType byte) (string, error) {
	switch addressType {
	case 0x01:
		var ip [4]byte
		if _, err := io.ReadFull(reader, ip[:]); err != nil {
			return "", err
		}
		return net.IP(ip[:]).String(), nil
	case 0x03:
		var length [1]byte
		if _, err := io.ReadFull(reader, length[:]); err != nil {
			return "", err
		}
		name := make([]byte, int(length[0]))
		if _, err := io.ReadFull(reader, name); err != nil {
			return "", err
		}
		return string(name), nil
	case 0x04:
		var ip [16]byte
		if _, err := io.ReadFull(reader, ip[:]); err != nil {
			return "", err
		}
		return net.IP(ip[:]).String(), nil
	default:
		return "", fmt.Errorf("unsupported address type %d", addressType)
	}
}
