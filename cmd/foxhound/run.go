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

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/engine"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
	"github.com/sadewadee/foxhound/middleware"
	"github.com/sadewadee/foxhound/parse"
	"github.com/sadewadee/foxhound/pipeline"
	"github.com/sadewadee/foxhound/pipeline/export"
	"github.com/sadewadee/foxhound/proxy"
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

// buildMiddlewares assembles the middleware chain from config. Order:
//  1. RateLimit (outermost — limits before dedup cost)
//  2. Dedup
//  3. DepthLimit
//  4. Retry (innermost — retries the actual fetch)
func buildMiddlewares(cfg *foxhound.Config) []foxhound.Middleware {
	var mws []foxhound.Middleware

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

	mws = append(mws, middleware.NewDedup())

	if cfg.Middleware.DepthLimit.Max > 0 {
		mws = append(mws, middleware.NewDepthLimit(cfg.Middleware.DepthLimit.Max))
		slog.Debug("middleware: depth limit enabled", "max", cfg.Middleware.DepthLimit.Max)
	}

	// Retry is always last so it retries the full inner stack.
	mws = append(mws, middleware.NewRetry(3, 500*time.Millisecond))

	return mws
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

	// --- Proxy pool ---
	var providers []proxy.Provider
	for _, pe := range cfg.Proxy.Providers {
		if pe.Type == "static" && len(pe.List) > 0 {
			providers = append(providers, proxy.Static(pe.List))
		}
	}
	proxyPool := proxy.NewPool(providers...)
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

	// --- Middleware chain ---
	mws := buildMiddlewares(cfg)

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
		Name:        cfg.Hunt.Domain,
		Domain:      cfg.Hunt.Domain,
		Walkers:     cfg.Hunt.Walkers,
		Seeds:       seeds,
		Processor:   defaultProcessor,
		Fetcher:     smartFetcher,
		Queue:       q,
		Pipelines:   pipelineStages,
		Writers:     writers,
		Middlewares: mws,
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
