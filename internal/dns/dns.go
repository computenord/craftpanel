// Package dns keeps public DNS records in sync for the panel's domain
// mapping: one wildcard record pointing at this host plus SRV records so
// Java players can connect to <server>.<domain> without typing a port.
package dns

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Record is one desired DNS record. Address records (A/AAAA/CNAME) use
// Content; SRV records use Port and Target instead.
type Record struct {
	Type    string // A | AAAA | CNAME | SRV
	Name    string // fully qualified, e.g. "*.mc.example.com"
	Content string
	Port    int
	Target  string
}

// Result summarizes one reconciliation run.
type Result struct {
	Created  int
	Updated  int
	Deleted  int
	Warnings []string
}

// ipServices answer plain-text "what is my IP" requests. The first two are
// IPv4-only on purpose: Minecraft clients need an A record that works for
// everyone, so a dual-stack host must not register its IPv6 address.
var ipServices = []string{
	"https://api.ipify.org",
	"https://ipv4.icanhazip.com",
	"https://checkip.amazonaws.com",
}

// PublicIP detects this host's public IP address, used as the wildcard
// record target when the operator did not pin one (DynDNS style).
func PublicIP(ctx context.Context) (string, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	var lastErr error
	for _, u := range ipServices {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", err
		}
		res, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(res.Body, 128))
		res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s: HTTP %d", u, res.StatusCode)
			continue
		}
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) == nil {
			lastErr = fmt.Errorf("%s: unexpected response", u)
			continue
		}
		return ip, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no IP detection service reachable")
	}
	return "", lastErr
}
