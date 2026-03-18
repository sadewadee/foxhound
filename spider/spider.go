// Package spider provides a high-level scraping API built on top of the
// foxhound engine. It offers a struct-based approach with start URLs, callback
// routing, allowed domains filtering, and lifecycle hooks.
//
// A Spider is the primary abstraction for building scrapers. Users embed
// BaseSpider and override Parse methods:
//
//	type ProductSpider struct {
//	    spider.BaseSpider
//	}
//
//	func (s *ProductSpider) Parse(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
//	    // Extract product data from the page.
//	    item := foxhound.NewItem()
//	    item.Set("title", resp.CSS("h1.product-title").Text())
//	    item.Set("price", resp.CSS(".price").Text())
//	    return &foxhound.Result{
//	        Items: []*foxhound.Item{item},
//	        Jobs:  resp.Follow("a.next-page[href]"),
//	    }, nil
//	}
package spider

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/engine"

	// Import parse to register HTML selectors for resp.CSS()/resp.XPath().
	_ "github.com/sadewadee/foxhound/parse"
)

// Spider defines the interface that all spiders must implement.
// The BaseSpider provides a default implementation for all methods.
type Spider interface {
	// Name returns the spider's unique name.
	Name() string
	// StartURLs returns the seed URLs to begin crawling.
	StartURLs() []string
	// AllowedDomains returns the list of domains this spider is allowed to
	// crawl. An empty list means all domains are allowed.
	AllowedDomains() []string
	// Parse is the default callback for processing responses. It is invoked
	// when a job has no specific Callback set.
	Parse(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error)
}

// CallbackSpider extends Spider with named callback routing. When a Job has
// a Callback field set, the spider routes the response to that method instead
// of the default Parse.
type CallbackSpider interface {
	Spider
	// Callbacks returns a map of callback name -> handler function.
	// The spider routes responses to the appropriate handler based on
	// the Job.Callback field.
	Callbacks() map[string]func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error)
}

// BlockChecker is optionally implemented by spiders to detect blocked
// responses. When IsBlocked returns true, the runner retries the request
// (up to MaxBlockedRetries times).
type BlockChecker interface {
	// IsBlocked returns true if the response indicates the scraper is blocked.
	IsBlocked(resp *foxhound.Response) bool
}

// BlockRetrier is optionally implemented by spiders to customize how blocked
// requests are retried. The runner calls RetryBlockedRequest before
// re-enqueuing a blocked job.
type BlockRetrier interface {
	// RetryBlockedRequest modifies the job before retrying a blocked request.
	// Implementations may change the fetch mode, add headers, switch proxy, etc.
	RetryBlockedRequest(job *foxhound.Job, resp *foxhound.Response) *foxhound.Job
}

// blockedStatusCodes are HTTP status codes that indicate a blocked response.
var blockedStatusCodes = map[int]bool{
	401: true, // Unauthorized
	403: true, // Forbidden
	407: true, // Proxy Authentication Required
	429: true, // Too Many Requests
	444: true, // Connection Closed Without Response
	500: true, // Internal Server Error
	502: true, // Bad Gateway
	503: true, // Service Unavailable
	504: true, // Gateway Timeout
}

// BaseSpider provides sensible defaults for the Spider interface. Embed it in
// your spider struct and override the methods you need:
//
//	type MySpider struct {
//	    spider.BaseSpider
//	}
type BaseSpider struct {
	// SpiderName is the spider's unique identifier.
	SpiderName string
	// StartingURLs are the seed URLs to begin crawling.
	StartingURLs []string
	// Domains restricts crawling to these domains. Empty means all.
	Domains []string
	// FetchMode sets the default fetch mode for seed jobs.
	FetchMode foxhound.FetchMode
	// MaxBlockedRetries is the max retries for blocked requests. Default: 3.
	MaxBlockedRetries int
	// CustomSettings overrides engine defaults for this spider.
	CustomSettings *Settings
}

// IsBlocked implements the default block detection. It returns true for
// common anti-bot HTTP status codes (401, 403, 407, 429, 444, 500-504).
// Override this method in your spider for custom detection logic.
func (s *BaseSpider) IsBlocked(resp *foxhound.Response) bool {
	return blockedStatusCodes[resp.StatusCode]
}

// Settings holds per-spider configuration overrides.
type Settings struct {
	// Walkers is the number of concurrent virtual-user goroutines.
	Walkers int
	// DownloadDelay is the base delay between requests per domain.
	DownloadDelay time.Duration
	// ConcurrentRequestsPerDomain limits parallel requests to the same domain.
	ConcurrentRequestsPerDomain int
	// MaxDepth limits how deep the spider crawls from seed URLs.
	MaxDepth int
	// MaxConcurrency is the global cap on simultaneous in-flight requests.
	MaxConcurrency int
	// AutoThrottle enables adaptive per-domain delay.
	AutoThrottle bool
	// BehaviorProfile selects human-simulation timing: "careful", "moderate", "aggressive".
	BehaviorProfile string
	// PageActions are JS snippets executed after each page load in browser mode.
	PageActions []string
	// RespectRobotsTxt enables robots.txt compliance.
	RespectRobotsTxt bool
	// FingerprintIncludeHeaders includes request headers in dedup fingerprint.
	FingerprintIncludeHeaders bool
	// FingerprintKeepFragments preserves URL fragments in dedup fingerprint.
	FingerprintKeepFragments bool
	// FingerprintFunc is a custom fingerprint function for dedup. When set,
	// it replaces the default URL-based fingerprinting.
	FingerprintFunc func(job *foxhound.Job) string
}

// Name returns the spider name.
func (s *BaseSpider) Name() string {
	if s.SpiderName == "" {
		return "unnamed-spider"
	}
	return s.SpiderName
}

// StartURLs returns the configured start URLs.
func (s *BaseSpider) StartURLs() []string {
	return s.StartingURLs
}

// AllowedDomains returns the domain whitelist.
func (s *BaseSpider) AllowedDomains() []string {
	return s.Domains
}

// Parse is the default response handler. Override this in your spider.
func (s *BaseSpider) Parse(_ context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
	// Default: extract title, yield no follow-up jobs.
	item := foxhound.NewItem()
	item.Set("url", resp.URL)
	item.Set("status", resp.StatusCode)
	item.URL = resp.URL
	return &foxhound.Result{Items: []*foxhound.Item{item}}, nil
}

// ---------------------------------------------------------------------------
// Runner — executes a Spider using the engine
// ---------------------------------------------------------------------------

// Runner configures and executes a Spider using the foxhound engine.
type Runner struct {
	spider    Spider
	fetcher   foxhound.Fetcher
	queue     foxhound.Queue
	pipelines []foxhound.Pipeline
	writers   []foxhound.Writer
	mws       []foxhound.Middleware
	logger    *slog.Logger

	// Hooks
	onStart func(ctx context.Context)
	onClose func(ctx context.Context, stats *engine.Stats)
	onError func(ctx context.Context, job *foxhound.Job, err error)
	onItem  func(ctx context.Context, job *foxhound.Job, item *foxhound.Item)

	// Internal
	hunt *engine.Hunt
	mu   sync.Mutex
}

// RunnerOption configures a Runner.
type RunnerOption func(*Runner)

// WithFetcher sets the fetcher for the runner.
func WithFetcher(f foxhound.Fetcher) RunnerOption {
	return func(r *Runner) { r.fetcher = f }
}

// WithQueue sets the queue backend.
func WithQueue(q foxhound.Queue) RunnerOption {
	return func(r *Runner) { r.queue = q }
}

// WithPipelines sets the pipeline stages.
func WithPipelines(ps ...foxhound.Pipeline) RunnerOption {
	return func(r *Runner) { r.pipelines = ps }
}

// WithWriters sets the export writers.
func WithWriters(ws ...foxhound.Writer) RunnerOption {
	return func(r *Runner) { r.writers = ws }
}

// WithMiddlewares sets the middleware chain.
func WithMiddlewares(mws ...foxhound.Middleware) RunnerOption {
	return func(r *Runner) { r.mws = mws }
}

// WithOnStart sets the OnStart lifecycle hook.
func WithOnStart(fn func(ctx context.Context)) RunnerOption {
	return func(r *Runner) { r.onStart = fn }
}

// WithOnClose sets the OnClose lifecycle hook.
func WithOnClose(fn func(ctx context.Context, stats *engine.Stats)) RunnerOption {
	return func(r *Runner) { r.onClose = fn }
}

// WithOnError sets the OnError lifecycle hook.
func WithOnError(fn func(ctx context.Context, job *foxhound.Job, err error)) RunnerOption {
	return func(r *Runner) { r.onError = fn }
}

// WithOnItem sets the OnItem lifecycle hook.
func WithOnItem(fn func(ctx context.Context, job *foxhound.Job, item *foxhound.Item)) RunnerOption {
	return func(r *Runner) { r.onItem = fn }
}

// NewRunner creates a Runner for the given Spider with optional configuration.
func NewRunner(s Spider, opts ...RunnerOption) *Runner {
	r := &Runner{
		spider: s,
		logger: slog.With("component", "spider-runner", "spider", s.Name()),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Run executes the spider and blocks until all jobs are processed.
func (r *Runner) Run(ctx context.Context) error {
	if r.fetcher == nil {
		return fmt.Errorf("spider: fetcher is required (use WithFetcher)")
	}
	if r.queue == nil {
		return fmt.Errorf("spider: queue is required (use WithQueue)")
	}

	// Build the processor that routes callbacks.
	processor := r.buildProcessor()

	// Build seed jobs from start URLs.
	seeds := r.buildSeeds()
	if len(seeds) == 0 {
		return fmt.Errorf("spider %q: no start URLs configured", r.spider.Name())
	}

	// Determine domain from start URLs for the hunt.
	domain := ""
	if u, err := url.Parse(seeds[0].URL); err == nil {
		domain = u.Host
	}

	// Build hunt config.
	walkers := 3
	maxConc := 0
	profile := "moderate"
	var pageActions []string

	if bs, ok := r.spider.(*BaseSpider); ok && bs.CustomSettings != nil {
		s := bs.CustomSettings
		if s.Walkers > 0 {
			walkers = s.Walkers
		}
		if s.MaxConcurrency > 0 {
			maxConc = s.MaxConcurrency
		}
		if s.BehaviorProfile != "" {
			profile = s.BehaviorProfile
		}
		pageActions = s.PageActions
	}

	cfg := engine.HuntConfig{
		Name:            r.spider.Name(),
		Domain:          domain,
		Walkers:         walkers,
		MaxConcurrency:  maxConc,
		Seeds:           seeds,
		Processor:       processor,
		Fetcher:         r.fetcher,
		Queue:           r.queue,
		Pipelines:       r.pipelines,
		Writers:         r.writers,
		Middlewares:     r.mws,
		BehaviorProfile: profile,
		PageActions:     pageActions,
		OnStart:         r.onStart,
		OnClose:         r.onClose,
		OnError:         r.onError,
		OnItem:          r.onItem,
	}

	h := engine.NewHunt(cfg)

	r.mu.Lock()
	r.hunt = h
	r.mu.Unlock()

	return h.Run(ctx)
}

// Stream starts the spider and returns a channel of items.
func (r *Runner) Stream(ctx context.Context) (<-chan *foxhound.Item, error) {
	if r.fetcher == nil {
		return nil, fmt.Errorf("spider: fetcher is required")
	}
	if r.queue == nil {
		return nil, fmt.Errorf("spider: queue is required")
	}

	processor := r.buildProcessor()
	seeds := r.buildSeeds()
	if len(seeds) == 0 {
		return nil, fmt.Errorf("spider %q: no start URLs", r.spider.Name())
	}

	domain := ""
	if u, err := url.Parse(seeds[0].URL); err == nil {
		domain = u.Host
	}

	cfg := engine.HuntConfig{
		Name:            r.spider.Name(),
		Domain:          domain,
		Walkers:         3,
		Seeds:           seeds,
		Processor:       processor,
		Fetcher:         r.fetcher,
		Queue:           r.queue,
		Pipelines:       r.pipelines,
		Writers:         r.writers,
		Middlewares:     r.mws,
		BehaviorProfile: "moderate",
		OnStart:         r.onStart,
		OnClose:         r.onClose,
		OnError:         r.onError,
		OnItem:          r.onItem,
	}

	h := engine.NewHunt(cfg)

	r.mu.Lock()
	r.hunt = h
	r.mu.Unlock()

	return h.Stream(ctx)
}

// Pause pauses the spider's hunt.
func (r *Runner) Pause() {
	r.mu.Lock()
	h := r.hunt
	r.mu.Unlock()
	if h != nil {
		h.Pause()
	}
}

// Resume resumes the spider's hunt.
func (r *Runner) Resume() {
	r.mu.Lock()
	h := r.hunt
	r.mu.Unlock()
	if h != nil {
		h.Resume()
	}
}

// Stop stops the spider's hunt.
func (r *Runner) Stop() {
	r.mu.Lock()
	h := r.hunt
	r.mu.Unlock()
	if h != nil {
		h.Stop()
	}
}

// Stats returns the spider's hunt stats, or nil if the hunt hasn't started.
func (r *Runner) Stats() *engine.Stats {
	r.mu.Lock()
	h := r.hunt
	r.mu.Unlock()
	if h != nil {
		return h.Stats()
	}
	return nil
}

// buildProcessor creates a Processor that routes responses to the spider's
// callbacks based on the Job.Callback field, enforces AllowedDomains, and
// delegates to the spider's Parse method.
func (r *Runner) buildProcessor() foxhound.Processor {
	allowed := r.spider.AllowedDomains()
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, d := range allowed {
		allowedSet[strings.ToLower(d)] = struct{}{}
	}

	var callbacks map[string]func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error)
	if cs, ok := r.spider.(CallbackSpider); ok {
		callbacks = cs.Callbacks()
	}

	return foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
		// Domain filtering: if AllowedDomains is set, drop responses from
		// domains not in the whitelist.
		if len(allowedSet) > 0 {
			if u, err := url.Parse(resp.URL); err == nil {
				host := strings.ToLower(u.Hostname())
				if _, ok := allowedSet[host]; !ok {
					r.logger.Debug("spider: skipping response from disallowed domain",
						"url", resp.URL, "host", host)
					return &foxhound.Result{}, nil
				}
			}
		}

		// Route to named callback if the job has one.
		if resp.Job != nil && resp.Job.Callback != "" && callbacks != nil {
			if fn, ok := callbacks[resp.Job.Callback]; ok {
				result, err := fn(ctx, resp)
				if err != nil {
					return nil, err
				}
				return r.filterJobs(result, allowedSet), nil
			}
			r.logger.Warn("spider: unknown callback, falling back to Parse",
				"callback", resp.Job.Callback, "url", resp.URL)
		}

		// Default: call spider's Parse method.
		result, err := r.spider.Parse(ctx, resp)
		if err != nil {
			return nil, err
		}
		return r.filterJobs(result, allowedSet), nil
	})
}

// filterJobs removes discovered jobs that target disallowed domains.
func (r *Runner) filterJobs(result *foxhound.Result, allowedSet map[string]struct{}) *foxhound.Result {
	if result == nil || len(allowedSet) == 0 || len(result.Jobs) == 0 {
		return result
	}

	filtered := make([]*foxhound.Job, 0, len(result.Jobs))
	for _, job := range result.Jobs {
		if u, err := url.Parse(job.URL); err == nil {
			host := strings.ToLower(u.Hostname())
			if _, ok := allowedSet[host]; !ok {
				r.logger.Debug("spider: filtering job for disallowed domain",
					"url", job.URL, "host", host)
				continue
			}
		}
		filtered = append(filtered, job)
	}
	result.Jobs = filtered
	return result
}

// buildSeeds creates seed jobs from the spider's start URLs.
func (r *Runner) buildSeeds() []*foxhound.Job {
	urls := r.spider.StartURLs()
	seeds := make([]*foxhound.Job, 0, len(urls))

	fetchMode := foxhound.FetchAuto
	if bs, ok := r.spider.(*BaseSpider); ok {
		fetchMode = bs.FetchMode
	}

	for _, rawURL := range urls {
		if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
			rawURL = "https://" + rawURL
		}

		domain := ""
		if u, err := url.Parse(rawURL); err == nil {
			domain = u.Host
		}

		seeds = append(seeds, &foxhound.Job{
			ID:        rawURL,
			URL:       rawURL,
			Method:    "GET",
			FetchMode: fetchMode,
			Priority:  foxhound.PriorityHigh,
			Depth:     0,
			Domain:    domain,
			CreatedAt: time.Now(),
		})
	}
	return seeds
}
