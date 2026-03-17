// Package proxy manages HTTP/SOCKS proxy pools, health checking, and rotation
// strategies for the Foxhound scraping framework.
package proxy

import (
	"fmt"
	"net/url"
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
// Accepted formats:
//   - http://user:pass@host:port
//   - socks5://user:pass@host:port
//   - https://host:port
//   - host:port  (assumed http, no auth)
func Parse(raw string) (*Proxy, error) {
	// Try parsing as a full URL first.
	u, err := url.Parse(raw)
	if err == nil && u.Host != "" && u.Scheme != "" {
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

	// Fall back to bare host:port.
	u2, err2 := url.Parse("http://" + raw)
	if err2 == nil && u2.Host != "" && u2.Hostname() != "" && u2.Port() != "" {
		return &Proxy{
			URL:      "http://" + raw,
			Protocol: "http",
			Host:     u2.Hostname(),
			Port:     u2.Port(),
		}, nil
	}

	return nil, fmt.Errorf("proxy: cannot parse %q", raw)
}
