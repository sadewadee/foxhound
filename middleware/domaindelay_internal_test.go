package middleware

import (
	"context"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

func TestDomainDelayRandomizeAppliesJitter(t *testing.T) {
	mw := NewDomainDelay(DomainDelayConfig{
		DefaultDelay: 100 * time.Millisecond,
		Randomize:    true,
	})

	dd := mw.(*domainDelayMiddleware)

	seen := make(map[time.Duration]bool)
	for i := 0; i < 100; i++ {
		d := dd.delayFor("example.com")
		seen[d] = true
		// Verify within clamped log-normal bounds [0.5x, 2.5x]
		lo := time.Duration(float64(100*time.Millisecond) * 0.5)
		hi := time.Duration(float64(100*time.Millisecond) * 2.5)
		if d < lo || d > hi {
			t.Fatalf("delay %v outside [0.5x, 2.5x] range [%v, %v]", d, lo, hi)
		}
	}

	// With 100 samples and continuous jitter, we should see multiple distinct values
	if len(seen) < 5 {
		t.Fatalf("Expected varied delays with Randomize=true, got only %d distinct values", len(seen))
	}
}

func TestDomainDelayNoRandomize(t *testing.T) {
	mw := NewDomainDelay(DomainDelayConfig{
		DefaultDelay: 100 * time.Millisecond,
		Randomize:    false,
	})

	dd := mw.(*domainDelayMiddleware)

	for i := 0; i < 10; i++ {
		d := dd.delayFor("example.com")
		if d != 100*time.Millisecond {
			t.Fatalf("Expected exact delay without Randomize, got %v", d)
		}
	}
}

func TestDomainDelayPerDomain(t *testing.T) {
	mw := NewDomainDelay(DomainDelayConfig{
		DefaultDelay: 1 * time.Second,
		PerDomain: map[string]time.Duration{
			"fast.com": 200 * time.Millisecond,
		},
		Randomize: false,
	})

	dd := mw.(*domainDelayMiddleware)

	if d := dd.delayFor("fast.com"); d != 200*time.Millisecond {
		t.Fatalf("Expected per-domain delay 200ms, got %v", d)
	}
	if d := dd.delayFor("other.com"); d != 1*time.Second {
		t.Fatalf("Expected default delay 1s, got %v", d)
	}
}

func TestDomainDelayEnforcesMinGap(t *testing.T) {
	delay := 50 * time.Millisecond
	mw := NewDomainDelay(DomainDelayConfig{
		DefaultDelay: delay,
		Randomize:    false,
	})

	noop := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		return &foxhound.Response{StatusCode: 200}, nil
	})

	fetcher := mw.Wrap(noop)
	job := &foxhound.Job{URL: "https://example.com/page", Domain: "example.com"}

	start := time.Now()
	fetcher.Fetch(context.Background(), job)
	fetcher.Fetch(context.Background(), job)
	elapsed := time.Since(start)

	// Second request should have been delayed
	if elapsed < delay {
		t.Fatalf("Expected at least %v between requests, got %v", delay, elapsed)
	}
}
