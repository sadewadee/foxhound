package engine

import (
	"context"
	"log/slog"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/behavior"
)

// Walker is a virtual user that pops jobs from the queue, fetches them,
// processes the responses, runs items through the pipeline chain, and
// writes results — looping until the context is cancelled or the queue is
// drained.
type Walker struct {
	id        string
	hunt      *Hunt
	fetcher   foxhound.Fetcher
	processor foxhound.Processor
	queue     foxhound.Queue
	pipelines []foxhound.Pipeline
	writers   []foxhound.Writer
	retry     *RetryPolicy
	logger    *slog.Logger
	// timing and rhythm drive human-like inter-request delays.
	// Both are initialised from the hunt's BehaviorProfile in newWalker.
	timing *behavior.Timing
	rhythm *behavior.Rhythm
}

// newWalker creates a Walker that shares the Hunt's configured components.
// It initialises timing and rhythm from the hunt's BehaviorProfile, falling
// back to ModerateProfile when the profile name is empty or unrecognised so
// that human-simulation delays are always active.
func newWalker(id string, hunt *Hunt) *Walker {
	profile := behavior.GetProfile(behavior.ProfileName(hunt.config.BehaviorProfile))
	if profile == nil {
		profile = behavior.ModerateProfile()
	}
	return &Walker{
		id:        id,
		hunt:      hunt,
		fetcher:   hunt.fetcher,
		processor: hunt.config.Processor,
		queue:     hunt.config.Queue,
		pipelines: hunt.config.Pipelines,
		writers:   hunt.config.Writers,
		retry:     DefaultRetryPolicy(),
		logger:    slog.With("component", "walker", "walker_id", id),
		timing:    behavior.NewTiming(profile.Timing),
		rhythm:    profile.Rhythm,
	}
}

// Run is the main loop. It exits when ctx is cancelled. The caller's WaitGroup
// must be decremented after Run returns.
func (w *Walker) Run(ctx context.Context) error {
	for {
		job, err := w.queue.Pop(ctx)
		if err != nil {
			// Context cancelled or queue closed — normal exit.
			return nil
		}

		w.processJob(ctx, job)
	}
}

// processJob executes a single job: fetch → process → pipeline → write →
// enqueue discovered jobs. Retries are handled inline.
func (w *Walker) processJob(ctx context.Context, job *foxhound.Job) {
	// Track this walker as in-flight so drainQueue does not cancel the context
	// before discovered jobs are enqueued.
	w.hunt.activeWalkers.Add(1)
	defer w.hunt.activeWalkers.Add(-1)

	// Apply a human-like inter-request delay using the rhythm state machine.
	// The delay is drawn from the active burst/pause phase. Using a select
	// with ctx.Done() ensures cancellation is honoured immediately even when
	// the delay is several seconds long (e.g. careful profile pause phases).
	if w.rhythm != nil {
		delay := w.rhythm.Next()
		if delay > 0 {
			w.logger.Debug("behavior: applying rhythm delay",
				"delay", delay, "state", w.rhythm.State())
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
		}
	}

	var (
		resp     *foxhound.Response
		fetchErr error
	)

	w.logger.Debug("processing job",
		"url", job.URL, "domain", job.Domain, "depth", job.Depth,
		"fetch_mode", job.FetchMode, "priority", job.Priority)

	start := time.Now()
	for attempt := 0; ; attempt++ {
		resp, fetchErr = w.fetcher.Fetch(ctx, job)
		if ctx.Err() != nil {
			return // context cancelled during fetch
		}

		blocked := resp != nil && (resp.StatusCode == 403 || resp.StatusCode == 429)
		duration := time.Since(start)
		w.hunt.stats.RecordRequest(job.Domain, duration, fetchErr, blocked)
		if resp != nil {
			w.hunt.stats.RecordBytes(int64(len(resp.Body)))
		}

		if !w.retry.ShouldRetry(attempt, fetchErr, resp) {
			break
		}

		delay := w.retry.Delay(attempt)
		w.logger.Warn("retrying job",
			"url", job.URL, "attempt", attempt+1, "delay", delay, "err", fetchErr)

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}

	if fetchErr != nil {
		w.logger.Error("fetch failed", "url", job.URL, "err", fetchErr)
		return
	}

	w.logger.Debug("fetch complete",
		"url", job.URL, "status", resp.StatusCode, "bytes", len(resp.Body),
		"duration", time.Since(start), "fetch_mode", resp.FetchMode)

	result, procErr := w.processor.Process(ctx, resp)
	if procErr != nil {
		w.logger.Error("process failed", "url", job.URL, "err", procErr)
		return
	}
	if result == nil {
		return
	}

	w.logger.Debug("process complete",
		"url", job.URL, "items", len(result.Items), "next_jobs", len(result.Jobs))

	// Run each item through the pipeline chain and write survivors.
	for _, item := range result.Items {
		out := w.runPipelines(ctx, item)
		if out == nil {
			continue // item was dropped
		}
		w.writeItem(ctx, out)
		w.hunt.stats.RecordItems(1)
	}

	// Enqueue newly discovered jobs.
	for _, next := range result.Jobs {
		if err := w.queue.Push(ctx, next); err != nil {
			if ctx.Err() != nil {
				return
			}
			w.logger.Error("enqueue failed", "url", next.URL, "err", err)
		}
	}
}

// runPipelines threads item through each pipeline stage in order. Returns nil
// if any stage drops the item (returns nil, nil).
func (w *Walker) runPipelines(ctx context.Context, item *foxhound.Item) *foxhound.Item {
	cur := item
	for _, p := range w.pipelines {
		var err error
		cur, err = p.Process(ctx, cur)
		if err != nil {
			w.logger.Warn("pipeline error", "err", err)
			return nil
		}
		if cur == nil {
			return nil
		}
	}
	return cur
}

// writeItem sends item to every configured Writer. Errors are logged but do
// not stop processing of other items.
func (w *Walker) writeItem(ctx context.Context, item *foxhound.Item) {
	for _, wr := range w.writers {
		if err := wr.Write(ctx, item); err != nil {
			w.logger.Error("write failed", "err", err)
		}
	}
}
