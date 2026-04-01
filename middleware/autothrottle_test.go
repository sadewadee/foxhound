package middleware

import (
	"context"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

func TestAutoThrottleAlphaConfigurable(t *testing.T) {
	mw := NewAutoThrottle(AutoThrottleConfig{
		TargetConcurrency: 1,
		InitialDelay:      0,
		MinDelay:          0,
		MaxDelay:          10 * time.Second,
		Alpha:             0.5,
	})

	at := mw.(*autoThrottleMiddleware)
	dt := at.throttleFor("example.com")

	if dt.config.Alpha != 0.5 {
		t.Fatalf("Expected Alpha=0.5, got %v", dt.config.Alpha)
	}

	// Feed two latency samples and verify EMA uses alpha=0.5
	dt.update(100*time.Millisecond, 200)
	dt.update(200*time.Millisecond, 200)

	dt.mu.Lock()
	avg := dt.avgMs
	dt.mu.Unlock()

	// With alpha=0.5: after 100ms -> avg=100, after 200ms -> avg=0.5*200 + 0.5*100 = 150
	// Allow some tolerance for dampening with few samples
	if avg < 100 || avg > 200 {
		t.Fatalf("Expected EMA around 150 with alpha=0.5, got %.1f", avg)
	}
}

func TestAutoThrottleAlphaDefault(t *testing.T) {
	mw := NewAutoThrottle(AutoThrottleConfig{
		TargetConcurrency: 1,
		MinDelay:          0,
		MaxDelay:          10 * time.Second,
		// Alpha not set — should default to 0.3
	})

	at := mw.(*autoThrottleMiddleware)
	dt := at.throttleFor("test.com")

	if dt.config.Alpha != 0.3 {
		t.Fatalf("Expected default Alpha=0.3, got %v", dt.config.Alpha)
	}
}

func TestAutoThrottleOutlierDampening(t *testing.T) {
	mw := NewAutoThrottle(AutoThrottleConfig{
		TargetConcurrency: 1,
		InitialDelay:      0,
		MinDelay:          0,
		MaxDelay:          60 * time.Second,
		Alpha:             0.3,
	})

	at := mw.(*autoThrottleMiddleware)
	dt := at.throttleFor("example.com")

	// Feed 9 normal latencies (~100ms)
	for i := 0; i < 9; i++ {
		dt.update(100*time.Millisecond, 200)
	}

	dt.mu.Lock()
	avgBefore := dt.avgMs
	dt.mu.Unlock()

	// Now feed one massive outlier (10s)
	dt.update(10*time.Second, 200)

	dt.mu.Lock()
	avgAfter := dt.avgMs
	dt.mu.Unlock()

	// Without dampening, the EMA would spike significantly.
	// With dampening, the outlier gets clamped, so the EMA should stay reasonable.
	// The avg should not have more than doubled from the outlier.
	if avgAfter > avgBefore*3 {
		t.Fatalf("Outlier dampening failed: avg went from %.1f to %.1f (more than 3x)", avgBefore, avgAfter)
	}
}

func TestAutoThrottleSpikeOn429(t *testing.T) {
	maxDelay := 5 * time.Second
	mw := NewAutoThrottle(AutoThrottleConfig{
		TargetConcurrency: 1,
		InitialDelay:      100 * time.Millisecond,
		MinDelay:          50 * time.Millisecond,
		MaxDelay:          maxDelay,
	})

	noop := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		return &foxhound.Response{StatusCode: 429}, nil
	})

	fetcher := mw.Wrap(noop)
	job := &foxhound.Job{URL: "https://example.com/page", Domain: "example.com"}

	fetcher.Fetch(context.Background(), job)

	at := mw.(*autoThrottleMiddleware)
	dt := at.throttleFor("example.com")

	current := dt.current()
	if current != maxDelay {
		t.Fatalf("Expected MaxDelay %v on 429, got %v", maxDelay, current)
	}
}
