// Example: E-commerce product scraper
//
// Demonstrates how to scrape product listings from books.toscrape.com, a
// public scraping sandbox. Covers:
//
//   - Generating a consistent identity profile (UA + headers match)
//   - Creating a StealthFetcher with that identity
//   - Seeding an in-memory queue with the first catalogue page
//   - Writing a ProcessorFunc that parses product name, price, and URL with
//     goquery via parse.Document
//   - Building a pipeline: Validate required fields → Clean whitespace → CSV export
//   - Creating and running a Hunt that processes all seed jobs
//   - Printing a stats summary on completion
//
// Run:
//
//	go run ./examples/ecommerce/
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/engine"
	"github.com/foxhound-scraper/foxhound/fetch"
	"github.com/foxhound-scraper/foxhound/identity"
	"github.com/PuerkitoBio/goquery"
	"github.com/foxhound-scraper/foxhound/parse"
	"github.com/foxhound-scraper/foxhound/pipeline"
	"github.com/foxhound-scraper/foxhound/pipeline/export"
	"github.com/foxhound-scraper/foxhound/queue"
)

// baseURL is a public scraping sandbox — safe to scrape in examples.
const baseURL = "http://books.toscrape.com/"

// outputFile is the CSV file written when the hunt completes.
const outputFile = "books.csv"

func main() {
	// Configure structured logging to stderr so CSV output is not polluted.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// -------------------------------------------------------------------------
	// 1. Generate a consistent identity profile.
	//
	// All attributes — User-Agent, TLS fingerprint, header order, OS, locale —
	// are derived from the same profile so they are internally consistent.
	// Sending a mismatched UA + TLS fingerprint is the fastest way to trigger
	// anti-bot systems.
	// -------------------------------------------------------------------------
	profile := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
		identity.WithLocale("en-US", "en-US", "en"),
		identity.WithTimezone("America/New_York"),
	)
	slog.Info("identity generated",
		"ua", profile.UA,
		"os", profile.OS,
		"locale", profile.Locale,
	)

	// -------------------------------------------------------------------------
	// 2. Create a StealthFetcher using the generated identity.
	//
	// StealthFetcher applies correct browser header ordering so the HTTP
	// fingerprint matches the declared User-Agent. In Phase 2 the underlying
	// client will be replaced with azuretls for real JA3/JA4 TLS impersonation.
	// -------------------------------------------------------------------------
	fetcher := fetch.NewStealth(
		fetch.WithIdentity(profile),
		fetch.WithTimeout(30*time.Second),
	)
	defer fetcher.Close()

	// -------------------------------------------------------------------------
	// 3. Create an in-memory queue and seed it with the catalogue homepage.
	// -------------------------------------------------------------------------
	q := queue.NewMemoryQueue()
	defer q.Close()

	// -------------------------------------------------------------------------
	// 4. Create a CSV writer.
	//
	// Explicit headers fix the column order in the output file regardless of
	// the order fields are added to items in the processor.
	// -------------------------------------------------------------------------
	csvWriter, err := export.NewCSV(outputFile, "title", "price", "rating", "url")
	if err != nil {
		log.Fatalf("failed to create CSV writer: %v", err)
	}
	defer csvWriter.Close()

	// -------------------------------------------------------------------------
	// 5. Build the pipeline chain.
	//
	// Stages run left-to-right. An item is dropped (and not written) if any
	// stage returns nil.
	//
	//   Validate  — drop items missing required fields (title, price, url)
	//   Clean     — trim whitespace from all string fields
	// -------------------------------------------------------------------------
	pipelineChain := pipeline.NewChain(
		&pipeline.Validate{Required: []string{"title", "price", "url"}},
		&pipeline.Clean{TrimWhitespace: true},
	)

	// -------------------------------------------------------------------------
	// 6. Define the processor.
	//
	// ProcessorFunc is an adapter so you do not need a named type for simple
	// single-site scrapers. For a multi-domain crawler, implement the full
	// Processor interface and dispatch on resp.Job.Domain.
	// -------------------------------------------------------------------------
	processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
		if resp.StatusCode != 200 {
			slog.Warn("unexpected status", "status", resp.StatusCode, "url", resp.URL)
			return &foxhound.Result{}, nil
		}

		doc, err := parse.NewDocument(resp)
		if err != nil {
			return nil, fmt.Errorf("parsing HTML from %s: %w", resp.URL, err)
		}

		var items []*foxhound.Item
		var nextJobs []*foxhound.Job

		// Each book is wrapped in an <article class="product_pod"> element.
		doc.Each("article.product_pod", func(i int, s *goquery.Selection) {
			item := foxhound.NewItem()
			item.URL = resp.URL

			// Title is in the <a title="…"> attribute inside the <h3> heading.
			title, _ := s.Find("h3 a").Attr("title")
			item.Set("title", title)

			// Price text is in the <p class="price_color"> element.
			price := strings.TrimSpace(s.Find("p.price_color").Text())
			item.Set("price", price)

			// Rating is encoded as a word in the CSS class of <p class="star-rating One|Two|…">.
			ratingClass, _ := s.Find("p.star-rating").Attr("class")
			rating := strings.TrimPrefix(ratingClass, "star-rating ")
			item.Set("rating", rating)

			// Absolute URL to the book detail page.
			relHref, _ := s.Find("h3 a").Attr("href")
			item.Set("url", resolveURL(baseURL, relHref))

			// Run pipeline stages — nil means the item is dropped.
			processed, pipeErr := pipelineChain.Process(ctx, item)
			if pipeErr != nil {
				slog.Warn("pipeline error", "err", pipeErr, "title", title)
				return
			}
			if processed != nil {
				items = append(items, processed)
			}
		})

		// Follow the "next" pagination link so every catalogue page is scraped.
		nextHref := doc.Attr("li.next a", "href")
		if nextHref != "" {
			nextJobs = append(nextJobs, &foxhound.Job{
				ID:        fmt.Sprintf("page-%s", nextHref),
				URL:       resolveURL(resp.URL, nextHref),
				FetchMode: foxhound.FetchStatic,
				Priority:  foxhound.PriorityNormal,
			})
		}

		slog.Info("page scraped",
			"url", resp.URL,
			"items", len(items),
			"next_pages", len(nextJobs),
		)

		return &foxhound.Result{Items: items, Jobs: nextJobs}, nil
	})

	// -------------------------------------------------------------------------
	// 7. Create and run the Hunt.
	// -------------------------------------------------------------------------
	h := engine.NewHunt(engine.HuntConfig{
		Name:      "books-toscrape",
		Domain:    "books.toscrape.com",
		Walkers:   2, // two concurrent virtual users
		Fetcher:   fetcher,
		Processor: processor,
		Queue:     q,
		Writers:   []foxhound.Writer{csvWriter},
		Seeds: []*foxhound.Job{
			{
				ID:        "seed-catalogue",
				URL:       baseURL,
				FetchMode: foxhound.FetchStatic,
				Priority:  foxhound.PriorityHigh,
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	slog.Info("starting hunt", "target", baseURL, "output", outputFile)
	if err := h.Run(ctx); err != nil {
		log.Fatalf("hunt failed: %v", err)
	}

	// -------------------------------------------------------------------------
	// 8. Print stats summary.
	// -------------------------------------------------------------------------
	fmt.Printf("\nHunt complete: %s\n", h.Stats().Summary())
	fmt.Printf("Output written to: %s\n", outputFile)
}

// resolveURL resolves href relative to base, following the same rules as a
// browser. If href is already absolute it is returned unchanged.
func resolveURL(base, href string) string {
	// Simple approach: if href starts with "http" it is absolute.
	if strings.HasPrefix(href, "http") {
		return href
	}
	// Strip any trailing path component from base and join.
	// e.g. base="http://books.toscrape.com/catalogue/page-2.html" + href="../page-3.html"
	// A production implementation would use url.ResolveReference.
	if strings.HasSuffix(base, "/") {
		return base + href
	}
	// Remove the last path segment from base.
	idx := strings.LastIndex(base, "/")
	if idx < 0 {
		return href
	}
	return base[:idx+1] + href
}
