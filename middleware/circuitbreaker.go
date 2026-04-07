package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/url"
	"sync"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

// CircuitState represents the current state of a circuit breaker.
type CircuitState int

const (
	// CircuitClosed is the normal operating state — requests pass through.
	CircuitClosed CircuitState = iota
	// CircuitOpen means the circuit has tripped — requests fail immediately.
	CircuitOpen
	// CircuitHalfOpen allows one probe request through to test recovery.
	CircuitHalfOpen
)

// CircuitBreakerConfig controls the circuit breaker behavior.
type CircuitBreakerConfig struct {
	// FailureThreshold is the failure rate (0.0-1.0) that triggers the circuit
	// to open (default 0.5 = 50%).
	FailureThreshold float64
	// MinObservations is the minimum number of requests in the window before
	// the failure rate is evaluated (default 5).
	MinObservations int
	// WindowSize is the number of outcomes tracked in the sliding window (default 20).
	WindowSize int
	// BaseTimeout is the initial open-state duration (default 30s).
	BaseTimeout time.Duration
	// MaxTimeout caps the exponential backoff (default 10min).
	MaxTimeout time.Duration
	// MaxTrips caps the number of consecutive trips before holding at MaxTimeout.
	MaxTrips int
}

// DefaultCircuitBreakerConfig returns sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 0.5,
		MinObservations:  5,
		WindowSize:       20,
		BaseTimeout:      30 * time.Second,
		MaxTimeout:       10 * time.Minute,
		MaxTrips:         8,
	}
}

// outcome records a single request result.
type outcome struct {
	success bool
}

// domainCircuit holds per-domain circuit breaker state.
type domainCircuit struct {
	mu        sync.Mutex
	state     CircuitState
	outcomes  []outcome // ring buffer
	outIdx    int
	outCount  int
	trips     int       // consecutive trip count (resets on successful half-open probe)
	openUntil time.Time // when the circuit should transition to half-open
	probing   bool      // true when a half-open probe is in flight
}

// record adds an outcome and returns the current failure rate.
func (dc *domainCircuit) record(success bool) float64 {
	dc.outcomes[dc.outIdx] = outcome{success: success}
	dc.outIdx = (dc.outIdx + 1) % len(dc.outcomes)
	if dc.outCount < len(dc.outcomes) {
		dc.outCount++
	}

	failures := 0
	for i := 0; i < dc.outCount; i++ {
		if !dc.outcomes[i].success {
			failures++
		}
	}
	return float64(failures) / float64(dc.outCount)
}

// circuitBreakerMiddleware implements per-domain circuit breaking.
type circuitBreakerMiddleware struct {
	config  CircuitBreakerConfig
	mu      sync.Mutex
	domains map[string]*domainCircuit
}

// NewCircuitBreaker creates a circuit breaker middleware.
//
// The circuit breaker monitors the failure rate per domain using a sliding
// window. When failures exceed the threshold, the circuit opens and all
// requests fail immediately for an exponentially increasing duration:
//
//	open_duration = min(BaseTimeout * 2^(trips-1), MaxTimeout) * U(0.5, 1.5)
//
// After the open duration, one probe request is allowed (half-open state).
// If it succeeds, the circuit closes. If it fails, the circuit re-opens
// with an incremented trip count.
func NewCircuitBreaker(cfg CircuitBreakerConfig) foxhound.Middleware {
	if cfg.FailureThreshold <= 0 || cfg.FailureThreshold > 1 {
		cfg.FailureThreshold = 0.5
	}
	if cfg.MinObservations <= 0 {
		cfg.MinObservations = 5
	}
	if cfg.WindowSize <= 0 {
		cfg.WindowSize = 20
	}
	if cfg.BaseTimeout <= 0 {
		cfg.BaseTimeout = 30 * time.Second
	}
	if cfg.MaxTimeout <= 0 {
		cfg.MaxTimeout = 10 * time.Minute
	}
	if cfg.MaxTrips <= 0 {
		cfg.MaxTrips = 8
	}
	return &circuitBreakerMiddleware{
		config:  cfg,
		domains: make(map[string]*domainCircuit),
	}
}

// Wrap returns a Fetcher with circuit breaker protection per domain.
func (cb *circuitBreakerMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		domain := extractDomainCB(job)
		dc := cb.circuitFor(domain)

		dc.mu.Lock()
		switch dc.state {
		case CircuitOpen:
			if time.Now().Before(dc.openUntil) {
				dc.mu.Unlock()
				slog.Debug("circuitbreaker: circuit open, rejecting request",
					"domain", domain, "retry_after", time.Until(dc.openUntil))
				return &foxhound.Response{
					StatusCode: 503,
					Body:       []byte("circuit breaker open for " + domain),
				}, nil
			}
			// Open duration expired — transition to half-open
			dc.state = CircuitHalfOpen
			dc.probing = true
			slog.Debug("circuitbreaker: transitioning to half-open", "domain", domain)
			dc.mu.Unlock()

		case CircuitHalfOpen:
			if dc.probing {
				// Another goroutine is already probing — reject this request
				dc.mu.Unlock()
				slog.Debug("circuitbreaker: half-open probe in progress, rejecting",
					"domain", domain)
				return &foxhound.Response{
					StatusCode: 503,
					Body:       []byte("circuit breaker half-open, probe in progress for " + domain),
				}, nil
			}
			dc.probing = true
			dc.mu.Unlock()

		default: // CircuitClosed
			dc.mu.Unlock()
		}

		// Execute the request
		resp, err := next.Fetch(ctx, job)

		// Determine if this was a success or failure
		success := err == nil && resp != nil &&
			resp.StatusCode != 429 && resp.StatusCode != 503 &&
			resp.StatusCode != 403

		dc.mu.Lock()
		defer dc.mu.Unlock()

		if dc.state == CircuitHalfOpen {
			dc.probing = false
			if success {
				// Recovery — close the circuit and reset sliding window
				dc.state = CircuitClosed
				dc.trips = 0
				dc.outCount = 0
				dc.outIdx = 0
				slog.Info("circuitbreaker: half-open probe succeeded, circuit closed", "domain", domain)
			} else {
				// Still failing — re-open with increased backoff
				dc.trips++
				dc.state = CircuitOpen
				dc.openUntil = time.Now().Add(cb.openDuration(dc.trips))
				slog.Warn("circuitbreaker: half-open probe failed, re-opening",
					"domain", domain, "trips", dc.trips)
			}
			return resp, err
		}

		// CircuitClosed: record outcome and check if we should trip
		failRate := dc.record(success)
		if !success && dc.outCount >= cb.config.MinObservations && failRate > cb.config.FailureThreshold {
			dc.trips++
			dc.state = CircuitOpen
			dc.openUntil = time.Now().Add(cb.openDuration(dc.trips))
			slog.Warn("circuitbreaker: circuit opened",
				"domain", domain,
				"failure_rate", fmt.Sprintf("%.0f%%", failRate*100),
				"trips", dc.trips,
				"open_until", dc.openUntil,
			)
		}

		return resp, err
	})
}

// openDuration computes the exponential backoff duration with jitter.
// Formula: min(baseTimeout * 2^(trips-1), maxTimeout) * U(0.5, 1.5)
func (cb *circuitBreakerMiddleware) openDuration(trips int) time.Duration {
	if trips > cb.config.MaxTrips {
		trips = cb.config.MaxTrips
	}
	base := float64(cb.config.BaseTimeout) * math.Pow(2, float64(trips-1))
	if base > float64(cb.config.MaxTimeout) {
		base = float64(cb.config.MaxTimeout)
	}
	// ±50% jitter to obscure the exponential backoff structure
	jitter := 0.5 + rand.Float64() // range [0.5, 1.5]
	return time.Duration(base * jitter)
}

// circuitFor returns (or lazily creates) the per-domain circuit state.
func (cb *circuitBreakerMiddleware) circuitFor(domain string) *domainCircuit {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	dc, ok := cb.domains[domain]
	if !ok {
		dc = &domainCircuit{
			outcomes: make([]outcome, cb.config.WindowSize),
		}
		cb.domains[domain] = dc
	}
	return dc
}

func extractDomainCB(job *foxhound.Job) string {
	if job.Domain != "" {
		return job.Domain
	}
	if u, err := url.Parse(job.URL); err == nil {
		return u.Hostname()
	}
	return "unknown"
}
