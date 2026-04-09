package engine

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/behavior"
	"github.com/sadewadee/foxhound/captcha"
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
		logger:    hunt.logger.With("component", "walker", "walker_id", id),
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

		// Increment activeWalkers before processJob to close the race window
		// between Pop returning and processJob starting where drainQueue could
		// see queue=0 and activeWalkers=0 and terminate prematurely.
		w.hunt.activeWalkers.Add(1)
		w.processJob(ctx, job)
	}
}

// processJob executes a single job: fetch → process → pipeline → write →
// enqueue discovered jobs. Retries are handled inline.
func (w *Walker) processJob(ctx context.Context, job *foxhound.Job) {
	jobStart := time.Now()
	// activeWalkers was already incremented by Run() before calling processJob
	// to close the race window between Pop and here.
	defer w.hunt.activeWalkers.Add(-1)

	// Acquire global concurrency slot before doing any network I/O.
	// This ensures at most MaxConcurrency requests are in-flight at once.
	select {
	case w.hunt.sem <- struct{}{}:
		defer func() { <-w.hunt.sem }()
	case <-ctx.Done():
		return
	}

	// Apply a human-like inter-request delay using the rhythm state machine.
	// The delay is drawn from the active burst/pause phase. Using a select
	// with ctx.Done() ensures cancellation is honoured immediately even when
	// the delay is several seconds long (e.g. careful profile pause phases).
	if w.rhythm != nil {
		delay := w.rhythm.Next()
		if delay > 0 {
			w.logger.Debug("behavior: applying rhythm delay",
				"delay", delay, "state", w.rhythm.State())
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return
			}
		}
	}

	var (
		resp          *foxhound.Response
		fetchErr      error
		start         time.Time
		devKey        string
		devHit        bool
		activeFetcher = w.fetcher
	)

	w.logger.Debug("processing job",
		"url", job.URL, "domain", job.Domain, "depth", job.Depth,
		"fetch_mode", job.FetchMode, "priority", job.Priority)

	// Development mode: serve from disk if available, otherwise fetch and cache.
	if w.hunt.devModeCache != nil {
		devKey = devModeCacheKey(job.URL)
		if data, hit := w.hunt.devModeCache.Get(ctx, devKey); hit {
			var cached foxhound.Response
			if uerr := json.Unmarshal(data, &cached); uerr == nil {
				cached.Job = job
				resp = &cached
				devHit = true
				w.logger.Debug("dev-mode: cache hit", "url", job.URL, "key", devKey)
				w.hunt.stats.RecordRequest(job.Domain, 0, nil, false)
				w.hunt.stats.RecordBytes(int64(len(cached.Body)))
			} else {
				w.logger.Warn("dev-mode: cache entry corrupt, refetching", "url", job.URL, "err", uerr)
			}
		}
	}

	if devHit {
		goto afterFetch
	}

	// Per-job session routing: when job.SessionID names a registered session,
	// route through that session's fetcher; otherwise use the walker's default.
	if job.SessionID != "" {
		if f := w.hunt.sessionFetcherFor(job); f != nil {
			activeFetcher = f
		}
	}

	start = time.Now()
	for attempt := 0; ; attempt++ {
		resp, fetchErr = activeFetcher.Fetch(ctx, job)
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

		retryTimer := time.NewTimer(delay)
		select {
		case <-retryTimer.C:
		case <-ctx.Done():
			retryTimer.Stop()
			return
		}
	}

	if fetchErr != nil {
		w.logger.Error("fetch failed", "url", job.URL, "err", fetchErr)
		if w.hunt.config.OnError != nil {
			w.hunt.config.OnError(ctx, job, fetchErr)
		}
		return
	}

	// Development mode: store the freshly fetched response so the next run
	// for this URL replays from disk. Errors are logged but never abort the
	// fetch — the cache is purely an optimisation.
	if w.hunt.devModeCache != nil && resp != nil {
		if data, merr := json.Marshal(resp); merr == nil {
			if serr := w.hunt.devModeCache.Set(ctx, devKey, data, 0); serr != nil {
				w.logger.Warn("dev-mode: cache write failed", "url", job.URL, "err", serr)
			}
		}
	}

afterFetch:
	var fetchDuration time.Duration
	if !start.IsZero() {
		fetchDuration = time.Since(start)
	}

	w.logger.Debug("fetch complete",
		"url", job.URL, "status", resp.StatusCode, "bytes", len(resp.Body),
		"duration", fetchDuration, "fetch_mode", resp.FetchMode)

	// CAPTCHA detection — if the response contains a CAPTCHA challenge,
	// log it and count as blocked. Skip when the response came through the
	// browser fetcher: SmartFetcher already ran captcha.Detect() to decide
	// escalation, and the browser result should not be re-judged by the same
	// heuristic (avoids false positives on legitimate content and stats
	// double-counting).
	if resp.FetchMode != foxhound.FetchBrowser {
		if detection := captcha.Detect(resp); detection.Type != captcha.CaptchaNone {
			w.logger.Warn("CAPTCHA detected in response",
				"url", job.URL,
				"captcha_type", detection.Type,
				"site_key", detection.SiteKey,
			)
			w.hunt.stats.RecordBlock(job.Domain)
			return
		}
	}

	// Attach Hunt-scoped adaptive extractor (if any) so the user
	// processor can call resp.Adaptive / CSSAdaptive without manual wiring.
	if ae := w.hunt.adaptiveExtractor; ae != nil {
		resp.SetAdaptiveExtractor(ae)
		// Apply any deferred Trail-level adaptive registrations attached
		// to the originating job. Done here so the registration uses the
		// Hunt's shared extractor and learns its signature from the
		// freshly fetched body.
		w.applyTrailAdaptive(resp, job)
	}

	result, procErr := w.processor.Process(ctx, resp)
	if procErr != nil {
		w.logger.Error("process failed", "url", job.URL, "err", procErr)
		if w.hunt.config.OnError != nil {
			w.hunt.config.OnError(ctx, job, procErr)
		}
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
		// Invoke ItemCallback for streaming item processing.
		if w.hunt.config.ItemCallback != nil {
			w.hunt.config.ItemCallback(ctx, out)
		}
		// Invoke OnItem with job context.
		if w.hunt.config.OnItem != nil {
			w.hunt.config.OnItem(ctx, job, out)
		}
		w.writeItem(ctx, out)
		w.hunt.stats.RecordItems(1)
		w.hunt.streamItem(out)
		w.hunt.maybeCheckpoint()
	}

	// Enqueue newly discovered jobs (inject page actions if configured).
	for _, next := range result.Jobs {
		w.hunt.injectPageActions(next)
		if err := w.queue.Push(ctx, next); err != nil {
			if ctx.Err() != nil {
				return
			}
			w.logger.Error("enqueue failed", "url", next.URL, "err", err)
		}
	}

	processDuration := time.Since(jobStart)
	w.hunt.stats.RecordProcessDuration(job.Domain, processDuration)
	w.logger.Info("job complete",
		"url", job.URL,
		"status", resp.StatusCode,
		"fetch_duration", fetchDuration.Truncate(time.Millisecond),
		"process_duration", processDuration.Truncate(time.Millisecond),
		"items", len(result.Items),
	)
}

// trailAdaptiveMetaKey is the Job.Meta key used to carry deferred adaptive
// selector registrations from a Trail to the walker.
const trailAdaptiveMetaKey = "_foxhound_trail_adaptive"

// applyTrailAdaptive reads any Trail-level adaptive selector registrations
// from job.Meta and applies them against the Hunt's shared extractor using
// resp.CSSAdaptive (which both registers and learns the signature). The
// meta entries are produced by Trail.Adaptive at ToJobs time.
func (w *Walker) applyTrailAdaptive(resp *foxhound.Response, job *foxhound.Job) {
	if job == nil || job.Meta == nil {
		return
	}
	raw, ok := job.Meta[trailAdaptiveMetaKey]
	if !ok {
		return
	}
	regs, ok := raw.([][2]string)
	if !ok {
		return
	}
	for _, r := range regs {
		_ = resp.CSSAdaptive(r[1], r[0])
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
