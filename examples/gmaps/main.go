//go:build playwright

// Google Maps Scraper — demonstrates the collect-pool pattern.
//
// Flow:
//   1. Navigate to Maps search → scroll feed → collect all profile URLs (Collect phase)
//   2. Open each profile URL concurrently → extract business data (Process phase)
//   3. (Optional) Visit business website → extract contacts (Enrichment phase)
//
// Usage:
//
//   # Default: in-memory pool
//   go run -tags playwright ./examples/gmaps/ -query "yoga studio canggu bali"
//
//   # SQLite pool (resumable)
//   go run -tags playwright ./examples/gmaps/ -query "cafe seminyak" -pool sqlite -pool-dsn gmaps_pool.db
//
//   # PostgreSQL pool (distributed)
//   go run -tags playwright ./examples/gmaps/ -query "villa ubud" -pool postgres -pool-dsn "postgres://user:pass@localhost/foxhound?sslmode=disable"
//
//   # With website enrichment
//   go run -tags playwright ./examples/gmaps/ -query "yoga studio canggu bali" -enrich
//
//   # Visible browser
//   go run -tags playwright ./examples/gmaps/ -query "yoga studio canggu bali" -headless false
//
//   # Fast mode (no warm-up, aggressive timing)
//   go run -tags playwright ./examples/gmaps/ -query "yoga studio canggu bali" -fast
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
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
	"github.com/sadewadee/foxhound/queue"
)

func main() {
	query := flag.String("query", "yoga studio canggu bali", "Maps search query")
	poolBackend := flag.String("pool", "memory", "Pool backend: memory, sqlite, postgres")
	poolDSN := flag.String("pool-dsn", "gmaps_pool.db", "Pool DSN (file path for sqlite, connection string for postgres)")
	poolTable := flag.String("pool-table", "gmaps_pool", "Pool table name (postgres only)")
	output := flag.String("output", "gmaps_results.jsonl", "Output file path")
	workers := flag.Int("workers", 3, "Concurrent workers for profile scraping")
	maxResults := flag.Int("max-results", 120, "Max results to collect from feed")
	headless := flag.String("headless", "true", "Browser headless mode: true, false, virtual")
	enrich := flag.Bool("enrich", false, "Visit business websites to extract contacts")
	fast := flag.Bool("fast", false, "Disable warm-up, use aggressive timing (dev/testing)")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	fmt.Println("================================================================")
	fmt.Printf("  Google Maps Scraper — %q\n", *query)
	fmt.Printf("  Pool: %s | Workers: %d | Max: %d | Enrich: %v\n", *poolBackend, *workers, *maxResults, *enrich)
	fmt.Println("================================================================")

	// ── Identity ─────────────────────────────────────────────────
	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSMacOS),
		identity.WithLocale("en-US", "en-US", "en"),
		identity.WithTimezone("Asia/Makassar"),
		identity.WithGeo(-8.34, 115.09),
	)

	// ── Browser fetcher (for Maps SPA) ──────────────────────────
	cf, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithHeadless(*headless),
		fetch.WithBlockImages(false),
		fetch.WithBrowserTimeout(60*time.Second),
	)
	if err != nil {
		slog.Error("camoufox init failed", "err", err)
		os.Exit(1)
	}
	defer cf.Close()

	// ── Stealth fetcher (for website enrichment) ────────────────
	stealth := fetch.NewStealth(
		fetch.WithIdentity(prof),
		fetch.WithTimeout(20*time.Second),
	)
	defer stealth.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// ════════════════════════════════════════════════════════════
	// PHASE 1: COLLECT — scroll Maps feed, collect all profile URLs
	// ════════════════════════════════════════════════════════════
	fmt.Println("\n[PHASE 1] Collecting profile URLs from Maps feed...")

	pool := buildPool(*poolBackend, *poolDSN, *poolTable)
	defer pool.Close()

	searchURL := fmt.Sprintf("https://www.google.com/maps/search/%s/",
		strings.ReplaceAll(*query, " ", "+"))

	collectTrail := engine.NewTrail("gmaps-collect").
		Navigate(searchURL).
		WaitOptional("button[aria-label*='Accept'], button[aria-label*='Reject'], form[action*='consent'] button", 3*time.Second).
		ClickOptional("button[aria-label*='Reject'], form[action*='consent'] button").
		Wait("div[role='feed']", 15*time.Second).
		InfiniteScrollInUntil("div[role='feed']", "div.Nv2PK", *maxResults, 200).
		Collect("a.hfpxzc", "href")

	if *fast {
		collectTrail.NoWarmup()
	}

	collectJobs := collectTrail.ToJobs()
	if len(collectJobs) == 0 {
		slog.Error("no jobs generated from trail")
		os.Exit(1)
	}

	var resp *foxhound.Response
	for i, job := range collectJobs {
		job.ID = fmt.Sprintf("gmaps-collect-%d", i)
		var err error
		resp, err = cf.Fetch(ctx, job)
		if err != nil {
			slog.Error("collect fetch failed", "url", job.URL, "err", err)
			os.Exit(1)
		}
		slog.Info("collect step complete", "url", job.URL, "status", resp.StatusCode)
	}

	// Extract collected URLs from StepResults
	var profileURLs []string
	if resp.StepResults != nil {
		for _, v := range resp.StepResults {
			if urls, ok := v.([]interface{}); ok {
				for _, u := range urls {
					if s, ok := u.(string); ok && strings.Contains(s, "/maps/place/") {
						profileURLs = append(profileURLs, s)
					}
				}
			}
		}
	}

	// Fallback: extract from DOM if StepResults empty
	if len(profileURLs) == 0 {
		doc, err := parse.NewDocument(resp)
		if err == nil {
			doc.Each("a.hfpxzc", func(_ int, s *goquery.Selection) {
				if href, exists := s.Attr("href"); exists && strings.Contains(href, "/maps/place/") {
					profileURLs = append(profileURLs, href)
				}
			})
		}
	}

	pool.AddBatch(ctx, profileURLs)
	fmt.Printf("  Collected %d unique profile URLs\n", pool.Len())

	if pool.Len() == 0 {
		fmt.Println("  No profiles found. Maps may have blocked or query returned no results.")
		os.Exit(1)
	}

	// ════════════════════════════════════════════════════════════
	// PHASE 2: PROCESS — open each profile concurrently, extract data
	// ════════════════════════════════════════════════════════════
	fmt.Printf("\n[PHASE 2] Processing %d profiles with %d workers...\n", pool.Len(), *workers)

	jsonlWriter, err := export.NewJSON(*output, export.JSONLines)
	if err != nil {
		slog.Error("writer init failed", "err", err)
		os.Exit(1)
	}
	defer jsonlWriter.Close()

	enrichFlag := *enrich
	stealthFetcher := stealth

	processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
		doc, err := parse.NewDocument(resp)
		if err != nil {
			return &foxhound.Result{}, nil
		}

		item := foxhound.NewItem()
		item.URL = resp.URL

		item.Set("name", doc.Text("h1.DUwDvf"))
		item.Set("category", doc.Text("button.DkEaL"))
		item.Set("rating", doc.Text("div.F7nice span[aria-hidden='true']"))
		item.Set("review_count", cleanReviewCount(doc.Text("div.F7nice span[aria-label]")))
		item.Set("address", doc.Text("button[data-item-id='address'] div.fontBodyMedium"))
		item.Set("phone", doc.Text("button[data-item-id*='phone'] div.fontBodyMedium"))
		item.Set("plus_code", doc.Text("button[data-item-id='oloc'] div.fontBodyMedium"))
		item.Set("price_range", doc.Text("span[aria-label*='Price']"))
		item.Set("place_url", resp.URL)

		website := doc.Attr("a[data-item-id='authority']", "href")
		if website == "" {
			website = doc.Attr("a[aria-label*='Website']", "href")
		}
		item.Set("website", website)

		hours := doc.Text("div.t39EBf div.fontBodyMedium")
		item.Set("hours", strings.TrimSpace(hours))

		if enrichFlag && website != "" {
			enrichFromWebsite(ctx, stealthFetcher, item, website)
		}

		return &foxhound.Result{Items: []*foxhound.Item{item}}, nil
	})

	pipelineChain := pipeline.NewChain(
		&pipeline.Validate{Required: []string{"name"}},
		&pipeline.Clean{TrimWhitespace: true},
	)

	behaviorProfile := "careful"
	if *fast {
		behaviorProfile = "aggressive"
	}

	h := engine.NewHunt(engine.HuntConfig{
		Name:            "gmaps-profiles",
		Domain:          "www.google.com",
		Walkers:         *workers,
		Pool:            pool,
		PoolFetchMode:   foxhound.FetchBrowser,
		Fetcher:         cf,
		Processor:       processor,
		Queue:           queue.NewMemoryQueue(),
		Writers:         []foxhound.Writer{jsonlWriter},
		Pipelines:       []foxhound.Pipeline{pipelineChain},
		Middlewares: []foxhound.Middleware{
			middleware.NewRateLimit(0.5, 1),
		},
		BehaviorProfile: behaviorProfile,
	})

	start := time.Now()
	if err := h.Run(ctx); err != nil {
		slog.Error("hunt failed", "err", err)
		os.Exit(1)
	}
	elapsed := time.Since(start)

	stats := h.Stats()
	fmt.Println("\n================================================================")
	fmt.Println("  RESULTS")
	fmt.Println("================================================================")
	fmt.Printf("  Profiles scraped: %d\n", stats.ItemCount.Load())
	fmt.Printf("  Requests:         %d (errors: %d)\n", stats.RequestCount.Load(), stats.ErrorCount.Load())
	fmt.Printf("  Duration:         %s\n", elapsed.Round(time.Second))
	fmt.Printf("  Output:           %s\n", *output)
	fmt.Println("================================================================")
}

func buildPool(backend, dsn, table string) engine.Pool {
	switch backend {
	case "sqlite":
		p, err := engine.NewSQLitePool(dsn)
		if err != nil {
			slog.Error("sqlite pool init failed", "err", err)
			os.Exit(1)
		}
		return p
	case "postgres":
		p, err := engine.NewPostgresPool(dsn, table)
		if err != nil {
			slog.Error("postgres pool init failed", "err", err)
			os.Exit(1)
		}
		return p
	default:
		return engine.NewMemoryPool()
	}
}

func enrichFromWebsite(ctx context.Context, fetcher foxhound.Fetcher, item *foxhound.Item, website string) {
	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	resp, err := fetcher.Fetch(fetchCtx, &foxhound.Job{
		ID:  "enrich-" + website,
		URL: website,
	})
	if err != nil || resp == nil || resp.StatusCode >= 400 {
		return
	}

	emails := parse.ExtractEmails(resp)
	if len(emails) > 0 {
		item.Set("emails", strings.Join(emails, ", "))
	}

	phones := parse.ExtractPhones(resp)
	if len(phones) > 0 {
		item.Set("phones", strings.Join(phones, ", "))
	}
}

func cleanReviewCount(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "()")
	s = strings.TrimSuffix(s, " reviews")
	s = strings.TrimSuffix(s, " review")
	s = strings.ReplaceAll(s, ",", "")
	return s
}
