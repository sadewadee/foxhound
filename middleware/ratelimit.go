package middleware

import (
	"context"
	"log/slog"
	"sync"

	foxhound "github.com/sadewadee/foxhound"
	"golang.org/x/time/rate"
)

// rateLimitMiddleware enforces a per-domain request rate using token buckets.
type rateLimitMiddleware struct {
	rps   float64
	burst int

	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

// NewRateLimit creates a Middleware that allows requestsPerSec requests per
// second per domain, with a burst of burstSize tokens.
// A separate rate.Limiter is created lazily for each unique domain.
func NewRateLimit(requestsPerSec float64, burstSize int) foxhound.Middleware {
	return &rateLimitMiddleware{
		rps:      requestsPerSec,
		burst:    burstSize,
		limiters: make(map[string]*rate.Limiter),
	}
}

// Wrap returns a Fetcher that waits on the per-domain limiter before
// forwarding the request to next.
func (m *rateLimitMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		domain := job.Domain
		if domain == "" {
			domain = "__default__"
		}

		lim := m.limiterFor(domain)
		if err := lim.Wait(ctx); err != nil {
			return nil, err
		}
		slog.Debug("ratelimit: token acquired", "domain", domain)
		return next.Fetch(ctx, job)
	})
}

// limiterFor returns (or creates) the rate.Limiter for the given domain.
func (m *rateLimitMiddleware) limiterFor(domain string) *rate.Limiter {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.limiters[domain]
	if !ok {
		l = rate.NewLimiter(rate.Limit(m.rps), m.burst)
		m.limiters[domain] = l
		slog.Debug("ratelimit: created limiter", "domain", domain, "rps", m.rps, "burst", m.burst)
	}
	return l
}
