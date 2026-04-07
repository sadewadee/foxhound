package middleware

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	foxhound "github.com/sadewadee/foxhound"
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

	// Alpha is the EMA smoothing factor (default 0.3). Higher values react faster
	// to latency changes but are more sensitive to outliers.
	Alpha float64
}

// domainThrottle holds per-domain adaptive state.
type domainThrottle struct {
	mu     sync.Mutex
	avgMs  float64 // exponential moving average of response latency in ms
	delay  time.Duration
	config AutoThrottleConfig
	// Ring buffer for outlier dampening
	latencies [10]float64
	latCount  int
	latIdx    int
	// Pre-allocated scratch buffers for dampenOutlier (zero-alloc sort).
	scratch [10]float64
	devBuf  [10]float64
}

// addLatency records a latency sample in the ring buffer.
func (dt *domainThrottle) addLatency(ms float64) {
	dt.latencies[dt.latIdx] = ms
	dt.latIdx = (dt.latIdx + 1) % len(dt.latencies)
	if dt.latCount < len(dt.latencies) {
		dt.latCount++
	}
}

// dampenOutlier clamps the latency to median ± 3*MAD to prevent single spikes
// from swinging the EMA wildly.
func (dt *domainThrottle) dampenOutlier(ms float64) float64 {
	if dt.latCount < 3 {
		return ms // not enough data for dampening
	}

	// Copy into pre-allocated scratch buffer and sort (zero alloc).
	n := dt.latCount
	sorted := dt.scratch[:n]
	for i := 0; i < n; i++ {
		idx := (dt.latIdx - n + i + len(dt.latencies)) % len(dt.latencies)
		sorted[i] = dt.latencies[idx]
	}
	sort.Float64s(sorted)

	// Median
	median := sorted[n/2]
	if n%2 == 0 {
		median = (sorted[n/2-1] + sorted[n/2]) / 2.0
	}

	// MAD (Median Absolute Deviation) using pre-allocated buffer.
	deviations := dt.devBuf[:n]
	for i, v := range sorted {
		deviations[i] = math.Abs(v - median)
	}
	sort.Float64s(deviations)
	mad := deviations[n/2]
	if mad == 0 {
		mad = 1 // avoid zero MAD for constant latencies
	}

	// Clamp to median ± 3*MAD
	lo := median - 3*mad
	hi := median + 3*mad
	if ms < lo {
		return lo
	}
	if ms > hi {
		return hi
	}
	return ms
}

// update recalculates the delay based on the latest response latency and status.
func (dt *domainThrottle) update(latency time.Duration, statusCode int) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	if statusCode == 429 || statusCode == 503 {
		dt.delay = dt.config.MaxDelay
		slog.Debug("autothrottle: spiked to max", "delay", dt.delay, "status", statusCode)
		return
	}

	latMs := float64(latency.Milliseconds())

	// Record in ring buffer and apply outlier dampening
	dt.addLatency(latMs)
	dampened := dt.dampenOutlier(latMs)

	alpha := dt.config.Alpha
	if dt.avgMs == 0 {
		dt.avgMs = dampened
	} else {
		dt.avgMs = alpha*dampened + (1-alpha)*dt.avgMs
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
	if cfg.Alpha <= 0 || cfg.Alpha >= 1 {
		cfg.Alpha = 0.3
	}
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
