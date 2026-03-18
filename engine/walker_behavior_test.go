package engine_test

// walker_behavior_test.go — TDD tests for behavior wiring into Walker.
//
// RED phase: these tests fail until Walker gains timing/rhythm fields and
// HuntConfig gains BehaviorProfile.

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/engine"
)

// gapRecordingFetcher records the wall-clock duration between successive Fetch
// calls so tests can assert that a non-zero delay was applied between requests.
type gapRecordingFetcher struct {
	resp      *foxhound.Response
	lastFetch atomic.Int64 // Unix nanosecond timestamp of last Fetch entry
	mu        sync.Mutex
	gaps      []time.Duration
}

func (f *gapRecordingFetcher) Fetch(_ context.Context, job *foxhound.Job) (*foxhound.Response, error) {
	now := time.Now().UnixNano()
	prev := f.lastFetch.Swap(now)
	if prev != 0 {
		gap := time.Duration(now - prev)
		f.mu.Lock()
		f.gaps = append(f.gaps, gap)
		f.mu.Unlock()
	}
	r := *f.resp
	r.Job = job
	return &r, nil
}

func (f *gapRecordingFetcher) Close() error { return nil }

func (f *gapRecordingFetcher) Gaps() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]time.Duration, len(f.gaps))
	copy(out, f.gaps)
	return out
}

// TestWalker_AppliesRhythmDelayBetweenRequests verifies that a Walker
// configured with a BehaviorProfile inserts a measurable delay between
// consecutive job executions.
//
// We use the "aggressive" profile (burst delay 200-1500ms) to keep the test
// fast while still being detectable above zero.
func TestWalker_AppliesRhythmDelayBetweenRequests(t *testing.T) {
	q := newMemQueue(32)
	tf := &gapRecordingFetcher{resp: &foxhound.Response{StatusCode: 200, Body: []byte("ok")}}
	processor := &stubProcessor{result: &foxhound.Result{}}

	// Seed exactly 3 jobs so we get at least 2 inter-request gaps to measure.
	for i := 0; i < 3; i++ {
		_ = q.Push(context.Background(), seedJob("https://example.com/"+string(rune('a'+i))))
	}

	h := engine.NewHunt(engine.HuntConfig{
		Name:            "behavior-timing-test",
		Domain:          "example.com",
		Walkers:         1,
		Queue:           q,
		Fetcher:         tf,
		Processor:       processor,
		BehaviorProfile: "aggressive", // burst delay 200ms-1500ms; detectable even in fast CI
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	gaps := tf.Gaps()
	if len(gaps) == 0 {
		t.Fatal("expected at least one inter-request gap to be recorded")
	}

	// Aggressive burst delay is 200ms-1500ms. Any gap >50ms is proof the
	// rhythm delay fired (without it, gaps are <5ms on fast hardware).
	const minExpectedGap = 50 * time.Millisecond
	for i, g := range gaps {
		if g < minExpectedGap {
			t.Errorf("gap[%d] = %v, want >= %v — rhythm delay not applied", i, g, minExpectedGap)
		}
	}
}

// TestWalker_FallsBackToModerateProfileWhenNoneSet verifies that when
// BehaviorProfile is empty the Walker still initialises without panicking
// and processes jobs normally.
func TestWalker_FallsBackToModerateProfileWhenNoneSet(t *testing.T) {
	q := newMemQueue(8)
	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200}}
	processor := &stubProcessor{result: &foxhound.Result{}}

	_ = q.Push(context.Background(), seedJob("https://example.com/"))

	h := engine.NewHunt(engine.HuntConfig{
		Name:            "no-profile-test",
		Domain:          "example.com",
		Walkers:         1,
		Queue:           q,
		Fetcher:         fetcher,
		Processor:       processor,
		BehaviorProfile: "", // empty — must fall back to moderate without panic
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run with empty BehaviorProfile: %v", err)
	}

	if fetcher.CallCount() != 1 {
		t.Errorf("fetcher calls: want 1, got %d", fetcher.CallCount())
	}
}

// TestHuntConfig_BehaviorProfileField verifies that HuntConfig exposes the
// BehaviorProfile string field (compile-time check via struct literal).
func TestHuntConfig_BehaviorProfileField(t *testing.T) {
	cfg := engine.HuntConfig{
		BehaviorProfile: "careful",
	}
	if cfg.BehaviorProfile != "careful" {
		t.Errorf("BehaviorProfile: want %q, got %q", "careful", cfg.BehaviorProfile)
	}
}

// TestWalker_ContextCancelDuringRhythmDelay verifies that context cancellation
// during a rhythm delay causes the walker to exit promptly rather than
// hanging until the full delay elapses.
func TestWalker_ContextCancelDuringRhythmDelay(t *testing.T) {
	q := newMemQueue(100)
	// Flood the queue to keep the walker producing rhythm delays indefinitely.
	for i := 0; i < 50; i++ {
		_ = q.Push(context.Background(), seedJob("https://example.com/"+string(rune('a'+i%26))))
	}

	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200}}
	processor := &stubProcessor{result: &foxhound.Result{}}

	h := engine.NewHunt(engine.HuntConfig{
		Name:            "cancel-during-delay-test",
		Domain:          "example.com",
		Walkers:         1,
		Queue:           q,
		Fetcher:         fetcher,
		Processor:       processor,
		BehaviorProfile: "careful", // longest delays — best for testing cancellation
	})

	start := time.Now()
	go func() {
		time.Sleep(300 * time.Millisecond)
		h.Stop()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_ = h.Run(ctx)
	elapsed := time.Since(start)

	// The careful profile burst delays are 200-1500ms. Stop fires at 300ms.
	// Without context-aware sleeping the walker would hang up to 1.5s extra.
	// Allow 3s total to be safe on slow CI environments.
	if elapsed > 3*time.Second {
		t.Errorf("Walker did not honour context cancellation during rhythm delay: elapsed %v", elapsed)
	}
}
