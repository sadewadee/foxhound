package middleware_test

import (
	"context"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/middleware"
)

// TestDomainDelay_EnforcesMinimumInterval verifies that the domain delay
// middleware enforces the configured minimum interval between requests
// to the same domain.
func TestDomainDelay_EnforcesMinimumInterval(t *testing.T) {
	md := middleware.NewDomainDelay(middleware.DomainDelayConfig{
		DefaultDelay: 100 * time.Millisecond,
	})

	// Stub fetcher that records call timestamps.
	var calls []time.Time
	stub := foxhound.FetcherFunc(func(_ context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		calls = append(calls, time.Now())
		return &foxhound.Response{StatusCode: 200, Job: job}, nil
	})

	wrapped := md.Wrap(stub)
	ctx := context.Background()

	job := &foxhound.Job{URL: "https://example.com/1", Domain: "example.com"}
	job2 := &foxhound.Job{URL: "https://example.com/2", Domain: "example.com"}

	_, _ = wrapped.Fetch(ctx, job)
	_, _ = wrapped.Fetch(ctx, job2)

	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}

	gap := calls[1].Sub(calls[0])
	if gap < 80*time.Millisecond {
		t.Errorf("gap between same-domain requests = %v, want >= 80ms (100ms with margin)", gap)
	}
}

// TestDomainDelay_DifferentDomainsNotThrottled verifies that requests to
// different domains are not throttled against each other.
func TestDomainDelay_DifferentDomainsNotThrottled(t *testing.T) {
	md := middleware.NewDomainDelay(middleware.DomainDelayConfig{
		DefaultDelay: 500 * time.Millisecond,
	})

	stub := foxhound.FetcherFunc(func(_ context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		return &foxhound.Response{StatusCode: 200, Job: job}, nil
	})

	wrapped := md.Wrap(stub)
	ctx := context.Background()

	start := time.Now()
	_, _ = wrapped.Fetch(ctx, &foxhound.Job{URL: "https://a.com", Domain: "a.com"})
	_, _ = wrapped.Fetch(ctx, &foxhound.Job{URL: "https://b.com", Domain: "b.com"})
	elapsed := time.Since(start)

	// Different domains should not be delayed.
	if elapsed > 200*time.Millisecond {
		t.Errorf("different domain requests took %v, expected < 200ms", elapsed)
	}
}

// TestDomainDelay_PerDomainOverride verifies per-domain delay configuration.
func TestDomainDelay_PerDomainOverride(t *testing.T) {
	md := middleware.NewDomainDelay(middleware.DomainDelayConfig{
		DefaultDelay: 500 * time.Millisecond,
		PerDomain: map[string]time.Duration{
			"fast.com": 50 * time.Millisecond,
		},
	})

	var calls []time.Time
	stub := foxhound.FetcherFunc(func(_ context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		calls = append(calls, time.Now())
		return &foxhound.Response{StatusCode: 200, Job: job}, nil
	})

	wrapped := md.Wrap(stub)
	ctx := context.Background()

	// Two requests to the overridden domain.
	_, _ = wrapped.Fetch(ctx, &foxhound.Job{URL: "https://fast.com/1", Domain: "fast.com"})
	_, _ = wrapped.Fetch(ctx, &foxhound.Job{URL: "https://fast.com/2", Domain: "fast.com"})

	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}

	gap := calls[1].Sub(calls[0])
	// Should use the override delay (50ms) not the default (500ms).
	if gap > 200*time.Millisecond {
		t.Errorf("gap = %v, expected close to 50ms (not 500ms default)", gap)
	}
}

// TestDomainDelay_ContextCancellation verifies that the delay respects
// context cancellation.
func TestDomainDelay_ContextCancellation(t *testing.T) {
	md := middleware.NewDomainDelay(middleware.DomainDelayConfig{
		DefaultDelay: 5 * time.Second,
	})

	stub := foxhound.FetcherFunc(func(_ context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		return &foxhound.Response{StatusCode: 200, Job: job}, nil
	})

	wrapped := md.Wrap(stub)

	// First request (no delay).
	ctx := context.Background()
	_, _ = wrapped.Fetch(ctx, &foxhound.Job{URL: "https://example.com/1", Domain: "example.com"})

	// Second request: cancel before the 5s delay elapses.
	ctx2, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := wrapped.Fetch(ctx2, &foxhound.Job{URL: "https://example.com/2", Domain: "example.com"})
	if err == nil {
		t.Error("expected context cancellation error")
	}
}
