package fetch_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sadewadee/foxhound/fetch"
)

// TestPagePool_AcquireRelease verifies basic acquire and release cycle.
func TestPagePool_AcquireRelease(t *testing.T) {
	var created atomic.Int64
	pool := fetch.NewPagePool(4,
		func() (any, error) {
			created.Add(1)
			return "page", nil
		},
		func(p any) error {
			return nil
		},
	)
	defer pool.Close()

	page, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if page != "page" {
		t.Errorf("got %v, want 'page'", page)
	}

	pool.Release(page)

	// Verify it can be re-acquired.
	page2, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	pool.Release(page2)

	// Should have created only 1 page (reused from pool).
	if created.Load() != 1 {
		t.Errorf("created = %d, want 1 (should reuse)", created.Load())
	}
}

// TestPagePool_MaxSize verifies the pool respects max size.
func TestPagePool_MaxSize(t *testing.T) {
	pool := fetch.NewPagePool(2,
		func() (any, error) { return "page", nil },
		func(p any) error { return nil },
	)
	defer pool.Close()

	// Acquire 2 pages (max size).
	p1, _ := pool.Acquire(context.Background())
	p2, _ := pool.Acquire(context.Background())

	// Third acquire should block until release or timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := pool.Acquire(ctx)
	if err == nil {
		t.Error("expected timeout when pool is exhausted")
	}

	pool.Release(p1)
	pool.Release(p2)
}

// TestPagePool_ConcurrentAccess verifies thread safety.
func TestPagePool_ConcurrentAccess(t *testing.T) {
	pool := fetch.NewPagePool(4,
		func() (any, error) { return "page", nil },
		func(p any) error { return nil },
	)
	defer pool.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := pool.Acquire(context.Background())
			if err != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
			pool.Release(p)
		}()
	}
	wg.Wait()
}

// TestPagePool_WarmUp pre-creates pages.
func TestPagePool_WarmUp(t *testing.T) {
	var created atomic.Int64
	pool := fetch.NewPagePool(4,
		func() (any, error) {
			created.Add(1)
			return "page", nil
		},
		func(p any) error { return nil },
	)
	defer pool.Close()

	n := pool.WarmUp(3)
	if n != 3 {
		t.Errorf("WarmUp returned %d, want 3", n)
	}

	stats := pool.Stats()
	if stats.Idle != 3 {
		t.Errorf("idle pages = %d, want 3", stats.Idle)
	}
}

// TestPagePool_ResetOnRelease verifies reset function is called.
func TestPagePool_ResetOnRelease(t *testing.T) {
	var resetCount atomic.Int64
	pool := fetch.NewPagePool(2,
		func() (any, error) { return "page", nil },
		func(p any) error { return nil },
		fetch.WithPageReset(func(p any) error {
			resetCount.Add(1)
			return nil
		}),
	)
	defer pool.Close()

	p, _ := pool.Acquire(context.Background())
	pool.Release(p)

	if resetCount.Load() != 1 {
		t.Errorf("reset called %d times, want 1", resetCount.Load())
	}
}

// TestPagePool_ResetFailure destroys the page when reset fails.
func TestPagePool_ResetFailure(t *testing.T) {
	var destroyed atomic.Int64
	pool := fetch.NewPagePool(2,
		func() (any, error) { return "page", nil },
		func(p any) error {
			destroyed.Add(1)
			return nil
		},
		fetch.WithPageReset(func(p any) error {
			return errors.New("reset failed")
		}),
	)
	defer pool.Close()

	p, _ := pool.Acquire(context.Background())
	pool.Release(p)

	if destroyed.Load() != 1 {
		t.Errorf("page should be destroyed on reset failure, got %d destroys", destroyed.Load())
	}
}

// TestPagePool_Stats returns correct statistics.
func TestPagePool_Stats(t *testing.T) {
	pool := fetch.NewPagePool(4,
		func() (any, error) { return "page", nil },
		func(p any) error { return nil },
	)
	defer pool.Close()

	p, _ := pool.Acquire(context.Background())
	pool.Release(p)

	stats := pool.Stats()
	if stats.MaxSize != 4 {
		t.Errorf("MaxSize = %d, want 4", stats.MaxSize)
	}
	if stats.Created != 1 {
		t.Errorf("Created = %d, want 1", stats.Created)
	}
	if stats.Acquired != 1 {
		t.Errorf("Acquired = %d, want 1", stats.Acquired)
	}
	if stats.Released != 1 {
		t.Errorf("Released = %d, want 1", stats.Released)
	}
}

// TestPagePool_Close prevents further acquisition.
func TestPagePool_Close(t *testing.T) {
	pool := fetch.NewPagePool(2,
		func() (any, error) { return "page", nil },
		func(p any) error { return nil },
	)

	_ = pool.Close()

	_, err := pool.Acquire(context.Background())
	if err == nil {
		t.Error("expected error acquiring from closed pool")
	}
}

// TestPagePool_AcquireWithTimeout convenience method.
func TestPagePool_AcquireWithTimeout(t *testing.T) {
	pool := fetch.NewPagePool(1,
		func() (any, error) { return "page", nil },
		func(p any) error { return nil },
	)
	defer pool.Close()

	// First acquire should succeed quickly.
	p, err := pool.AcquireWithTimeout(time.Second)
	if err != nil {
		t.Fatalf("AcquireWithTimeout: %v", err)
	}

	// Second acquire with short timeout should fail (pool exhausted).
	_, err = pool.AcquireWithTimeout(50 * time.Millisecond)
	if err == nil {
		t.Error("expected timeout on exhausted pool")
	}

	pool.Release(p)
}

// TestPagePool_CreateError handles creation failures.
func TestPagePool_CreateError(t *testing.T) {
	pool := fetch.NewPagePool(2,
		func() (any, error) { return nil, errors.New("cannot create") },
		func(p any) error { return nil },
	)
	defer pool.Close()

	_, err := pool.Acquire(context.Background())
	if err == nil {
		t.Error("expected create error")
	}
}
