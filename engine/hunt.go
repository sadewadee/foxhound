package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// HuntConfig holds all dependencies and settings for a single scraping
// campaign.
type HuntConfig struct {
	// Name is a human-readable label used in logs and metrics.
	Name string
	// Domain is the primary target domain (used for metrics grouping).
	Domain string
	// Walkers is the number of concurrent virtual-user goroutines.
	Walkers int
	// Seeds are the initial jobs pushed to the queue before walkers start.
	Seeds []*foxhound.Job
	// Processor is the user-supplied response handler.
	Processor foxhound.Processor
	// Fetcher is the base fetcher before middleware wrapping.
	Fetcher foxhound.Fetcher
	// Queue is the job storage backend.
	Queue foxhound.Queue
	// Pipelines are applied to each extracted Item in order.
	Pipelines []foxhound.Pipeline
	// Writers receive items that survive the pipeline chain.
	Writers []foxhound.Writer
	// Middlewares are wrapped around the Fetcher (first middleware is outermost).
	Middlewares []foxhound.Middleware
}

// HuntState represents the lifecycle state of a Hunt.
type HuntState int

const (
	// HuntIdle means the hunt has not started yet.
	HuntIdle HuntState = iota
	// HuntRunning means walkers are actively processing jobs.
	HuntRunning
	// HuntPaused means the hunt is temporarily suspended.
	HuntPaused
	// HuntDone means all jobs have been processed successfully.
	HuntDone
	// HuntFailed means the hunt terminated with an unrecoverable error.
	HuntFailed
)

// String returns a human-readable state name.
func (s HuntState) String() string {
	switch s {
	case HuntIdle:
		return "idle"
	case HuntRunning:
		return "running"
	case HuntPaused:
		return "paused"
	case HuntDone:
		return "done"
	case HuntFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Hunt is the top-level campaign coordinator. It owns the lifecycle of all
// walkers, applies middleware to the fetcher, seeds the queue, drains results,
// and emits stats.
//
// Typical usage:
//
//	h := engine.NewHunt(cfg)
//	if err := h.Run(ctx); err != nil {
//	    log.Fatal(err)
//	}
type Hunt struct {
	config        HuntConfig
	fetcher       foxhound.Fetcher // middleware-wrapped fetcher
	state         HuntState
	stats         *Stats
	cancel        context.CancelFunc
	walkers       []*Walker
	mu            sync.RWMutex
	logger        *slog.Logger
	activeWalkers atomic.Int32
}

// NewHunt creates a Hunt from cfg. It does not start any goroutines; call
// Run to begin processing.
func NewHunt(cfg HuntConfig) *Hunt {
	if cfg.Walkers < 1 {
		cfg.Walkers = 1
	}
	return &Hunt{
		config: cfg,
		state:  HuntIdle,
		stats:  NewStats(),
		logger: slog.With("component", "hunt", "hunt", cfg.Name),
	}
}

// Run executes the campaign and blocks until all jobs are processed or ctx is
// cancelled. It returns nil on clean completion and a non-nil error on failure.
//
// Run lifecycle:
//  1. Build the middleware-wrapped fetcher.
//  2. Push seed jobs to the queue.
//  3. Launch N walker goroutines.
//  4. Wait until the queue is empty AND all walkers are idle.
//  5. Flush all writers.
//  6. Transition state to HuntDone.
func (h *Hunt) Run(ctx context.Context) error {
	if err := h.validateConfig(); err != nil {
		return fmt.Errorf("invalid hunt config: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	h.mu.Lock()
	h.cancel = cancel
	h.state = HuntRunning
	h.fetcher = h.buildFetcher()
	h.mu.Unlock()

	defer cancel()

	h.logger.Info("hunt started",
		"walkers", h.config.Walkers,
		"seeds", len(h.config.Seeds),
		"domain", h.config.Domain,
	)

	// Push seed jobs.
	for _, job := range h.config.Seeds {
		h.logger.Debug("seeding job", "url", job.URL, "fetch_mode", job.FetchMode)
		if err := h.config.Queue.Push(runCtx, job); err != nil {
			h.setState(HuntFailed)
			return fmt.Errorf("seeding queue: %w", err)
		}
	}

	// Launch walkers.
	var wg sync.WaitGroup
	walkers := make([]*Walker, h.config.Walkers)
	for i := 0; i < h.config.Walkers; i++ {
		id := fmt.Sprintf("%s-w%d", h.config.Name, i)
		w := newWalker(id, h)
		walkers[i] = w
		wg.Add(1)
		go func(w *Walker) {
			defer wg.Done()
			if err := w.Run(runCtx); err != nil {
				h.logger.Error("walker exited with error", "walker_id", w.id, "err", err)
			}
		}(w)
	}

	h.mu.Lock()
	h.walkers = walkers
	h.mu.Unlock()

	// Wait for the queue to drain then cancel walker contexts.
	//
	// Strategy: poll the queue length in a tight loop. Once the queue is
	// empty we give walkers a short window to finish their in-flight job,
	// then cancel the context. The walkers exit cleanly on context
	// cancellation.
	h.drainQueue(runCtx, cancel)

	// Wait for all walkers to exit.
	wg.Wait()

	// Flush writers.
	for _, w := range h.config.Writers {
		if err := w.Flush(ctx); err != nil {
			h.logger.Warn("writer flush failed", "err", err)
		}
	}

	h.setState(HuntDone)
	h.logger.Info("hunt complete", "stats", h.stats.Summary())
	return nil
}

// drainPollInterval is how often Hunt checks whether the queue is empty.
var drainPollInterval = 10 * time.Millisecond

// drainSettleDelay is a brief pause applied once both the queue is empty and
// all walkers are idle.  It covers the tiny window between a walker's
// queue.Pop return and its activeWalkers increment.
var drainSettleDelay = 50 * time.Millisecond

// drainQueue polls the queue until it is empty AND no walkers are actively
// processing a job, then cancels the run context so walkers exit cleanly.
//
// The double condition prevents premature cancellation when the last job has
// been popped but the processing walker has not yet enqueued its discovered
// jobs.
func (h *Hunt) drainQueue(ctx context.Context, cancel context.CancelFunc) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if h.config.Queue.Len() == 0 && h.activeWalkers.Load() == 0 {
			// Both conditions met.  Apply a short settling delay to handle the
			// tiny window between Pop returning and activeWalkers being
			// incremented by the walker goroutine.
			select {
			case <-ctx.Done():
				return
			case <-time.After(drainSettleDelay):
			}
			// Re-check after the settle delay — a walker may have incremented
			// activeWalkers or pushed a new job during that window.
			if h.config.Queue.Len() == 0 && h.activeWalkers.Load() == 0 {
				cancel()
				return
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(drainPollInterval):
		}
	}
}

// Pause signals all walkers to suspend work. It transitions the state to
// HuntPaused. Resuming is done via Resume.
func (h *Hunt) Pause() {
	h.setState(HuntPaused)
}

// Resume transitions the hunt back to HuntRunning after a Pause.
func (h *Hunt) Resume() {
	h.setState(HuntRunning)
}

// Stop cancels the hunt context, causing all walkers to exit after finishing
// their current job. Run will return shortly after Stop is called.
func (h *Hunt) Stop() {
	h.mu.RLock()
	cancel := h.cancel
	h.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

// State returns the current HuntState.
func (h *Hunt) State() HuntState {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.state
}

// Stats returns the live statistics for this Hunt.
func (h *Hunt) Stats() *Stats {
	return h.stats
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// buildFetcher wraps h.config.Fetcher in all configured middlewares.
// Middlewares are applied so that the first middleware in the slice is the
// outermost layer (first to intercept, last to return).
func (h *Hunt) buildFetcher() foxhound.Fetcher {
	f := h.config.Fetcher
	// Apply in reverse so index 0 ends up outermost.
	for i := len(h.config.Middlewares) - 1; i >= 0; i-- {
		f = h.config.Middlewares[i].Wrap(f)
	}
	return f
}

// setState sets the hunt state under the write lock.
func (h *Hunt) setState(s HuntState) {
	h.mu.Lock()
	h.state = s
	h.mu.Unlock()
}

// validateConfig returns an error if required fields are missing.
func (h *Hunt) validateConfig() error {
	if h.config.Fetcher == nil {
		return errors.New("Fetcher is required")
	}
	if h.config.Processor == nil {
		return errors.New("Processor is required")
	}
	if h.config.Queue == nil {
		return errors.New("Queue is required")
	}
	return nil
}
