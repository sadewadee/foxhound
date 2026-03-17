// Example: Travel site scraper — hotel listings with JavaScript rendering
//
// Demonstrates how to use SmartFetcher for sites that require JavaScript.
// SmartFetcher starts with the fast TLS-impersonating HTTP client and
// automatically escalates to the Camoufox browser when a block (403/429/503)
// is detected.
//
// Camoufox uses the Juggler protocol instead of CDP, making it far less
// detectable than Chromium-based scrapers. In Phase 1 the CamoufoxFetcher is
// a stub — it will return an error gracefully rather than crashing.
//
// This example also shows:
//
//   - Trail-based navigation: homepage → search → results → detail
//   - Proxy configuration (placeholder URLs — replace with real proxies)
//   - JSON Lines export (one JSON object per scraped hotel)
//   - Explicit FetchMode per job to force browser mode on JS-heavy pages
//
// Run:
//
//	go run ./examples/travel/
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/engine"
	"github.com/foxhound-scraper/foxhound/fetch"
	"github.com/foxhound-scraper/foxhound/identity"
	"github.com/foxhound-scraper/foxhound/parse"
	"github.com/foxhound-scraper/foxhound/pipeline"
	"github.com/foxhound-scraper/foxhound/pipeline/export"
	"github.com/foxhound-scraper/foxhound/queue"
)

const outputFile = "hotels.jsonl"

// proxyURLs contains placeholder proxy addresses.
// Replace these with real proxy provider URLs in production:
//
//	"http://user:pass@us-proxy.example.com:8080"
//
// Proxy geo MUST match the identity locale/timezone. New York proxy + Tokyo
// timezone = instant anti-bot flag.
var proxyURLs = []string{
	// "http://user:pass@proxy1.example.com:8080",
	// "http://user:pass@proxy2.example.com:8080",
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// -------------------------------------------------------------------------
	// 1. Generate a consistent identity profile.
	//
	// For travel sites that target US visitors, use an American English locale
	// paired with an Eastern timezone and a US-based proxy.
	// -------------------------------------------------------------------------
	profile := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
		identity.WithLocale("en-US", "en-US", "en"),
		identity.WithTimezone("America/New_York"),
		identity.WithGeo(40.7128, -74.0060), // New York City coordinates
	)

	// -------------------------------------------------------------------------
	// 2. Build the static (TLS-impersonating) fetcher.
	//
	// WithProxy wires the first proxy from the pool if one is configured.
	// In a production scraper you would rotate proxies via the proxy package.
	// -------------------------------------------------------------------------
	stealthOpts := []fetch.StealthOption{
		fetch.WithIdentity(profile),
		fetch.WithTimeout(30 * time.Second),
	}
	if len(proxyURLs) > 0 {
		stealthOpts = append(stealthOpts, fetch.WithProxy(proxyURLs[0]))
	}
	staticFetcher := fetch.NewStealth(stealthOpts...)

	// -------------------------------------------------------------------------
	// 3. Build the CamoufoxFetcher (browser mode).
	//
	// In Phase 1, NewCamoufox returns a stub that will log a warning and return
	// an error when called. The SmartFetcher will surface that error to the
	// walker's retry policy rather than crashing.
	//
	// To enable real browser rendering in Phase 2:
	//  - Install Camoufox: go run github.com/camoufox/go-playwright/run@latest
	//  - Replace the stub with the full CamoufoxFetcher implementation
	// -------------------------------------------------------------------------
	browserFetcher, err := fetch.NewCamoufox()
	if err != nil {
		// NewCamoufox only fails in Phase 2 when playwright cannot be launched.
		// In Phase 1 the stub always succeeds here; errors surface only on Fetch.
		log.Fatalf("failed to create browser fetcher: %v", err)
	}

	// -------------------------------------------------------------------------
	// 4. Create a SmartFetcher that routes jobs automatically.
	//
	// FetchAuto  → tries static first; escalates to browser if blocked (403/429/503)
	// FetchBrowser → goes directly to Camoufox (for known JS-heavy pages)
	// FetchStatic  → stays on the HTTP client regardless of response
	// -------------------------------------------------------------------------
	smartFetcher := fetch.NewSmart(staticFetcher, browserFetcher)
	defer smartFetcher.Close()

	// -------------------------------------------------------------------------
	// 5. Create a JSON Lines writer.
	//
	// JSON Lines (NDJSON) produces one JSON object per line, which is easy to
	// stream-process with tools like jq, BigQuery, and Spark.
	// -------------------------------------------------------------------------
	jsonWriter, err := export.NewJSON(outputFile, export.JSONLines)
	if err != nil {
		log.Fatalf("failed to create JSON writer: %v", err)
	}
	defer jsonWriter.Close()

	// -------------------------------------------------------------------------
	// 6. Build the pipeline.
	// -------------------------------------------------------------------------
	pipelineChain := pipeline.NewChain(
		&pipeline.Validate{Required: []string{"name", "price", "url"}},
		&pipeline.Clean{TrimWhitespace: true},
	)

	// -------------------------------------------------------------------------
	// 7. Define the processor.
	//
	// This example uses toscrape.com as a safe public target for the listing
	// page. A real travel site would require browser mode for search results.
	//
	// The processor demonstrates the Trail pattern: each step in the navigation
	// flow produces Jobs for the next step. The engine processes them in priority
	// order so deeper pages are crawled concurrently.
	// -------------------------------------------------------------------------
	processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
		slog.Info("processing page",
			"url", resp.URL,
			"status", resp.StatusCode,
			"mode", resp.FetchMode,
			"duration_ms", resp.Duration.Milliseconds(),
		)

		if resp.StatusCode != 200 {
			slog.Warn("non-200 response", "status", resp.StatusCode, "url", resp.URL)
			return &foxhound.Result{}, nil
		}

		doc, err := parse.NewDocument(resp)
		if err != nil {
			return nil, fmt.Errorf("parsing HTML from %s: %w", resp.URL, err)
		}

		var items []*foxhound.Item
		var nextJobs []*foxhound.Job

		// For this demo we reuse books.toscrape.com as a stand-in for a hotel
		// listing page. Each "book" represents a "hotel room".
		doc.Each("article.product_pod", func(i int, s *goquery.Selection) {
			item := foxhound.NewItem()
			item.URL = resp.URL

			name, _ := s.Find("h3 a").Attr("title")
			price := s.Find("p.price_color").Text()
			detailHref, _ := s.Find("h3 a").Attr("href")

			item.Set("name", name)
			item.Set("price", price)
			item.Set("url", resolveURL(resp.URL, detailHref))
			item.Set("scraped_at", resp.Job.CreatedAt.Format(time.RFC3339))

			processed, pipeErr := pipelineChain.Process(ctx, item)
			if pipeErr != nil {
				slog.Warn("pipeline error", "err", pipeErr)
				return
			}
			if processed != nil {
				items = append(items, processed)
			}
		})

		// Paginate: follow the "next" link at lower priority than fresh seeds.
		nextHref := doc.Attr("li.next a", "href")
		if nextHref != "" {
			nextJobs = append(nextJobs, &foxhound.Job{
				ID:  fmt.Sprintf("listing-%d", resp.Job.Depth+1),
				URL: resolveURL(resp.URL, nextHref),
				// Use FetchBrowser on deeper pages to demonstrate escalation.
				// In Phase 1 the Camoufox stub will error; the retry policy handles it.
				FetchMode: foxhound.FetchAuto,
				Priority:  foxhound.PriorityNormal,
				Depth:     resp.Job.Depth + 1,
			})
		}

		return &foxhound.Result{Items: items, Jobs: nextJobs}, nil
	})

	// -------------------------------------------------------------------------
	// 8. Seed the queue and run the Hunt.
	//
	// Phase 1: only the static page is scraped (1 page, fast).
	// Phase 2: with real Camoufox the full 50-page catalogue will be crawled.
	// -------------------------------------------------------------------------
	q := queue.NewMemoryQueue()
	defer q.Close()

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "travel-hotels",
		Domain:    "books.toscrape.com",
		Walkers:   3,
		Fetcher:   smartFetcher,
		Processor: processor,
		Queue:     q,
		Writers:   []foxhound.Writer{jsonWriter},
		Seeds: []*foxhound.Job{
			{
				ID:        "seed-listings",
				URL:       "http://books.toscrape.com/",
				FetchMode: foxhound.FetchAuto, // smart router decides
				Priority:  foxhound.PriorityHigh,
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	slog.Info("starting travel hunt", "output", outputFile)
	if err := h.Run(ctx); err != nil {
		log.Fatalf("hunt failed: %v", err)
	}

	fmt.Printf("\nHunt complete: %s\n", h.Stats().Summary())
	fmt.Printf("Output written to: %s\n", outputFile)
}

// resolveURL resolves href relative to base URL.
func resolveURL(base, href string) string {
	if len(href) > 4 && href[:4] == "http" {
		return href
	}
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '/' {
			return base[:i+1] + href
		}
	}
	return href
}
