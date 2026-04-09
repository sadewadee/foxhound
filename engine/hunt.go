package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/cache"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/parse"
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
	// MaxConcurrency is the global cap on simultaneous in-flight requests.
	// When 0, defaults to Walkers count.
	MaxConcurrency int
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
	// BehaviorProfile selects the human-simulation preset applied by each
	// Walker: "careful", "moderate", or "aggressive". Defaults to "moderate"
	// when empty so walkers always apply timing and rhythm delays.
	BehaviorProfile string
	// Checkpoint controls automatic state saving. Optional — checkpointing is
	// inactive when Checkpoint.Enabled is false (the zero value).
	Checkpoint CheckpointConfig
	// ItemCallback is invoked for every item that survives the pipeline chain,
	// before it is written. This enables streaming item processing during the
	// crawl without needing to use Stream(). The callback runs synchronously
	// in the walker goroutine so it must be fast.
	ItemCallback func(ctx context.Context, item *foxhound.Item)
	// OnStart is called once when the hunt begins (after seeds are queued).
	OnStart func(ctx context.Context)
	// OnClose is called once when the hunt completes (after writers flush).
	OnClose func(ctx context.Context, stats *Stats)
	// OnError is called when a fetch or process error occurs. Errors are
	// still logged; this hook enables custom error handling.
	OnError func(ctx context.Context, job *foxhound.Job, err error)
	// OnItem is called for each item after pipeline processing. Unlike
	// ItemCallback, OnItem receives the originating Job for context.
	OnItem func(ctx context.Context, job *foxhound.Job, item *foxhound.Item)
	// PageActions are JavaScript snippets executed after page load when using
	// the browser fetcher. They are injected as JobSteps of type
	// JobStepEvaluate on every job that uses browser mode.
	PageActions []string
	// Pool is an optional URL pool from a collect phase. When set, all URLs
	// in the pool are drained and added as seed jobs before walkers start.
	// This enables the two-phase pattern: collect URLs first, process concurrently.
	Pool Pool
	// PoolFetchMode sets the FetchMode for jobs created from pool URLs.
	// Defaults to FetchBrowser when PoolFetchModeSet is false.
	PoolFetchMode foxhound.FetchMode
	// PoolFetchModeSet indicates the user explicitly set PoolFetchMode.
	// When false and pool URLs exist, the mode defaults to FetchBrowser.
	PoolFetchModeSet bool
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
	sem           chan struct{} // global concurrency limiter
	// adaptiveExtractor is set by WithAdaptive and threaded into every
	// Response handed to the user processor so that Response.Adaptive(...)
	// and Response.CSSAdaptive(...) work without manual wiring.
	adaptiveExtractor *parse.AdaptiveExtractor
	// itemCh is non-nil only when Stream or StreamWithStats has been called.
	// Walkers send each processed item here; Run closes it when done.
	itemCh chan *foxhound.Item
	// blockedDomains and disabledResources are populated by WithBlockedDomains
	// and WithDisableResources. They are pushed to the fetcher's intercept
	// config at Run time when the fetcher is a *fetch.CamoufoxFetcher.
	blockedDomains    []string
	disabledResources []string
	// devModeCache is set by WithDevelopmentMode. When non-nil, Walker checks
	// it before every fetch and serves cached responses on hit; on miss it
	// fetches normally and stores the response.
	devModeCache *cache.FileCache
	// sessions holds named session bundles registered via AddSession. Walker
	// looks them up by Job.SessionID at fetch time.
	sessions map[string]*foxhound.Session
}

// NewHunt creates a Hunt from cfg. It does not start any goroutines; call
// Run to begin processing.
func NewHunt(cfg HuntConfig) *Hunt {
	if cfg.Walkers < 1 {
		cfg.Walkers = 1
	}
	maxConc := cfg.Walkers
	if cfg.MaxConcurrency > 0 {
		maxConc = cfg.MaxConcurrency
	}
	return &Hunt{
		config: cfg,
		state:  HuntIdle,
		stats:  NewStats(),
		logger: slog.With("component", "hunt", "hunt", cfg.Name),
		sem:    make(chan struct{}, maxConc),
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

	// Push hunt-level intercept config (blocked domains, disabled resources)
	// into the fetcher before middleware wrapping so the route handler installs
	// during context creation.
	h.applyHuntInterceptToFetcher()

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

	// Push seed jobs. If PageActions are configured, inject them as Evaluate
	// steps on every seed job that uses browser mode.
	for _, job := range h.config.Seeds {
		h.injectPageActions(job)
		h.logger.Debug("seeding job", "url", job.URL, "fetch_mode", job.FetchMode)
		if err := h.config.Queue.Push(runCtx, job); err != nil {
			h.setState(HuntFailed)
			return fmt.Errorf("seeding queue: %w", err)
		}
	}

	// Drain pool URLs into queue as seed jobs.
	if h.config.Pool != nil {
		poolURLs, err := h.config.Pool.Drain(runCtx)
		if err != nil {
			slog.Error("hunt: draining pool", "error", err)
		}
		fetchMode := h.config.PoolFetchMode
		if !h.config.PoolFetchModeSet && len(poolURLs) > 0 {
			fetchMode = foxhound.FetchBrowser
		}
		for i, u := range poolURLs {
			job := &foxhound.Job{
				ID:        fmt.Sprintf("pool-%d", i),
				URL:       u,
				Method:    "GET",
				FetchMode: fetchMode,
				Priority:  foxhound.PriorityNormal,
				CreatedAt: time.Now(),
				Meta:      map[string]any{"source": "pool"},
			}
			h.config.Queue.Push(runCtx, job)
		}
		slog.Info("hunt: pool drained", "urls", len(poolURLs))
	}

	// Call OnStart hook.
	if h.config.OnStart != nil {
		h.config.OnStart(runCtx)
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

	// Close the streaming channel now that all walkers have stopped producing
	// items. Must happen before writer flush so StreamWithStats sees the final
	// stats snapshot after all items have been forwarded.
	h.mu.Lock()
	if h.itemCh != nil {
		close(h.itemCh)
		h.itemCh = nil
	}
	h.mu.Unlock()

	// Flush writers.
	for _, w := range h.config.Writers {
		if err := w.Flush(ctx); err != nil {
			h.logger.Warn("writer flush failed", "err", err)
		}
	}

	// Call OnClose hook.
	if h.config.OnClose != nil {
		h.config.OnClose(ctx, h.stats)
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
	ticker := time.NewTicker(drainPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if h.config.Queue.Len() == 0 && h.activeWalkers.Load() == 0 {
			// Both conditions met. Apply a short settling delay to handle the
			// tiny window between Pop returning and activeWalkers being
			// incremented by the walker goroutine.
			timer := time.NewTimer(drainSettleDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
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
		case <-ticker.C:
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

// WithAdaptive enables adaptive selector mode for the Hunt. The savePath
// argument is the file where learned element signatures are persisted as
// JSON across runs; pass an empty string for in-memory only (signatures
// are lost between runs).
//
// Once enabled, the walker attaches a shared *parse.AdaptiveExtractor to
// every Response, so user code can call resp.Adaptive("name"),
// resp.CSSAdaptive(selector, name), or resp.CSSAdaptiveAll(selector, name)
// without manually constructing an extractor.
//
// WithAdaptive returns the Hunt for fluent chaining and is safe to call
// before Run.
func (h *Hunt) WithAdaptive(savePath string) *Hunt {
	h.mu.Lock()
	h.adaptiveExtractor = parse.NewAdaptiveExtractor(savePath)
	h.mu.Unlock()
	return h
}

// AdaptiveExtractor returns the Hunt-scoped adaptive extractor configured
// via WithAdaptive, or nil when adaptive mode has not been enabled.
func (h *Hunt) AdaptiveExtractor() *parse.AdaptiveExtractor {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.adaptiveExtractor
}

// WithBlockedDomains registers fully-qualified domain names whose requests
// must be aborted by the browser fetcher. Subdomains are also matched, so
// "example.com" blocks "tracker.example.com". Only effective when the Hunt's
// Fetcher is a *fetch.CamoufoxFetcher created and managed by the application;
// otherwise the call is silently ignored at Run time with a warning log.
func (h *Hunt) WithBlockedDomains(domains ...string) *Hunt {
	h.mu.Lock()
	h.blockedDomains = append(h.blockedDomains, domains...)
	h.mu.Unlock()
	return h
}

// WithDisableResources registers browser resource types to abort. Valid
// values: "image", "font", "media", "stylesheet", "object", "imageset",
// "texttrack", "websocket", "csp_report", "beacon". Unknown values are
// dropped at apply time. Only effective when the Hunt's Fetcher is a
// *fetch.CamoufoxFetcher; otherwise the call is silently ignored at Run
// time with a warning log.
func (h *Hunt) WithDisableResources(types ...string) *Hunt {
	h.mu.Lock()
	h.disabledResources = append(h.disabledResources, types...)
	h.mu.Unlock()
	return h
}

// WithDevelopmentMode enables on-disk response replay. The first time a job
// for a given URL is fetched the real fetcher is invoked and the response is
// serialised to dir/<sha256(url)>.json. Subsequent fetches for the same URL
// hit the cache and skip the network entirely, which is the standard fast
// inner-loop pattern when iterating on a parser.
//
// Pass an empty dir to disable. Errors creating the cache directory are
// returned at Run time, not here.
func (h *Hunt) WithDevelopmentMode(cacheDir string) *Hunt {
	if cacheDir == "" {
		return h
	}
	fc, err := cache.NewFile(cacheDir, 0)
	if err != nil {
		slog.Warn("hunt: WithDevelopmentMode failed to open cache dir, replay disabled",
			"dir", cacheDir, "err", err)
		return h
	}
	h.mu.Lock()
	h.devModeCache = fc
	h.mu.Unlock()
	return h
}

// applyHuntInterceptToFetcher pushes any WithBlockedDomains / WithDisableResources
// configuration into the underlying CamoufoxFetcher's intercept config. Called
// once at Run time before walkers start. When the fetcher is not a Camoufox
// fetcher the call is a no-op (with a warning when blocking was requested).
func (h *Hunt) applyHuntInterceptToFetcher() {
	if len(h.blockedDomains) == 0 && len(h.disabledResources) == 0 {
		return
	}
	cf, ok := h.config.Fetcher.(*fetch.CamoufoxFetcher)
	if !ok {
		slog.Warn("hunt: WithBlockedDomains/WithDisableResources requires a CamoufoxFetcher; ignored",
			"blocked_domains", len(h.blockedDomains),
			"disabled_resources", len(h.disabledResources))
		return
	}

	resourceMap := make(map[fetch.ResourceType]bool)
	for _, t := range h.disabledResources {
		resourceMap[fetch.ResourceType(t)] = true
	}
	domainMap := make(map[string]bool)
	for _, d := range h.blockedDomains {
		domainMap[d] = true
	}
	ic := fetch.NewInterceptConfig(resourceMap, domainMap)
	fetch.WithInterceptConfig(ic)(cf)
	slog.Info("hunt: applied intercept config",
		"blocked_domains", len(h.blockedDomains),
		"disabled_resources", len(h.disabledResources))
}

// devModeCacheKey returns the URL-derived key used by WithDevelopmentMode to
// look up cached responses. Walker also uses this constant.
func devModeCacheKey(url string) string {
	h := sha256.Sum256([]byte(url))
	return hex.EncodeToString(h[:])
}

// SessionConfig describes a named session bundle that a Hunt can route jobs
// to via Job.SessionID. Each session has its own fetcher, identity, and
// proxy URL — useful when one campaign needs to mix fast-static fetches for
// index pages with slow stealth fetches for detail pages, with separate
// cookie jars per role.
type SessionConfig struct {
	// Name is the unique identifier for this session within a Hunt.
	// Must match Job.SessionID for routing to work.
	Name string
	// Fetcher is the underlying fetcher for the session. Required.
	Fetcher foxhound.Fetcher
	// Identity is the optional identity profile attached to the session.
	// Stored as `any` to avoid an import cycle with the identity package.
	Identity any
	// Proxy is the optional proxy URL recorded with the session for
	// inspection. Wire it through the fetcher's own option at construction.
	Proxy string
}

// AddSession registers a named session bundle. When a Job's SessionID equals
// name, the walker uses cfg.Fetcher instead of the hunt's default fetcher.
// Subsequent calls with the same name overwrite the previous registration.
//
// AddSession is safe to call before Run; calling it after Run starts is also
// supported but the in-flight job currently being processed by a walker will
// not pick up the new registration until its next Pop.
func (h *Hunt) AddSession(name string, cfg SessionConfig) *Hunt {
	if name == "" || cfg.Fetcher == nil {
		return h
	}
	cfg.Name = name
	sess := foxhound.NewSession(
		foxhound.WithSessionFetcher(cfg.Fetcher),
		foxhound.WithSessionIdentity(cfg.Identity),
		foxhound.WithSessionProxy(cfg.Proxy),
	)
	sess.SetName(name)

	h.mu.Lock()
	if h.sessions == nil {
		h.sessions = make(map[string]*foxhound.Session)
	}
	h.sessions[name] = sess
	h.mu.Unlock()
	return h
}

// Session returns the named session previously registered via AddSession,
// or nil when no session with that name exists.
func (h *Hunt) Session(name string) *foxhound.Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.sessions == nil {
		return nil
	}
	return h.sessions[name]
}

// sessionFetcherFor returns the fetcher to use for the given job: when the
// job names a registered session via Job.SessionID, that session's fetcher
// is returned; otherwise the hunt's default middleware-wrapped fetcher is
// returned.
func (h *Hunt) sessionFetcherFor(job *foxhound.Job) foxhound.Fetcher {
	if job == nil || job.SessionID == "" {
		return h.fetcher
	}
	h.mu.RLock()
	sess := h.sessions[job.SessionID]
	h.mu.RUnlock()
	if sess == nil {
		return h.fetcher
	}
	if f := sess.Fetcher(); f != nil {
		return f
	}
	return h.fetcher
}

// SetLogger replaces the Hunt's logger. Walkers created after this call will
// inherit the new logger. Intended for testing.
func (h *Hunt) SetLogger(logger *slog.Logger) {
	h.mu.Lock()
	h.logger = logger
	h.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Streaming API
// ---------------------------------------------------------------------------

// StreamEvent is emitted on the channel returned by StreamWithStats.
// Exactly one of Item or Stats is non-nil per event.
type StreamEvent struct {
	// Item is non-nil for item events.
	Item *foxhound.Item
	// Stats is non-nil for periodic stats snapshot events.
	Stats *Stats
}

// Stream starts the hunt in a background goroutine and returns a channel that
// receives each item as it is produced. The channel is closed when the hunt
// completes or the context is cancelled, making it safe to use in a range loop:
//
//	for item := range hunt.Stream(ctx) { ... }
//
// Stream returns an error only when the hunt configuration is invalid.
// The channel is buffered (100 items) so that slow consumers do not block
// walkers; items are dropped with a warning log when the buffer is full.
func (h *Hunt) Stream(ctx context.Context) (<-chan *foxhound.Item, error) {
	if err := h.validateConfig(); err != nil {
		return nil, fmt.Errorf("invalid hunt config: %w", err)
	}

	ch := make(chan *foxhound.Item, 100)

	h.mu.Lock()
	h.itemCh = ch
	h.mu.Unlock()

	// Run closes h.itemCh (which aliases ch) when all walkers have stopped.
	// Do NOT close ch here — Run is the sole closer to avoid double-close.
	go func() {
		if err := h.Run(ctx); err != nil {
			h.logger.Error("stream hunt error", "err", err)
		}
	}()

	return ch, nil
}

// StreamWithStats starts the hunt and returns a channel of StreamEvent values.
// Item events arrive as items are scraped; Stats events are emitted every
// statsInterval. The channel is closed when the hunt finishes.
//
// A statsInterval of 0 disables periodic stats events (only item events are
// sent). Use Stream instead when stats events are not needed.
func (h *Hunt) StreamWithStats(ctx context.Context, statsInterval time.Duration) (<-chan StreamEvent, error) {
	if err := h.validateConfig(); err != nil {
		return nil, fmt.Errorf("invalid hunt config: %w", err)
	}

	itemCh := make(chan *foxhound.Item, 100)
	eventCh := make(chan StreamEvent, 128)

	h.mu.Lock()
	h.itemCh = itemCh
	h.mu.Unlock()

	go func() {
		defer close(eventCh)

		// Start periodic stats ticker (optional).
		var ticker *time.Ticker
		var tickC <-chan time.Time
		if statsInterval > 0 {
			ticker = time.NewTicker(statsInterval)
			tickC = ticker.C
			defer ticker.Stop()
		}

		// Fan-out: forward item events and inject stats events.
		// itemCh is closed by Run when the hunt completes.
		for {
			select {
			case item, ok := <-itemCh:
				if !ok {
					// Hunt finished; emit one final stats snapshot.
					select {
					case eventCh <- StreamEvent{Stats: h.stats}:
					default:
					}
					return
				}
				select {
				case eventCh <- StreamEvent{Item: item}:
				case <-ctx.Done():
					return
				}

			case <-tickC:
				select {
				case eventCh <- StreamEvent{Stats: h.stats}:
				default:
					h.logger.Warn("stream: stats event channel full, dropping stats event")
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		if err := h.Run(ctx); err != nil {
			h.logger.Error("stream hunt error", "err", err)
		}
		// itemCh is closed inside Run (after wg.Wait). The fan-out goroutine
		// above will detect the close and emit the final stats event.
	}()

	return eventCh, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// streamItem sends item to the streaming channel when streaming is active.
// It uses a non-blocking send so that a slow consumer never blocks walkers.
// A warning is logged when the channel buffer is full and the item is dropped.
func (h *Hunt) streamItem(item *foxhound.Item) {
	h.mu.RLock()
	ch := h.itemCh
	h.mu.RUnlock()
	if ch == nil {
		return
	}
	select {
	case ch <- item:
	default:
		h.logger.Warn("stream: item channel full, dropping item")
	}
}

// injectPageActions appends configured JavaScript page actions as Evaluate
// steps on jobs that will use the browser fetcher.
func (h *Hunt) injectPageActions(job *foxhound.Job) {
	if len(h.config.PageActions) == 0 {
		return
	}
	// Only inject on browser or auto-mode jobs.
	if job.FetchMode != foxhound.FetchBrowser && job.FetchMode != foxhound.FetchAuto {
		return
	}
	for _, script := range h.config.PageActions {
		job.Steps = append(job.Steps, foxhound.JobStep{
			Action: foxhound.JobStepEvaluate,
			Script: script,
		})
	}
	// If the job was auto mode and now has browser steps, upgrade to browser.
	if job.FetchMode == foxhound.FetchAuto && len(job.Steps) > 0 {
		job.FetchMode = foxhound.FetchBrowser
	}
}

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
