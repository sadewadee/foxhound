package middleware

import (
	"context"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

func TestCircuitBreakerTripsOnFailures(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 0.5,
		MinObservations:  5,
		WindowSize:       10,
		BaseTimeout:      100 * time.Millisecond,
		MaxTimeout:       1 * time.Second,
		MaxTrips:         3,
	})

	// Fetcher that always returns 429
	blocked := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		return &foxhound.Response{StatusCode: 429}, nil
	})

	fetcher := cb.Wrap(blocked)
	job := &foxhound.Job{URL: "https://example.com/page", Domain: "example.com"}

	// Send enough requests to trip the circuit
	for i := 0; i < 10; i++ {
		fetcher.Fetch(context.Background(), job)
	}

	// Next request should be rejected immediately (circuit open)
	resp, _ := fetcher.Fetch(context.Background(), job)
	if resp.StatusCode != 503 {
		t.Fatalf("Expected 503 from open circuit, got %d", resp.StatusCode)
	}
}

func TestCircuitBreakerExponentialBackoff(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	cfg.BaseTimeout = 100 * time.Millisecond
	cfg.MaxTimeout = 10 * time.Second

	cb := &circuitBreakerMiddleware{config: cfg, domains: make(map[string]*domainCircuit)}

	// With ±50% jitter, single samples can overlap between trip levels.
	// Average over many samples to verify the exponential growth trend.
	const samples = 200
	var avg1, avg2, avg3 float64
	for i := 0; i < samples; i++ {
		avg1 += float64(cb.openDuration(1))
		avg2 += float64(cb.openDuration(2))
		avg3 += float64(cb.openDuration(3))
	}
	avg1 /= samples
	avg2 /= samples
	avg3 /= samples

	// Each trip level should roughly double the previous on average
	if avg2 < avg1*1.5 {
		t.Fatalf("Expected avg d2 (%v) > 1.5 * avg d1 (%v)", time.Duration(avg2), time.Duration(avg1))
	}
	if avg3 < avg2*1.5 {
		t.Fatalf("Expected avg d3 (%v) > 1.5 * avg d2 (%v)", time.Duration(avg3), time.Duration(avg2))
	}
}

func TestCircuitBreakerHalfOpenRecovers(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 0.5,
		MinObservations:  3,
		WindowSize:       5,
		BaseTimeout:      50 * time.Millisecond,
		MaxTimeout:       1 * time.Second,
		MaxTrips:         3,
	})

	callCount := 0
	toggle := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		callCount++
		// First 3 calls fail (enough to trip the circuit with MinObservations=3),
		// then subsequent calls succeed (the half-open probe).
		if callCount <= 3 {
			return &foxhound.Response{StatusCode: 429}, nil
		}
		return &foxhound.Response{StatusCode: 200}, nil
	})

	fetcher := cb.Wrap(toggle)
	job := &foxhound.Job{URL: "https://example.com/page", Domain: "example.com"}

	// Trip the circuit: 3 failures reach the fetcher, remaining are rejected at the circuit
	for i := 0; i < 5; i++ {
		fetcher.Fetch(context.Background(), job)
	}

	// Wait for circuit to transition to half-open
	time.Sleep(100 * time.Millisecond)

	// This should be the probe request (callCount > 5, so it succeeds)
	resp, _ := fetcher.Fetch(context.Background(), job)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200 after recovery, got %d", resp.StatusCode)
	}
}

func TestCircuitBreakerMaxTimeout(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	cfg.BaseTimeout = 100 * time.Millisecond
	cfg.MaxTimeout = 500 * time.Millisecond
	cfg.MaxTrips = 10

	cb := &circuitBreakerMiddleware{config: cfg, domains: make(map[string]*domainCircuit)}

	d := cb.openDuration(20) // way beyond MaxTrips
	// Should be capped near MaxTimeout (±50% jitter, max multiplier 1.5)
	max := time.Duration(float64(cfg.MaxTimeout) * 1.55)
	if d > max {
		t.Fatalf("Duration %v exceeded max timeout %v (with jitter allowance)", d, max)
	}
}

func TestCircuitBreakerStaysClosedOnSuccess(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig())

	ok := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		return &foxhound.Response{StatusCode: 200}, nil
	})

	fetcher := cb.Wrap(ok)
	job := &foxhound.Job{URL: "https://example.com/page", Domain: "example.com"}

	for i := 0; i < 50; i++ {
		resp, _ := fetcher.Fetch(context.Background(), job)
		if resp.StatusCode != 200 {
			t.Fatalf("Expected 200 from healthy circuit, got %d at request %d", resp.StatusCode, i)
		}
	}
}
