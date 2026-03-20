package fetch

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// PagePool manages a pool of reusable browser pages/tabs. Instead of creating
// a fresh browser context + page for every fetch, the pool maintains a set of
// warm pages that are checked out, used, and returned. This eliminates the
// per-request overhead of context creation (~200ms) and reduces memory churn.
//
// Pages are reset between uses (cookies cleared, navigated to about:blank)
// to prevent session bleed. The pool is safe for concurrent use.
//
// Usage:
//
//	pool := NewPagePool(8, createPageFunc, destroyPageFunc)
//	defer pool.Close()
//
//	page, err := pool.Acquire(ctx)
//	if err != nil { ... }
//	defer pool.Release(page)
type PagePool struct {
	maxSize  int
	create   func() (any, error)
	destroy  func(any) error
	reset    func(any) error
	pages    chan any
	created  atomic.Int64
	acquired atomic.Int64
	released atomic.Int64
	mu       sync.Mutex
	closed   bool
}

// PagePoolOption configures a PagePool.
type PagePoolOption func(*PagePool)

// WithPageReset sets a function called when a page is returned to the pool.
// The reset function should clear cookies, navigate to about:blank, etc.
func WithPageReset(fn func(any) error) PagePoolOption {
	return func(p *PagePool) { p.reset = fn }
}

// NewPagePool creates a pool of reusable browser pages with the given capacity.
// create is called to instantiate a new page; destroy is called when a page is
// evicted or the pool is closed.
func NewPagePool(maxSize int, create func() (any, error), destroy func(any) error, opts ...PagePoolOption) *PagePool {
	if maxSize <= 0 {
		maxSize = 4
	}
	p := &PagePool{
		maxSize: maxSize,
		create:  create,
		destroy: destroy,
		pages:   make(chan any, maxSize),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Acquire checks out a page from the pool. If no pages are available and the
// pool hasn't reached maxSize, a new page is created. If maxSize is reached,
// Acquire blocks until a page is returned or ctx is cancelled.
func (p *PagePool) Acquire(ctx context.Context) (any, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, context.Canceled
	}
	p.mu.Unlock()

	// Try non-blocking first.
	select {
	case page := <-p.pages:
		p.acquired.Add(1)
		return page, nil
	default:
	}

	// Try to atomically claim a creation slot.
	for {
		cur := p.created.Load()
		if cur >= int64(p.maxSize) {
			break
		}
		if p.created.CompareAndSwap(cur, cur+1) {
			page, err := p.create()
			if err != nil {
				p.created.Add(-1) // release the slot on failure
				return nil, err
			}
			p.acquired.Add(1)
			slog.Debug("pagepool: created new page",
				"total", p.created.Load(), "max", p.maxSize)
			return page, nil
		}
	}

	// At capacity — wait for a released page or context cancellation.
	slog.Debug("pagepool: at capacity, waiting for release",
		"capacity", p.maxSize)
	select {
	case page := <-p.pages:
		p.acquired.Add(1)
		return page, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Release returns a page to the pool. If a reset function is configured,
// the page is reset before being made available. If reset fails, the page
// is destroyed and a new slot opens.
func (p *PagePool) Release(page any) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		if p.destroy != nil {
			_ = p.destroy(page)
		}
		return
	}
	p.mu.Unlock()

	// Reset the page if configured.
	if p.reset != nil {
		if err := p.reset(page); err != nil {
			slog.Warn("pagepool: reset failed, destroying page", "err", err)
			if p.destroy != nil {
				_ = p.destroy(page)
			}
			p.created.Add(-1)
			p.released.Add(1)
			return
		}
	}

	p.released.Add(1)

	// Try to return to pool, drop if full.
	select {
	case p.pages <- page:
	default:
		slog.Warn("pagepool: pool full, destroying extra page")
		if p.destroy != nil {
			_ = p.destroy(page)
		}
		p.created.Add(-1)
	}
}

// Close destroys all pooled pages and prevents further acquisitions.
func (p *PagePool) Close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()

	close(p.pages)
	for page := range p.pages {
		if p.destroy != nil {
			_ = p.destroy(page)
		}
	}

	slog.Debug("pagepool: closed",
		"total_created", p.created.Load(),
		"total_acquired", p.acquired.Load(),
		"total_released", p.released.Load())
	return nil
}

// Stats returns pool usage statistics.
func (p *PagePool) Stats() PagePoolStats {
	created := p.created.Load()
	acquired := p.acquired.Load()
	released := p.released.Load()
	return PagePoolStats{
		MaxSize:  p.maxSize,
		Created:  created,
		Acquired: acquired,
		Released: released,
		Idle:     int64(len(p.pages)),
		Busy:     acquired - released,
		Total:    created,
	}
}

// PagePoolStats holds pool usage metrics.
type PagePoolStats struct {
	MaxSize  int
	Created  int64
	Acquired int64
	Released int64
	Idle     int64
	Busy     int64 // currently checked out (Acquired - Released)
	Total    int64 // created and not destroyed
}

// Busy returns the number of pages currently checked out.
func (p *PagePool) Busy() int64 {
	return p.acquired.Load() - p.released.Load()
}

// Free returns the number of idle pages available for immediate acquisition.
func (p *PagePool) Free() int64 {
	return int64(len(p.pages))
}

// Total returns the total number of pages that have been created and not
// yet destroyed.
func (p *PagePool) Total() int64 {
	return p.created.Load()
}

// WarmUp pre-creates n pages in the pool. This reduces latency for the first
// n Acquire calls. Returns the number of pages successfully created.
func (p *PagePool) WarmUp(n int) int {
	if n > p.maxSize {
		n = p.maxSize
	}

	created := 0
	for i := 0; i < n; i++ {
		if p.created.Load() >= int64(p.maxSize) {
			break
		}
		page, err := p.create()
		if err != nil {
			slog.Warn("pagepool: warmup create failed", "err", err, "created_so_far", created)
			break
		}
		p.created.Add(1)
		select {
		case p.pages <- page:
			created++
		default:
			if p.destroy != nil {
				_ = p.destroy(page)
			}
			p.created.Add(-1)
		}
	}

	slog.Debug("pagepool: warmup complete", "requested", n, "created", created)
	return created
}

// AcquireWithTimeout is a convenience wrapper around Acquire with a timeout.
func (p *PagePool) AcquireWithTimeout(timeout time.Duration) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return p.Acquire(ctx)
}
