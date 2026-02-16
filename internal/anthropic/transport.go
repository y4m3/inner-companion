package anthropic

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// NewRestrictedTransport creates an http.Transport that only allows connections
// to the specified hosts.
func NewRestrictedTransport(allowedHosts []string) *http.Transport {
	allowed := make(map[string]bool, len(allowedHosts))
	for _, h := range allowedHosts {
		allowed[h] = true
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				host = addr
			}
			if !allowed[host] {
				return nil, fmt.Errorf("connection to %q blocked by network allowlist", host)
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
}
