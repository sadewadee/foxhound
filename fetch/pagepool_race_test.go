package fetch_test

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sadewadee/foxhound/fetch"
)

// newRacePool builds a pool whose pages are cheap ints. create/destroy/reset
// are all no-ops so the test exercises only the pool's own bookkeeping.
func newRacePool(maxSize int) (*fetch.PagePool, *atomic.Int64) {
	var destroyed atomic.Int64
	pool := fetch.NewPagePool(maxSize,
		func() (any, error) { return "page", nil },
		func(any) error { destroyed.Add(1); return nil },
		fetch.WithPageReset(func(any) error { return nil }),
	)
	return pool, &destroyed
}

// TestPagePool_WarmUpDuringClose is the regression test for the WarmUp twin
// of the Release race: WarmUp created pages and then sent them on p.pages
// without holding p.mu and without checking p.closed. A Close landing between
// a create() and its send (e.g. a fetcher restart while WarmUp is still
// filling a fresh pool) panicked with `send on closed channel`. The slow
// create widens that window so the old code fails reliably.
//
// On the fixed code WarmUp holds p.mu across the closed-check and the
// non-blocking send — mirroring Release — and stops warming once the pool is
// closed, so nothing panics regardless of interleaving.
func TestPagePool_WarmUpDuringClose(t *testing.T) {
	const iterations = 80

	for round := 0; round < iterations; round++ {
		var destroyed atomic.Int64
		pool := fetch.NewPagePool(8,
			func() (any, error) {
				// Slow enough for the concurrent Close to land between this
				// create and the send that follows it.
				time.Sleep(time.Millisecond)
				return "page", nil
			},
			func(any) error { destroyed.Add(1); return nil },
			fetch.WithPageReset(func(any) error { return nil }),
		)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.WarmUp(8)
		}()

		// Close while WarmUp is still creating pages.
		_ = pool.Close()
		wg.Wait()

		if got := destroyed.Load(); got == 0 {
			t.Fatalf("round %d: nothing was destroyed — WarmUp made no progress against the closer", round)
		}
	}
}

// TestPagePool_ReleaseDuringClose is the regression test for the production
// panic `panic: send on closed channel` (serp-scraper enrich worker,
// 2026-09-25, stack: PagePool.Release ← CamoufoxFetcher.navigate).
//
// Release used to check p.closed under p.mu, drop the lock, and only then send
// on p.pages. A Close landing in that window closed the channel, and the
// in-flight send panicked. That gap is narrow, so this test widens it: many
// workers hammer Acquire/Release while a closer runs concurrently, repeated
// many times so the old code panics reliably.
//
// On the fixed code Release holds p.mu across the check and the non-blocking
// send, and Close is idempotent, so nothing panics regardless of interleaving.
func TestPagePool_ReleaseDuringClose(t *testing.T) {
	const (
		workers    = 50
		iterations = 60
	)

	for round := 0; round < iterations; round++ {
		pool, _ := newRacePool(4)

		var wg sync.WaitGroup
		stop := make(chan struct{})
		var done atomic.Bool

		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx := context.Background()
				for {
					select {
					case <-stop:
						return
					default:
					}
					page, err := pool.Acquire(ctx)
					if err != nil {
						// Pool closed — that is the expected end state.
						return
					}
					// Yield between Acquire and Release so the closer can
					// land exactly in the window that used to panic.
					runtime.Gosched()
					pool.Release(page)
					runtime.Gosched()
				}
			}()
		}

		// Close exactly once, concurrently with the churn. Calling Close
		// repeatedly would trip the separate "close of closed channel" bug
		// before this test can reach the send-on-closed-channel window it
		// targets. Idempotent Close has its own test. To keep the failure
		// visible if a fix only addresses double-Close, this test closes
		// while workers hold pages out: in-flight Releases after the close
		// are the ones that panic on the old code.
		closerDone := make(chan struct{})
		go func() {
			defer close(closerDone)
			// Wait until workers are mid-churn, then close once.
			for i := 0; i < 200 && !done.Load(); i++ {
				runtime.Gosched()
			}
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("round %d: concurrent Close panicked: %v (double-Close not idempotent)", round, r)
				}
			}()
			_ = pool.Close()
		}()

		// Let the workers churn briefly, then stop and join.
		time.Sleep(2 * time.Millisecond)
		done.Store(true)
		close(stop)
		wg.Wait()
		<-closerDone

		// Close once more. On the fixed code this is a no-op. On the old
		// code it panics with "close of closed channel" — which is itself
		// one of the two defects under test. Recover here only to let the
		// loop keep probing the OTHER defect (send on closed channel in
		// Release) in later rounds; both are recorded.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("round %d: final Close panicked: %v (double-Close not idempotent)", round, r)
				}
			}()
			if err := pool.Close(); err != nil {
				t.Errorf("round %d: final Close: %v", round, err)
			}
		}()
	}
}

// TestPagePool_ReleaseAfterClose is the deterministic single-threaded
// guarantee behind the fix: a page released after Close is destroyed, never
// returned to the closed channel, and the pool's created count never goes
// negative.
func TestPagePool_ReleaseAfterClose(t *testing.T) {
	pool, destroyed := newRacePool(2)

	page, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := pool.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Must not panic — this is the exact call that panicked in prod.
	pool.Release(page)

	if got := destroyed.Load(); got != 1 {
		t.Errorf("destroyed = %d, want 1 (released page must be destroyed, not pooled)", got)
	}
	if got := pool.Total(); got != 0 {
		t.Errorf("Total = %d, want 0 after releasing the only page into a closed pool", got)
	}

	// Close is idempotent.
	if err := pool.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, err := pool.Acquire(context.Background()); err == nil {
		t.Error("Acquire on closed pool should fail")
	}
}

// TestPagePool_ConcurrentCloseIsIdempotent covers two closers racing: before
// the fix the second close(p.pages) panicked with "close of closed channel".
func TestPagePool_ConcurrentCloseIsIdempotent(t *testing.T) {
	pool, _ := newRacePool(4)
	page, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := pool.Close(); err != nil {
				t.Errorf("concurrent Close: %v", err)
			}
		}()
	}
	wg.Wait()
	pool.Release(page)
}

// TestPagePool_ReleaseIsAccountingConsistent makes sure holding p.mu across
// the send did not skew the counters: every acquired page is either back in
// the pool or destroyed, and Busy() returns to zero.
func TestPagePool_ReleaseIsAccountingConsistent(t *testing.T) {
	pool, _ := newRacePool(4)

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				page, err := pool.Acquire(context.Background())
				if err != nil {
					t.Errorf("worker %d: acquire: %v", w, err)
					return
				}
				_ = page
				pool.Release(page)
			}
		}(w)
	}
	wg.Wait()

	if err := pool.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	stats := pool.Stats()

	// Created tracks live slots (capped at maxSize), not lifetime pages, so it
	// is not comparable to the cumulative Acquired count. What must hold after
	// every acquired page was released: no checked-out page remains, and the
	// live slot count never left the pool's bound.
	if stats.Busy != 0 {
		t.Errorf("Busy = %d, want 0 — every acquired page was released (%+v)", stats.Busy, stats)
	}
	if stats.Created < 0 || stats.Created > int64(stats.MaxSize) {
		t.Errorf("Created = %d, want 0..%d (%+v)", stats.Created, stats.MaxSize, stats)
	}
	if stats.Released != stats.Acquired {
		t.Errorf("Released %d != Acquired %d — a release was lost (%+v)",
			stats.Released, stats.Acquired, stats)
	}
}
