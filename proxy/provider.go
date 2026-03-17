package proxy

import (
	"context"
	"log/slog"
)

// Provider is the source of proxy addresses for a pool.
type Provider interface {
	// Proxies returns the current list of available proxies.
	Proxies(ctx context.Context) ([]*Proxy, error)
}

// StaticProvider serves a fixed list of proxy URLs.
type StaticProvider struct {
	urls []string
}

// Static creates a Provider backed by a hardcoded list of proxy URL strings.
// Invalid URLs are logged and skipped rather than causing an error.
func Static(urls []string) Provider {
	return &StaticProvider{urls: urls}
}

// Proxies parses and returns all valid proxies from the static list.
// Entries that cannot be parsed are logged at WARN level and omitted.
func (s *StaticProvider) Proxies(_ context.Context) ([]*Proxy, error) {
	out := make([]*Proxy, 0, len(s.urls))
	for _, raw := range s.urls {
		p, err := Parse(raw)
		if err != nil {
			slog.Warn("proxy: skipping invalid proxy URL", "url", raw, "err", err)
			continue
		}
		out = append(out, p)
	}
	return out, nil
}
