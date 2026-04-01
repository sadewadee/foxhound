package middleware

import (
	"context"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/url"
	"sync"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

// DomainDelayConfig configures per-domain download delays.
type DomainDelayConfig struct {
	// DefaultDelay is the base delay between requests to the same domain.
	// Applied to all domains unless overridden.
	DefaultDelay time.Duration
	// PerDomain overrides the default delay for specific domains.
	PerDomain map[string]time.Duration
	// Randomize adds log-normal jitter to the delay to appear more human.
	Randomize bool
}

// domainDelayMiddleware enforces a minimum time gap between requests to the
// same domain. This prevents overwhelming a single target even when the
// global concurrency limit allows more requests.
type domainDelayMiddleware struct {
	defaultDelay time.Duration
	perDomain    map[string]time.Duration
	randomize    bool

	mu       sync.Mutex
	lastReqs map[string]time.Time
}

// NewDomainDelay creates a Middleware that enforces per-domain download delays.
// The delay is enforced as a minimum interval between consecutive requests to
// the same domain.
//
// Example:
//
//	middleware.NewDomainDelay(middleware.DomainDelayConfig{
//	    DefaultDelay: 2 * time.Second,
//	    PerDomain: map[string]time.Duration{
//	        "api.example.com": 500 * time.Millisecond,
//	    },
//	    Randomize: true,
//	})
func NewDomainDelay(cfg DomainDelayConfig) foxhound.Middleware {
	if cfg.DefaultDelay <= 0 {
		cfg.DefaultDelay = time.Second
	}
	if cfg.PerDomain == nil {
		cfg.PerDomain = make(map[string]time.Duration)
	}
	return &domainDelayMiddleware{
		defaultDelay: cfg.DefaultDelay,
		perDomain:    cfg.PerDomain,
		randomize:    cfg.Randomize,
		lastReqs:     make(map[string]time.Time),
	}
}

// Wrap returns a Fetcher that sleeps when requests to the same domain arrive
// too quickly.
func (d *domainDelayMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		domain := d.extractDomain(job)
		delay := d.delayFor(domain)

		d.mu.Lock()
		last, exists := d.lastReqs[domain]
		now := time.Now()
		if exists {
			elapsed := now.Sub(last)
			if elapsed < delay {
				wait := delay - elapsed
				d.mu.Unlock()

				slog.Debug("domain-delay: throttling request",
					"domain", domain, "delay", wait, "url", job.URL)

				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return nil, ctx.Err()
				}

				d.mu.Lock()
			}
		}
		d.lastReqs[domain] = time.Now()
		d.mu.Unlock()

		return next.Fetch(ctx, job)
	})
}

// delayFor returns the configured delay for the given domain.
func (d *domainDelayMiddleware) delayFor(domain string) time.Duration {
	base := d.defaultDelay
	if delay, ok := d.perDomain[domain]; ok {
		base = delay
	}
	if d.randomize {
		// Log-normal jitter centered on 1.0 with sigma=0.3 gives CV ~0.31,
		// closer to real human inter-request time variance than uniform ±25%.
		// The result scales the base delay by a factor with mode ~0.96, mean ~1.05.
		factor := math.Exp(rand.NormFloat64() * 0.3)
		// Clamp to [0.5, 2.5] to prevent extreme outliers
		if factor < 0.5 {
			factor = 0.5
		}
		if factor > 2.5 {
			factor = 2.5
		}
		base = time.Duration(float64(base) * factor)
	}
	return base
}

// extractDomain gets the domain from the job, preferring the Domain field.
func (d *domainDelayMiddleware) extractDomain(job *foxhound.Job) string {
	if job.Domain != "" {
		return job.Domain
	}
	if u, err := url.Parse(job.URL); err == nil {
		return u.Hostname()
	}
	return "unknown"
}
