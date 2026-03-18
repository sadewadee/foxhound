// Example: Real-time price monitor with webhook notifications
//
// Demonstrates how to build a continuous price monitoring loop that:
//
//   - Sends price alerts via a webhook when prices change
//   - Deduplicates items by SKU so each product is only reported once per run
//   - Uses a Transform pipeline stage to compute price changes
//   - Shows the concept of periodic re-scraping using a ticker loop
//
// This example targets books.toscrape.com as a safe public sandbox.
// In production you would substitute real product URLs and a real webhook
// endpoint (e.g., Slack, PagerDuty, or your own alerting service).
//
// Run:
//
//	go run ./examples/realtime/
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/engine"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
	"github.com/PuerkitoBio/goquery"
	"github.com/sadewadee/foxhound/parse"
	"github.com/sadewadee/foxhound/pipeline"
	"github.com/sadewadee/foxhound/pipeline/export"
	"github.com/sadewadee/foxhound/queue"
)

// webhookURL is the endpoint that receives price-change alerts as JSON batches.
// Replace with your real alert endpoint (Slack, PagerDuty, custom service…).
//
//	const webhookURL = "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
const webhookURL = "http://localhost:9999/price-alerts" // placeholder — not reachable

// scrapeInterval controls how often the monitor re-scrapes the target.
const scrapeInterval = 5 * time.Minute

// priceStore is an in-memory map of SKU → last known price.
// In production this would be backed by Redis or a database so prices survive
// restarts and can be queried by other services.
type priceStore struct {
	mu    sync.RWMutex
	prices map[string]float64
}

func newPriceStore() *priceStore {
	return &priceStore{prices: make(map[string]float64)}
}

// recordAndDiff records the new price for sku. Returns (previousPrice, changed).
// On first sighting the item is not considered a price change.
func (s *priceStore) recordAndDiff(sku string, newPrice float64) (prev float64, changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, known := s.prices[sku]
	s.prices[sku] = newPrice
	if !known {
		return 0, false // first sighting — not a change
	}
	return prev, prev != newPrice
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// -------------------------------------------------------------------------
	// 1. Identity and fetcher setup.
	// -------------------------------------------------------------------------
	profile := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
		identity.WithLocale("en-US", "en-US", "en"),
		identity.WithTimezone("America/New_York"),
	)

	fetcher := fetch.NewStealth(
		fetch.WithIdentity(profile),
		fetch.WithTimeout(30*time.Second),
	)
	defer fetcher.Close()

	// -------------------------------------------------------------------------
	// 2. Price state tracker.
	// -------------------------------------------------------------------------
	store := newPriceStore()

	// -------------------------------------------------------------------------
	// 3. Webhook writer for price alerts.
	//
	// Items are batched into groups of 10 and sent to the webhook endpoint.
	// The writer retries up to 3 times on transient failures before giving up.
	// WithAuth adds a Bearer token header for authentication.
	//
	// Note: webhookURL is a placeholder and won't actually connect in this demo.
	// The example will log an error on flush but otherwise continue cleanly.
	// -------------------------------------------------------------------------
	alertWriter := export.NewWebhook(
		webhookURL,
		export.WithBatchSize(10),
		export.WithRetry(3),
		export.WithAuth("Bearer", os.Getenv("WEBHOOK_SECRET")), // read secret from env
	)
	defer alertWriter.Close()

	// -------------------------------------------------------------------------
	// 4. Build the pipeline.
	//
	// Stages:
	//   Validate    — drop items missing required fields (sku, price, title)
	//   Clean       — trim whitespace and normalise price strings to float64
	//   ItemDedup   — drop items we have already seen in this run (by SKU)
	//   Transform   — compute price changes by comparing to the stored price
	// -------------------------------------------------------------------------
	priceTransform := &pipeline.Transform{
		Fn: func(item *foxhound.Item) (*foxhound.Item, error) {
			skuVal, ok := item.Get("sku")
			if !ok {
				return item, nil
			}
			sku := fmt.Sprintf("%v", skuVal)

			// Parse the cleaned price value (may be float64 after Clean).
			var newPrice float64
			priceVal, _ := item.Get("price")
			switch v := priceVal.(type) {
			case float64:
				newPrice = v
			case string:
				newPrice, _ = strconv.ParseFloat(strings.TrimPrefix(v, "$"), 64)
			}

			prev, changed := store.recordAndDiff(sku, newPrice)
			item.Set("price_current", newPrice)
			item.Set("price_previous", prev)
			item.Set("price_changed", changed)

			if changed {
				direction := "decreased"
				if newPrice > prev {
					direction = "increased"
				}
				item.Set("alert_message", fmt.Sprintf(
					"Price %s: %s was %.2f, now %.2f",
					direction, sku, prev, newPrice,
				))
				slog.Info("price change detected",
					"sku", sku,
					"prev", prev,
					"new", newPrice,
					"direction", direction,
				)
			}

			// Only forward items with price changes to the webhook writer.
			// Items without changes are dropped here to avoid alert fatigue.
			if !changed {
				return nil, nil // drop: no change to report
			}
			return item, nil
		},
	}

	pipelineChain := pipeline.NewChain(
		&pipeline.Validate{Required: []string{"sku", "title", "price"}},
		&pipeline.Clean{TrimWhitespace: true, NormalizePrice: true},
		pipeline.NewItemDedup("sku"), // de-duplicate within a single scraping run
		priceTransform,
	)

	// -------------------------------------------------------------------------
	// 5. Processor: parse products and apply the pipeline.
	// -------------------------------------------------------------------------
	processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
		if resp.StatusCode != 200 {
			return &foxhound.Result{}, nil
		}

		doc, err := parse.NewDocument(resp)
		if err != nil {
			return nil, fmt.Errorf("parsing HTML: %w", err)
		}

		var items []*foxhound.Item
		var nextJobs []*foxhound.Job

		doc.Each("article.product_pod", func(i int, s *goquery.Selection) {
			item := foxhound.NewItem()
			item.URL = resp.URL

			title, _ := s.Find("h3 a").Attr("title")
			price := s.Find("p.price_color").Text()
			// Use the detail page href as a stable SKU.
			sku, _ := s.Find("h3 a").Attr("href")

			item.Set("title", title)
			item.Set("price", price)
			item.Set("sku", sku)
			item.Set("monitored_at", time.Now().UTC().Format(time.RFC3339))

			processed, pipeErr := pipelineChain.Process(ctx, item)
			if pipeErr != nil {
				slog.Warn("pipeline error", "err", pipeErr)
				return
			}
			if processed != nil {
				items = append(items, processed)
			}
		})

		// Follow pagination so all products are checked each run.
		if nextHref := doc.Attr("li.next a", "href"); nextHref != "" {
			nextJobs = append(nextJobs, &foxhound.Job{
				ID:        fmt.Sprintf("monitor-page-%d", resp.Job.Depth+1),
				URL:       resolveURL(resp.URL, nextHref),
				FetchMode: foxhound.FetchStatic,
				Priority:  foxhound.PriorityNormal,
				Depth:     resp.Job.Depth + 1,
			})
		}

		return &foxhound.Result{Items: items, Jobs: nextJobs}, nil
	})

	// -------------------------------------------------------------------------
	// 6. Periodic re-scrape loop.
	//
	// The monitor runs a fresh Hunt every scrapeInterval. Each Hunt uses a new
	// queue and a new ItemDedup stage so stale dedup state from previous runs
	// does not suppress legitimate re-reports.
	//
	// A production implementation would persist price state in Redis or
	// PostgreSQL and schedule runs with a cron job or Temporal workflow.
	// -------------------------------------------------------------------------

	// Capture OS signals for clean shutdown.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	runScrape := func() {
		slog.Info("monitor: starting scrape run")

		q := queue.NewMemoryQueue()
		defer q.Close()

		h := engine.NewHunt(engine.HuntConfig{
			Name:      "price-monitor",
			Domain:    "books.toscrape.com",
			Walkers:   2,
			Fetcher:   fetcher,
			Processor: processor,
			Queue:     q,
			Writers:   []foxhound.Writer{alertWriter},
			Seeds: []*foxhound.Job{
				{
					ID:        "monitor-seed",
					URL:       "http://books.toscrape.com/",
					FetchMode: foxhound.FetchStatic,
					Priority:  foxhound.PriorityHigh,
				},
			},
		})

		runCtx, runCancel := context.WithTimeout(ctx, 5*time.Minute)
		defer runCancel()

		if err := h.Run(runCtx); err != nil {
			// Log but do not fatal — the monitor loop will retry next interval.
			slog.Error("scrape run failed", "err", err)
			return
		}

		slog.Info("monitor: scrape run complete", "stats", h.Stats().Summary())
	}

	// Run once immediately on startup, then on the ticker.
	runScrape()

	ticker := time.NewTicker(scrapeInterval)
	defer ticker.Stop()

	slog.Info("monitor: entering watch loop", "interval", scrapeInterval)
	for {
		select {
		case <-ctx.Done():
			slog.Info("monitor: shutting down")
			if err := alertWriter.Flush(context.Background()); err != nil {
				log.Printf("flush on shutdown failed: %v", err)
			}
			return
		case <-ticker.C:
			runScrape()
		}
	}
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
