package middleware

import (
	"context"
	"log/slog"
	"sync"
	"time"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// AutoThrottleConfig holds tuning parameters for the adaptive throttle.
type AutoThrottleConfig struct {
	// TargetConcurrency is the desired parallel request count per domain.
	// The algorithm targets: delay = avgLatency / TargetConcurrency.
	TargetConcurrency float64

	// InitialDelay is the starting inter-request delay per domain.
	InitialDelay time.Duration

	// MinDelay is the floor delay; the computed delay will not go below this.
	MinDelay time.Duration

	// MaxDelay is the ceiling delay. A 429 or 503 response spikes to MaxDelay.
	MaxDelay time.Duration
}

// domainThrottle holds per-domain adaptive state.
type domainThrottle struct {
	mu     sync.Mutex
	avgMs  float64 // exponential moving average of response latency in ms
	delay  time.Duration
	config AutoThrottleConfig
}

// update recalculates the delay based on the latest response latency and status.
func (dt *domainThrottle) update(latency time.Duration, statusCode int) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	// Spike to MaxDelay on server-side throttle signals.
	if statusCode == 429 || statusCode == 503 {
		dt.delay = dt.config.MaxDelay
		slog.Debug("autothrottle: spiked to max", "delay", dt.delay, "status", statusCode)
		return
	}

	const alpha = 0.3 // EMA smoothing factor
	latMs := float64(latency.Milliseconds())
	if dt.avgMs == 0 {
		dt.avgMs = latMs
	} else {
		dt.avgMs = alpha*latMs + (1-alpha)*dt.avgMs
	}

	tc := dt.config.TargetConcurrency
	if tc <= 0 {
		tc = 1
	}
	computed := time.Duration(dt.avgMs/tc) * time.Millisecond

	if computed < dt.config.MinDelay {
		computed = dt.config.MinDelay
	}
	if computed > dt.config.MaxDelay {
		computed = dt.config.MaxDelay
	}
	dt.delay = computed
	slog.Debug("autothrottle: updated delay", "avg_latency_ms", dt.avgMs, "delay", dt.delay)
}

// current returns the current delay, protected by the lock.
func (dt *domainThrottle) current() time.Duration {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return dt.delay
}

// autoThrottleMiddleware implements foxhound.Middleware with per-domain
// adaptive delay based on observed response latency.
type autoThrottleMiddleware struct {
	config  AutoThrottleConfig
	mu      sync.Mutex
	domains map[string]*domainThrottle
}

// NewAutoThrottle creates an adaptive throttle middleware.
//
// After each response the delay for that domain is recomputed:
//   - Normal response: delay = EMA(latency) / TargetConcurrency
//   - 429 or 503: delay spikes to MaxDelay immediately.
//   - Delay is clamped to [MinDelay, MaxDelay].
//
// The computed delay is slept before the *next* request to that domain.
func NewAutoThrottle(cfg AutoThrottleConfig) foxhound.Middleware {
	return &autoThrottleMiddleware{
		config:  cfg,
		domains: make(map[string]*domainThrottle),
	}
}

// Wrap returns a Fetcher that enforces per-domain adaptive delays.
func (at *autoThrottleMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		domain := job.Domain
		if domain == "" {
			domain = "__default__"
		}

		dt := at.throttleFor(domain)

		// Sleep the current delay before issuing the request.
		delay := dt.current()
		if delay > 0 {
			slog.Debug("autothrottle: sleeping", "domain", domain, "delay", delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		start := time.Now()
		resp, err := next.Fetch(ctx, job)
		elapsed := time.Since(start)

		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		dt.update(elapsed, statusCode)

		return resp, err
	})
}

// throttleFor returns (or lazily creates) the per-domain throttle state.
func (at *autoThrottleMiddleware) throttleFor(domain string) *domainThrottle {
	at.mu.Lock()
	defer at.mu.Unlock()
	dt, ok := at.domains[domain]
	if !ok {
		dt = &domainThrottle{
			config: at.config,
			delay:  at.config.InitialDelay,
		}
		at.domains[domain] = dt
		slog.Debug("autothrottle: created domain state", "domain", domain)
	}
	return dt
}
