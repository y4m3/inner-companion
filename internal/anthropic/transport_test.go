package anthropic

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestRestrictedTransport_AllowedHost(t *testing.T) {
	// Start a local TCP listener to simulate an allowed host
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	_, port, _ := net.SplitHostPort(listener.Addr().String())
	transport := NewRestrictedTransport([]string{"127.0.0.1"})

	conn, err := transport.DialContext(context.Background(), "tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatalf("expected allowed connection, got error: %v", err)
	}
	conn.Close()
}

func TestRestrictedTransport_BlockedHost(t *testing.T) {
	transport := NewRestrictedTransport([]string{"api.anthropic.com"})

	_, err := transport.DialContext(context.Background(), "tcp", "evil.com:443")
	if err == nil {
		t.Fatal("expected blocked connection, got nil error")
	}
	if !strings.Contains(err.Error(), "blocked by network allowlist") {
		t.Fatalf("expected allowlist error, got: %v", err)
	}
}

func TestRestrictedTransport_MultipleHosts(t *testing.T) {
	transport := NewRestrictedTransport([]string{"api.anthropic.com", "example.com"})

	// Blocked host
	_, err := transport.DialContext(context.Background(), "tcp", "other.com:443")
	if err == nil {
		t.Fatal("expected blocked connection")
	}
}
