// sleep_ctx_test.go — unit tests for sleepCtx, the context-aware sleep helper
// used on the captcha / Cloudflare solve path.

package fetch

import (
	"context"
	"testing"
	"time"
)

// TestSleepCtx_CompletesFullDuration verifies that sleepCtx returns true after
// the full duration elapses when the context is not cancelled.
func TestSleepCtx_CompletesFullDuration(t *testing.T) {
	ctx := context.Background()

	start := time.Now()
	ok := sleepCtx(ctx, 30*time.Millisecond)
	elapsed := time.Since(start)

	if !ok {
		t.Fatalf("sleepCtx returned false, expected true for uncancelled context")
	}
	if elapsed < 30*time.Millisecond {
		t.Errorf("sleepCtx returned too early: %v", elapsed)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("sleepCtx took far longer than requested: %v", elapsed)
	}
}

// TestSleepCtx_ReturnsFalseIfPreCancelled verifies that sleepCtx returns false
// immediately when the context is already cancelled at call time, without
// waiting.
func TestSleepCtx_ReturnsFalseIfPreCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	ok := sleepCtx(ctx, 5*time.Second)
	elapsed := time.Since(start)

	if ok {
		t.Error("sleepCtx returned true on pre-cancelled context, expected false")
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("sleepCtx blocked %v on pre-cancelled context, expected near-zero", elapsed)
	}
}

// TestSleepCtx_ReturnsFalseOnMidSleepCancel verifies that sleepCtx returns
// false promptly when the context is cancelled during the sleep, rather than
// waiting out the full duration.
func TestSleepCtx_ReturnsFalseOnMidSleepCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after 20ms while requesting a 10-second sleep. The helper must
	// return well before the 10s elapses.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	ok := sleepCtx(ctx, 10*time.Second)
	elapsed := time.Since(start)

	if ok {
		t.Error("sleepCtx returned true on cancelled context, expected false")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("sleepCtx did not honor cancellation quickly: took %v", elapsed)
	}
}

// TestSleepCtx_ZeroDuration verifies that sleepCtx returns true immediately
// for a zero or negative duration when the context is live.
func TestSleepCtx_ZeroDuration(t *testing.T) {
	ctx := context.Background()

	start := time.Now()
	ok := sleepCtx(ctx, 0)
	if !ok {
		t.Error("sleepCtx(ctx, 0) returned false, expected true")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Errorf("sleepCtx(ctx, 0) blocked %v, expected near-zero", elapsed)
	}

	ok = sleepCtx(ctx, -1*time.Second)
	if !ok {
		t.Error("sleepCtx(ctx, -1s) returned false, expected true for negative duration")
	}
}

// TestSleepCtx_ZeroDurationOnCancelledContext verifies that cancellation takes
// precedence over the zero-duration fast path: a cancelled context must return
// false even when d <= 0.
func TestSleepCtx_ZeroDurationOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if sleepCtx(ctx, 0) {
		t.Error("sleepCtx(cancelled, 0) returned true, expected false")
	}
}
