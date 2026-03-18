// Package proxy manages HTTP/SOCKS proxy pools, health checking, and rotation
// strategies for the Foxhound scraping framework.
package proxy

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// RotationStrategy controls when the pool selects a new proxy.
type RotationStrategy int

const (
	// PerRequest rotates the proxy on every outbound request.
	PerRequest RotationStrategy = iota
	// PerSession keeps the same proxy for the lifetime of a walker session.
	PerSession
	// PerDomain keeps the same proxy per target domain.
	PerDomain
	// OnBlock only rotates when a proxy is detected as blocked.
	OnBlock
)

// Proxy holds the parsed coordinates of a single proxy server.
type Proxy struct {
	// URL is the full proxy URL, e.g. "http://user:pass@host:port".
	URL string
	// Protocol is one of "http", "https", or "socks5".
	Protocol string
	// Host is the proxy hostname or IP address.
	Host string
	// Port is the proxy port number as a string.
	Port string
	// Username is the proxy auth username (may be empty).
	Username string
	// Password is the proxy auth password (may be empty).
	Password string
	// Country is the ISO 3166-1 alpha-2 country code of the proxy (optional).
	Country string
	// City is the city name of the proxy (optional).
	City string
}

// ProxyHealth records the current health state of a proxy.
type ProxyHealth struct {
	// Alive indicates whether the proxy is reachable.
	Alive bool
	// Latency is the round-trip time to the proxy check endpoint.
	Latency time.Duration
	// SuccessRate is the fraction of requests that succeeded (0.0–1.0).
	SuccessRate float64
	// BlockRate is the fraction of requests that were blocked (0.0–1.0).
	BlockRate float64
	// BanCount is the total number of domain bans recorded for this proxy.
	BanCount int
	// CooldownUntil is the time after which the proxy may be used again.
	CooldownUntil time.Time
	// Score is an aggregate quality score from 0.0 (dead) to 1.0 (perfect).
	Score float64
}

// Parse converts a raw proxy string into a Proxy value.
//
// Accepted formats (in order of detection):
//   - protocol://user:pass@host:port    — standard URL (already had scheme)
//   - user:pass@host:port               — URL without scheme (http assumed)
//   - host:port                         — bare address (http, no auth)
//   - host:port:user:pass               — colon-separated, port is numeric
//   - user:pass:host:port               — colon-separated, port is last field
//   - protocol:host:port:user:pass      — five colon-separated fields
func Parse(raw string) (*Proxy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("proxy: empty string")
	}

	// Format 1: full URL with scheme.
	if strings.Contains(raw, "://") {
		return parseProxyURL(raw)
	}

	// Format: user:pass@host:port (no scheme).
	if strings.Contains(raw, "@") {
		return parseProxyURL("http://" + raw)
	}

	// Remaining formats use only colon as separator.
	parts := strings.Split(raw, ":")

	switch len(parts) {
	case 2:
		// host:port
		if !isPort(parts[1]) {
			return nil, fmt.Errorf("proxy: unrecognized format %q (port %q is not numeric)", raw, parts[1])
		}
		return &Proxy{
			URL:      "http://" + raw,
			Protocol: "http",
			Host:     parts[0],
			Port:     parts[1],
		}, nil

	case 4:
		// host:port:user:pass  OR  user:pass:host:port
		// Heuristic: if parts[1] is numeric it is a port → host:port:user:pass.
		if isPort(parts[1]) {
			return buildProxy("http", parts[0], parts[1], parts[2], parts[3])
		}
		// Otherwise: user:pass:host:port
		return buildProxy("http", parts[2], parts[3], parts[0], parts[1])

	case 5:
		// protocol:host:port:user:pass
		return buildProxy(parts[0], parts[1], parts[2], parts[3], parts[4])

	default:
		return nil, fmt.Errorf("proxy: unrecognized format %q "+
			"(supported: protocol://user:pass@host:port, host:port, "+
			"host:port:user:pass, user:pass:host:port, protocol:host:port:user:pass)",
			raw)
	}
}

// isPort returns true when s is a non-empty numeric string of at most 5 digits.
func isPort(s string) bool {
	if len(s) == 0 || len(s) > 5 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// buildProxy constructs a Proxy from its individual components, normalising
// the protocol name and assembling the canonical URL.
func buildProxy(protocol, host, port, username, password string) (*Proxy, error) {
	if protocol == "" {
		protocol = "http"
	}
	switch strings.ToLower(protocol) {
	case "http", "https", "socks5", "socks4":
		protocol = strings.ToLower(protocol)
	}
	rawURL := fmt.Sprintf("%s://%s:%s@%s:%s", protocol, username, password, host, port)
	return &Proxy{
		URL:      rawURL,
		Protocol: protocol,
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
	}, nil
}

// parseProxyURL parses a raw proxy URL that already contains a scheme (or has
// had "http://" prepended) using the standard net/url package.
func parseProxyURL(raw string) (*Proxy, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("proxy: invalid URL %q: %v", raw, err)
	}
	p := &Proxy{
		URL:      raw,
		Protocol: u.Scheme,
		Host:     u.Hostname(),
		Port:     u.Port(),
	}
	if u.User != nil {
		p.Username = u.User.Username()
		p.Password, _ = u.User.Password()
	}
	return p, nil
}
