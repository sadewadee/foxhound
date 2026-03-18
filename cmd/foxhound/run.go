package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"encoding/json"

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/cache"
	"github.com/sadewadee/foxhound/captcha"
	"github.com/sadewadee/foxhound/engine"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
	"github.com/sadewadee/foxhound/middleware"
	"github.com/sadewadee/foxhound/monitor"
	"github.com/sadewadee/foxhound/parse"
	"github.com/sadewadee/foxhound/pipeline"
	"github.com/sadewadee/foxhound/pipeline/export"
	"github.com/sadewadee/foxhound/proxy"
	proxyproviders "github.com/sadewadee/foxhound/proxy/providers"
	"github.com/sadewadee/foxhound/queue"
	"github.com/google/uuid"
)

// defaultProcessor extracts the page title and all same-domain links.
// This is used when running from the CLI without user-provided Go code.
// Users building with the library provide their own Processor implementation.
var defaultProcessor = foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
	doc, err := parse.NewDocument(resp)
	if err != nil {
		// Return an empty result rather than failing the whole crawl.
		return &foxhound.Result{}, nil
	}

	item := foxhound.NewItem()
	item.Set("url", resp.URL)
	item.Set("title", strings.TrimSpace(doc.Text("title")))
	item.Set("status", resp.StatusCode)
	item.URL = resp.URL

	// Parse the base URL so relative hrefs can be resolved.
	base, err := url.Parse(resp.URL)
	if err != nil {
		return &foxhound.Result{Items: []*foxhound.Item{item}}, nil
	}

	// Determine the parent depth (0 when job is nil).
	parentDepth := 0
	if resp.Job != nil {
		parentDepth = resp.Job.Depth
	}

	var jobs []*foxhound.Job
	seen := map[string]struct{}{}

	doc.Each("a[href]", func(_ int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists || href == "" || strings.HasPrefix(href, "#") {
			return
		}

		ref, err := url.Parse(href)
		if err != nil {
			return
		}

		resolved := base.ResolveReference(ref)
		// Strip fragment — dedup canonicalises query params later.
		resolved.Fragment = ""
		link := resolved.String()

		// Only follow links on the same host.
		if resolved.Host != base.Host {
			return
		}
		// Skip non-HTTP(S) schemes (mailto:, javascript:, etc.).
		scheme := strings.ToLower(resolved.Scheme)
		if scheme != "http" && scheme != "https" {
			return
		}

		if _, dup := seen[link]; dup {
			return
		}
		seen[link] = struct{}{}

		jobs = append(jobs, &foxhound.Job{
			ID:        uuid.NewString(),
			URL:       link,
			Method:    "GET",
			FetchMode: foxhound.FetchAuto,
			Priority:  foxhound.PriorityNormal,
			Depth:     parentDepth + 1,
			Domain:    resolved.Host,
			CreatedAt: time.Now(),
		})
	})

	return &foxhound.Result{
		Items: []*foxhound.Item{item},
		Jobs:  jobs,
	}, nil
})

// buildQueue creates a foxhound.Queue from a backend name and an optional
// connection URL / file path. For "redis" the URL must be a full redis:// URL.
// For "sqlite" the URL may be a file path or a sqlite:// URL. "memory" ignores
// the URL argument.
func buildQueue(backend, queueURL string) (foxhound.Queue, error) {
	switch strings.ToLower(backend) {
	case "memory", "":
		return queue.NewMemoryQueue(), nil

	case "redis":
		if queueURL == "" {
			queueURL = "redis://localhost:6379/0"
		}
		addr, password, db, err := parseRedisURL(queueURL)
		if err != nil {
			return nil, fmt.Errorf("parsing redis URL %q: %w", queueURL, err)
		}
		return queue.NewRedis(addr, password, db, "foxhound:jobs")

	case "sqlite":
		dbPath := queueURL
		if strings.HasPrefix(queueURL, "sqlite://") {
			dbPath = strings.TrimPrefix(queueURL, "sqlite://")
		}
		if dbPath == "" {
			dbPath = "foxhound.db"
		}
		return queue.NewSQLite(dbPath)

	default:
		return nil, fmt.Errorf("unknown queue backend %q (supported: memory, redis, sqlite)", backend)
	}
}

// buildMiddlewares assembles the FULL middleware chain from config.
// Order (outermost → innermost):
//  0. Concurrency  — limits parallel requests per domain (optional, outermost)
//  1. Metrics      — records all requests including retries
//  2. RateLimit    — enforces per-domain request rate
//  3. RobotsTxt    — respects robots.txt (optional)
//  4. DeltaFetch   — skips previously-scraped URLs (optional)
//  5. Dedup        — skips duplicate URLs within this run
//  6. AutoThrottle — adapts delay based on server response time (optional)
//  7. Cookies      — persists cookies across requests (always, critical for anti-bot)
//  8. Referer      — sets realistic Referer header (always, critical for anti-bot)
//  9. Redirect     — follows HTTP redirects (always)
// 10. DepthLimit   — limits crawl depth (optional)
// 11. Retry        — retries failed requests (always, innermost)
func buildMiddlewares(cfg *foxhound.Config) []foxhound.Middleware {
	var mws []foxhound.Middleware

	// 0. Concurrency — outermost, limits parallel in-flight requests per domain
	if cfg.Middleware.Concurrency.PerDomain > 0 {
		mws = append(mws, middleware.NewConcurrency(cfg.Middleware.Concurrency.PerDomain))
		slog.Debug("middleware: concurrency enabled", "per_domain", cfg.Middleware.Concurrency.PerDomain)
	}

	// 1. Metrics — outermost after concurrency, records everything
	if cfg.Monitor.Metrics.Enabled {
		mws = append(mws, middleware.NewMetrics("foxhound"))
		slog.Debug("middleware: metrics enabled")
	}

	// 2. RateLimit
	if cfg.Middleware.RateLimit.Enabled {
		rps := cfg.Middleware.RateLimit.RequestsPerSec
		burst := cfg.Middleware.RateLimit.BurstSize
		if rps <= 0 {
			rps = 1.0
		}
		if burst <= 0 {
			burst = 1
		}
		mws = append(mws, middleware.NewRateLimit(rps, burst))
		slog.Debug("middleware: ratelimit enabled", "rps", rps, "burst", burst)
	}

	// 3. RobotsTxt
	if cfg.Middleware.RobotsTxt.Enabled {
		mws = append(mws, middleware.NewRobotsTxt(cfg.Identity.Browser))
		slog.Debug("middleware: robots.txt enabled", "user_agent", cfg.Identity.Browser)
	}

	// 4. DeltaFetch
	if cfg.Middleware.DeltaFetch.Enabled {
		store := buildDeltaStore(cfg.Middleware.DeltaFetch.Store)
		strategy := middleware.DeltaSkipSeen
		if cfg.Middleware.DeltaFetch.Strategy == "skip_recent" {
			strategy = middleware.DeltaSkipRecent
		}
		mws = append(mws, middleware.NewDeltaFetch(strategy, store, cfg.Middleware.DeltaFetch.TTL.Duration))
		slog.Debug("middleware: deltafetch enabled", "strategy", cfg.Middleware.DeltaFetch.Strategy, "store", cfg.Middleware.DeltaFetch.Store)
	}

	// 5. Dedup — always on
	mws = append(mws, middleware.NewDedup())

	// 6. AutoThrottle
	if cfg.Middleware.AutoThrottle.Enabled {
		mws = append(mws, middleware.NewAutoThrottle(middleware.AutoThrottleConfig{
			TargetConcurrency: cfg.Middleware.AutoThrottle.TargetConcurrency,
			InitialDelay:      cfg.Middleware.AutoThrottle.InitialDelay.Duration,
			MinDelay:          cfg.Middleware.AutoThrottle.MinDelay.Duration,
			MaxDelay:          cfg.Middleware.AutoThrottle.MaxDelay.Duration,
		}))
		slog.Debug("middleware: autothrottle enabled",
			"target_concurrency", cfg.Middleware.AutoThrottle.TargetConcurrency)
	}

	// 7. Cookies — always on (critical: sites set cookies then check them)
	mws = append(mws, middleware.NewCookies())

	// 8. Referer — always on (realistic browsing pattern)
	mws = append(mws, middleware.NewReferer())

	// 9. Redirect — always on (follow 301/302/307/308)
	mws = append(mws, middleware.NewRedirect(10))

	// 10. DepthLimit
	if cfg.Middleware.DepthLimit.Max > 0 {
		mws = append(mws, middleware.NewDepthLimit(cfg.Middleware.DepthLimit.Max))
		slog.Debug("middleware: depth limit enabled", "max", cfg.Middleware.DepthLimit.Max)
	}

	// 11. Retry — innermost, retries the actual fetch
	mws = append(mws, middleware.NewRetry(3, 500*time.Millisecond))

	return mws
}

// buildDeltaStore creates a DeltaStore from the config store type.
func buildDeltaStore(storeType string) middleware.DeltaStore {
	switch strings.ToLower(storeType) {
	case "sqlite":
		s, err := middleware.NewSQLiteDeltaStore("foxhound_delta.db")
		if err != nil {
			slog.Warn("deltafetch: sqlite store failed, falling back to memory", "err", err)
			return &middleware.MemoryDeltaStore{}
		}
		return s
	case "redis":
		s, err := middleware.NewRedisDeltaStore("localhost:6379", "", 0, "foxhound:delta")
		if err != nil {
			slog.Warn("deltafetch: redis store failed, falling back to memory", "err", err)
			return &middleware.MemoryDeltaStore{}
		}
		return s
	default:
		return &middleware.MemoryDeltaStore{}
	}
}

// buildCacheMiddleware creates a response caching middleware.
func buildCacheMiddleware(c cache.Cache, ttl time.Duration) foxhound.Middleware {
	return foxhound.MiddlewareFunc(func(next foxhound.Fetcher) foxhound.Fetcher {
		return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
			key := job.Method + ":" + job.URL
			if data, ok := c.Get(ctx, key); ok {
				var resp foxhound.Response
				if err := json.Unmarshal(data, &resp); err == nil {
					resp.Job = job
					slog.Debug("cache: hit", "url", job.URL)
					return &resp, nil
				}
			}
			resp, err := next.Fetch(ctx, job)
			if err == nil && resp.StatusCode == 200 {
				if data, merr := json.Marshal(resp); merr == nil {
					_ = c.Set(ctx, key, data, ttl)
				}
			}
			return resp, err
		})
	})
}

// buildPipelineStages constructs pipeline stages and export writers from the
// config pipeline entries. outputDir is used to resolve relative export paths.
func buildPipelineStages(entries []foxhound.PipelineEntry, outputDir string) ([]foxhound.Pipeline, []foxhound.Writer) {
	var stages []foxhound.Pipeline
	var writers []foxhound.Writer

	for _, entry := range entries {
		if entry.Validate != nil {
			stages = append(stages, &pipeline.Validate{Required: entry.Validate.Required})
			slog.Debug("pipeline: added validate stage", "required", entry.Validate.Required)
		}

		if entry.Clean != nil {
			stages = append(stages, &pipeline.Clean{
				TrimWhitespace: entry.Clean.TrimWhitespace,
				NormalizePrice: entry.Clean.NormalizePrice,
			})
			slog.Debug("pipeline: added clean stage")
		}

		for _, exp := range entry.Export {
			w, err := buildWriter(exp, outputDir)
			if err != nil {
				slog.Warn("pipeline: skipping export — failed to create writer",
					"type", exp.Type, "path", exp.Path, "err", err)
				continue
			}
			writers = append(writers, w)
			slog.Debug("pipeline: added export writer", "type", exp.Type, "path", exp.Path)
		}
	}

	// Guarantee non-nil slices so callers can range without nil checks.
	if stages == nil {
		stages = []foxhound.Pipeline{}
	}
	if writers == nil {
		writers = []foxhound.Writer{}
	}

	return stages, writers
}

// buildWriter creates a single Writer from an ExportConfig.
func buildWriter(exp foxhound.ExportConfig, outputDir string) (foxhound.Writer, error) {
	path := exp.Path
	if path == "" {
		path = outputDir + "/output." + exp.Type
	}

	switch strings.ToLower(exp.Type) {
	case "json":
		return export.NewJSON(path, export.JSONArray)
	case "jsonl", "ndjson":
		return export.NewJSON(path, export.JSONLines)
	case "csv":
		return export.NewCSV(path)
	case "webhook":
		if path == "" {
			return nil, fmt.Errorf("webhook export requires a URL in the path field")
		}
		var opts []export.WebhookOption
		if exp.BatchSize > 0 {
			opts = append(opts, export.WithBatchSize(exp.BatchSize))
		}
		return export.NewWebhook(path, opts...), nil
	case "postgres":
		connString := os.Getenv("FOXHOUND_EXPORT_DB")
		if connString == "" {
			connString = path // fall back to path field
		}
		if connString == "" {
			return nil, fmt.Errorf("postgres export requires FOXHOUND_EXPORT_DB env var or path field")
		}
		table := exp.Table
		if table == "" {
			table = "items"
		}
		var opts []export.PostgresOption
		if exp.UpsertKey != "" {
			opts = append(opts, export.WithUpsert(exp.UpsertKey))
		}
		if exp.BatchSize > 0 {
			opts = append(opts, export.WithPGBatchSize(exp.BatchSize))
		}
		return export.NewPostgres(connString, table, opts...)
	default:
		return nil, fmt.Errorf("unknown export type %q (supported: json, jsonl, csv, webhook, postgres)", exp.Type)
	}
}

// cmdRun loads the configuration and runs the hunt using engine.Hunt.
func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: foxhound run [flags]")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}

	configPath := fs.String("config", "config.yaml", "path to configuration file")
	hunt := fs.String("hunt", "", "name of the hunt to run (optional, uses config default)")
	workers := fs.Int("workers", 0, "number of walker workers (overrides config)")
	dryRun := fs.Bool("dry-run", false, "validate config and print summary without running")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	cfg, err := foxhound.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration %q: %v\n", *configPath, err)
		os.Exit(1)
	}

	// Setup logging from config + global verbose flag.
	foxhound.SetupLogging(cfg.Logging, globalVerbose)

	// CLI flags override config values when explicitly provided.
	if *workers > 0 {
		cfg.Hunt.Walkers = *workers
	}
	if *hunt != "" {
		cfg.Hunt.Domain = *hunt
	}

	slog.Info("configuration loaded",
		"config", *configPath,
		"domain", cfg.Hunt.Domain,
		"walkers", cfg.Hunt.Walkers,
		"queue", cfg.Queue.Backend,
		"log_level", cfg.Logging.Level,
	)

	printRunSummary(cfg, *configPath)

	if *dryRun {
		fmt.Println("\nDry run complete. Configuration is valid.")
		return
	}

	if err := runHunt(cfg); err != nil {
		slog.Error("hunt failed", "err", err)
		fmt.Fprintf(os.Stderr, "Hunt failed: %v\n", err)
		os.Exit(1)
	}
}

// runHunt wires and executes the engine.Hunt from a loaded config.
// It is extracted so resume.go can call it directly with a pre-built queue.
func runHunt(cfg *foxhound.Config) error {
	return runHuntWithQueue(cfg, nil)
}

// runHuntWithQueue wires the engine using cfg, substituting overrideQueue when
// non-nil (resume path). A nil overrideQueue causes the queue to be built from
// cfg.Queue.
func runHuntWithQueue(cfg *foxhound.Config, overrideQueue foxhound.Queue) error {
	// --- Identity ---
	idOpts := []identity.Option{
		identity.WithBrowser(identity.Browser(cfg.Identity.Browser)),
	}
	// Pick a random OS from the configured list if more than one.
	if len(cfg.Identity.OS) == 1 {
		idOpts = append(idOpts, identity.WithOS(identity.OS(cfg.Identity.OS[0])))
	}
	prof := identity.Generate(idOpts...)
	slog.Info("identity generated",
		"browser", prof.BrowserName,
		"os", prof.OS,
		"ua", prof.UA,
	)

	// --- Fetchers ---
	stealthFetcher := fetch.NewStealth(
		fetch.WithIdentity(prof),
		fetch.WithTimeout(cfg.Fetch.Static.Timeout.Duration),
	)

	var browserFetcher foxhound.Fetcher
	if cfg.Fetch.Browser.Instances > 0 {
		cf, err := fetch.NewCamoufox(
			fetch.WithBrowserIdentity(prof),
			fetch.WithBlockImages(cfg.Fetch.Browser.BlockImages),
			fetch.WithHeadless(cfg.Fetch.Browser.Headless),
		)
		if err != nil {
			slog.Warn("fetch: camoufox initialisation failed, continuing static-only",
				"err", err)
		} else {
			browserFetcher = cf
			slog.Info("fetch: camoufox browser fetcher initialised",
				"instances", cfg.Fetch.Browser.Instances,
				"headless", cfg.Fetch.Browser.Headless,
			)
		}
	} else {
		slog.Info("fetch: browser instances = 0, running static-only mode")
	}

	smartFetcher := fetch.NewSmart(stealthFetcher, browserFetcher)

	// --- Proxy pool (all providers) ---
	var proxyProviders []proxy.Provider
	for _, pe := range cfg.Proxy.Providers {
		switch strings.ToLower(pe.Type) {
		case "static":
			if len(pe.List) > 0 {
				proxyProviders = append(proxyProviders, proxy.Static(pe.List))
			}
		case "brightdata":
			if pe.APIKey != "" {
				proxyProviders = append(proxyProviders, proxyproviders.NewBrightData(pe.APIKey, pe.Product, pe.Country))
				slog.Debug("proxy: brightdata provider added", "country", pe.Country)
			}
		case "oxylabs":
			if pe.Username != "" {
				proxyProviders = append(proxyProviders, proxyproviders.NewOxylabs(pe.Username, pe.Password, pe.Product, pe.Country))
				slog.Debug("proxy: oxylabs provider added", "country", pe.Country)
			}
		case "smartproxy":
			if pe.Username != "" {
				proxyProviders = append(proxyProviders, proxyproviders.NewSmartproxy(pe.Username, pe.Password, pe.Country))
				slog.Debug("proxy: smartproxy provider added", "country", pe.Country)
			}
		default:
			slog.Warn("proxy: unknown provider type, skipping", "type", pe.Type)
		}
	}
	proxyPool := proxy.NewPool(proxyProviders...)
	if proxyPool.Len() > 0 {
		slog.Info("proxy pool loaded", "count", proxyPool.Len())
	}

	// --- Queue ---
	var q foxhound.Queue
	if overrideQueue != nil {
		q = overrideQueue
	} else {
		var err error
		q, err = buildQueue(cfg.Queue.Backend, "")
		if err != nil {
			return fmt.Errorf("creating queue: %w", err)
		}
	}
	defer func() { _ = q.Close() }()

	// --- Cache middleware ---
	var responseCache cache.Cache
	if cfg.Cache.Backend != "" && cfg.Cache.Backend != "none" {
		switch strings.ToLower(cfg.Cache.Backend) {
		case "memory":
			responseCache = cache.NewMemory(cfg.Cache.MaxSize)
		case "file":
			if c, err := cache.NewFile("./foxhound_cache", cfg.Cache.TTL.Duration); err == nil {
				responseCache = c
			} else {
				slog.Warn("cache: file backend failed", "err", err)
			}
		case "sqlite":
			if c, err := cache.NewSQLite("foxhound_cache.db"); err == nil {
				responseCache = c
			} else {
				slog.Warn("cache: sqlite backend failed", "err", err)
			}
		}
		if responseCache != nil {
			slog.Info("cache: enabled", "backend", cfg.Cache.Backend, "ttl", cfg.Cache.TTL.Duration)
			defer responseCache.Close()
		}
	}

	// --- Middleware chain (all 11 middleware) ---
	mws := buildMiddlewares(cfg)

	// Insert cache middleware before dedup if cache is enabled
	if responseCache != nil {
		cacheMw := buildCacheMiddleware(responseCache, cfg.Cache.TTL.Duration)
		// Insert after ratelimit but before dedup (position 2-ish)
		mws = append([]foxhound.Middleware{cacheMw}, mws...)
	}

	// --- CAPTCHA solver ---
	var captchaSolver captcha.Solver
	if cfg.Captcha.Enabled && cfg.Captcha.APIKey != "" {
		switch strings.ToLower(cfg.Captcha.Provider) {
		case "capsolver":
			captchaSolver = captcha.NewCapSolver(cfg.Captcha.APIKey)
			slog.Info("captcha: capsolver enabled")
		case "twocaptcha", "2captcha":
			captchaSolver = captcha.NewTwoCaptcha(cfg.Captcha.APIKey)
			slog.Info("captcha: twocaptcha enabled")
		default:
			slog.Warn("captcha: unknown provider", "provider", cfg.Captcha.Provider)
		}
	}
	// Suppress unused var if captcha is off — solver is passed to HuntConfig
	_ = captchaSolver

	// --- Prometheus monitoring ---
	var promExporter *monitor.PrometheusExporter
	if cfg.Monitor.Metrics.Enabled {
		promExporter = monitor.NewPrometheus("foxhound", cfg.Monitor.Metrics.Port)
		if err := promExporter.Start(); err != nil {
			slog.Warn("monitor: prometheus start failed", "err", err)
		} else {
			slog.Info("monitor: prometheus started", "port", cfg.Monitor.Metrics.Port)
			defer promExporter.Stop()
		}
	}

	// --- Alerting ---
	var alerter *monitor.Alerter
	if cfg.Monitor.Alerting.WebhookURL != "" {
		var rules []monitor.AlertRule
		if cfg.Monitor.Alerting.ErrorRateThreshold > 0 {
			rules = append(rules, monitor.ErrorRateRule(
				cfg.Monitor.Alerting.ErrorRateThreshold,
				cfg.Monitor.Alerting.Cooldown.Duration,
			))
		}
		if cfg.Monitor.Alerting.BlockRateThreshold > 0 {
			rules = append(rules, monitor.BlockRateRule(
				cfg.Monitor.Alerting.BlockRateThreshold,
				cfg.Monitor.Alerting.Cooldown.Duration,
			))
		}
		if len(rules) > 0 {
			alerter = monitor.NewAlerter(cfg.Monitor.Alerting.WebhookURL, rules...)
			slog.Info("monitor: alerting enabled", "webhook", cfg.Monitor.Alerting.WebhookURL, "rules", len(rules))
		}
	}
	_ = alerter // used after hunt completes for final check

	// --- Pipeline ---
	outputDir, _ := os.Getwd()
	pipelineStages, writers := buildPipelineStages(cfg.Pipeline, outputDir)
	defer func() {
		for _, w := range writers {
			_ = w.Close()
		}
	}()

	// --- Seed jobs ---
	seeds := buildSeeds(cfg.Hunt.Domain)

	// --- Signal context for graceful shutdown ---
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- Build and run hunt ---
	huntCfg := engine.HuntConfig{
		Name:            cfg.Hunt.Domain,
		Domain:          cfg.Hunt.Domain,
		Walkers:         cfg.Hunt.Walkers,
		MaxConcurrency:  cfg.Hunt.MaxConcurrency,
		Seeds:           seeds,
		Processor:       defaultProcessor,
		Fetcher:         smartFetcher,
		Queue:           q,
		Pipelines:       pipelineStages,
		Writers:         writers,
		Middlewares:     mws,
		BehaviorProfile: cfg.Behavior.Profile, // "careful" | "moderate" | "aggressive"
	}

	h := engine.NewHunt(huntCfg)

	slog.Info("hunt starting",
		"domain", cfg.Hunt.Domain,
		"walkers", cfg.Hunt.Walkers,
		"seeds", len(seeds),
	)

	if err := h.Run(ctx); err != nil {
		return fmt.Errorf("hunt execution: %w", err)
	}

	stats := h.Stats()
	printStatsSummary(stats)

	return nil
}

// buildSeeds creates the initial seed jobs for a domain. When no explicit seed
// URLs are provided in config the domain homepage is used.
func buildSeeds(domain string) []*foxhound.Job {
	// Normalise: add https:// if no scheme is present.
	seedURL := domain
	if !strings.HasPrefix(domain, "http://") && !strings.HasPrefix(domain, "https://") {
		seedURL = "https://" + domain
	}

	parsed, err := url.Parse(seedURL)
	if err != nil {
		slog.Warn("seeds: could not parse domain URL, using as-is", "domain", domain, "err", err)
	}

	host := domain
	if parsed != nil && parsed.Host != "" {
		host = parsed.Host
	}

	return []*foxhound.Job{
		{
			ID:        uuid.NewString(),
			URL:       seedURL,
			Method:    "GET",
			FetchMode: foxhound.FetchAuto,
			Priority:  foxhound.PriorityHigh,
			Depth:     0,
			Domain:    host,
			CreatedAt: time.Now(),
		},
	}
}

// printStatsSummary prints a formatted stats summary after a hunt completes.
func printStatsSummary(stats *engine.Stats) {
	fmt.Printf("\nHunt complete\n")
	fmt.Printf("%-20s %d\n", "Requests:", stats.RequestCount.Load())
	fmt.Printf("%-20s %d\n", "Success:", stats.SuccessCount.Load())
	fmt.Printf("%-20s %d\n", "Errors:", stats.ErrorCount.Load())
	fmt.Printf("%-20s %d\n", "Blocked:", stats.BlockedCount.Load())
	fmt.Printf("%-20s %d\n", "Items scraped:", stats.ItemCount.Load())
	fmt.Printf("%-20s %d\n", "Escalated:", stats.EscalatedCount.Load())
	fmt.Printf("%-20s %d bytes\n", "Bytes received:", stats.BytesReceived.Load())
}

// ---------------------------------------------------------------------------
// Redis URL parsing helper
// ---------------------------------------------------------------------------

// parseRedisURL parses a redis:// URL into (addr, password, db).
// Supports redis://:password@host:port/db and redis://host:port/db.
func parseRedisURL(rawURL string) (addr, password string, db int, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid redis URL: %w", err)
	}
	if u.Scheme != "redis" {
		return "", "", 0, fmt.Errorf("expected redis:// scheme, got %q", u.Scheme)
	}
	addr = u.Host
	if addr == "" {
		addr = "localhost:6379"
	}
	if u.User != nil {
		password, _ = u.User.Password()
	}
	// Parse /db path component.
	if u.Path != "" && u.Path != "/" {
		dbStr := strings.TrimPrefix(u.Path, "/")
		if _, scanErr := fmt.Sscanf(dbStr, "%d", &db); scanErr != nil {
			// Non-numeric db path — ignore, default to 0.
			db = 0
		}
	}
	return addr, password, db, nil
}

// ---------------------------------------------------------------------------
// printRunSummary — kept from original, no change
// ---------------------------------------------------------------------------

func printRunSummary(cfg *foxhound.Config, configPath string) {
	fmt.Printf("\nFoxhound Hunt Summary\n")
	fmt.Printf("%-20s %s\n", "Config:", configPath)
	fmt.Printf("%-20s %s\n", "Domain:", cfg.Hunt.Domain)
	fmt.Printf("%-20s %d\n", "Walkers:", cfg.Hunt.Walkers)
	fmt.Printf("%-20s %s\n", "Queue backend:", cfg.Queue.Backend)
	fmt.Printf("%-20s %s\n", "Static timeout:", cfg.Fetch.Static.Timeout.Duration)
	fmt.Printf("%-20s %s\n", "Browser timeout:", cfg.Fetch.Browser.Timeout.Duration)
	fmt.Printf("%-20s %t\n", "Rate limit:", cfg.Middleware.RateLimit.Enabled)
	if cfg.Middleware.RateLimit.Enabled {
		fmt.Printf("%-20s %.2f req/s (burst %d)\n",
			"  Rate:",
			cfg.Middleware.RateLimit.RequestsPerSec,
			cfg.Middleware.RateLimit.BurstSize,
		)
	}
	fmt.Printf("%-20s %d\n", "Max depth:", cfg.Middleware.DepthLimit.Max)
	fmt.Printf("%-20s %d pipeline stage(s)\n", "Pipeline:", len(cfg.Pipeline))
}
