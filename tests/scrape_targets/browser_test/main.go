//go:build playwright

// browser_test runs all 4 scrape targets using Camoufox browser mode
// to render JavaScript and extract actual data.
//
// Build: go build -tags playwright ./tests/scrape_targets/browser_test/
// Run:   go run -tags playwright ./tests/scrape_targets/browser_test/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
	"github.com/sadewadee/foxhound/parse"
)

type ScrapedItem struct {
	Title    string `json:"title,omitempty"`
	URL      string `json:"url,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
	Price    string `json:"price,omitempty"`
	Rating   string `json:"rating,omitempty"`
	Address  string `json:"address,omitempty"`
	Supplier string `json:"supplier,omitempty"`
	Location string `json:"location,omitempty"`
	Type     string `json:"type,omitempty"`
}

type Benchmark struct {
	Target         string  `json:"target"`
	URL            string  `json:"url"`
	TotalRequests  int     `json:"total_requests"`
	Successful     int     `json:"successful"`
	Blocked        int     `json:"blocked"`
	Items          int     `json:"items"`
	DurationMs     int64   `json:"duration_ms"`
	AvgLatencyMs   int64   `json:"avg_latency_ms"`
	BytesReceived  int64   `json:"bytes_received"`
	BlockAvoidance float64 `json:"block_avoidance_pct"`
	Mode           string  `json:"mode"`
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	// Generate identity
	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
		identity.WithLocale("en-US", "en-US", "en"),
		identity.WithTimezone("America/New_York"),
	)

	slog.Info("launching Camoufox browser (auto-installs if needed)...")

	camoufox, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBlockImages(true),
		fetch.WithHeadless("true"),
		fetch.WithBrowserTimeout(45*time.Second),
	)
	if err != nil {
		slog.Error("failed to launch Camoufox", "err", err)
		os.Exit(1)
	}
	defer camoufox.Close()

	slog.Info("Camoufox ready", "ua", prof.UA[:60]+"...")

	// Run all 4 targets
	targets := []struct {
		name string
		url  string
		fn   func(ctx context.Context, f foxhound.Fetcher, url string) ([]ScrapedItem, *Benchmark)
	}{
		{"google_serp", "https://www.google.com/search?q=wisata+alam+jawa+timur&hl=id&gl=id&num=20", scrapeGoogleSERP},
		{"google_maps", "https://www.google.com/maps/search/villa+di+bali/", scrapeGoogleMaps},
		{"alibaba", "https://www.alibaba.com/trade/search?SearchText=yoga+mat&page=1", scrapeAlibaba},
		{"yoga_alliance", "https://app.yogaalliance.org/directoryregistrants?type=School", scrapeYogaAlliance},
	}

	var allBenchmarks []Benchmark

	for i, t := range targets {
		if i > 0 {
			slog.Info("waiting 3s between targets...")
			time.Sleep(3 * time.Second)
		}

		fmt.Printf("\n{'='*60}\n")
		slog.Info("starting target", "name", t.name, "url", t.url)

		items, bench := t.fn(context.Background(), camoufox, t.url)

		// Save items
		itemPath := fmt.Sprintf("tests/results/%s_browser.json", t.name)
		saveJSON(itemPath, items)

		// Save benchmark
		benchPath := fmt.Sprintf("tests/results/%s_browser_benchmark.json", t.name)
		saveJSON(benchPath, bench)

		allBenchmarks = append(allBenchmarks, *bench)
		printBenchmark(bench)
	}

	// Print combined summary
	fmt.Println("\n\n══════════════════════════════════════════════════════════════")
	fmt.Println("COMBINED BENCHMARK SUMMARY (Camoufox Browser Mode)")
	fmt.Println("══════════════════════════════════════════════════════════════")
	totalReqs, totalSuccess, totalBlocked, totalItems := 0, 0, 0, 0
	var totalBytes int64
	for _, b := range allBenchmarks {
		totalReqs += b.TotalRequests
		totalSuccess += b.Successful
		totalBlocked += b.Blocked
		totalItems += b.Items
		totalBytes += b.BytesReceived
		fmt.Printf("%-20s | Reqs: %d | OK: %d | Blocked: %d | Items: %d | Avoidance: %.0f%%\n",
			b.Target, b.TotalRequests, b.Successful, b.Blocked, b.Items, b.BlockAvoidance)
	}
	fmt.Println("──────────────────────────────────────────────────────────────")
	avoidance := float64(totalSuccess) / float64(max(totalReqs, 1)) * 100
	fmt.Printf("%-20s | Reqs: %d | OK: %d | Blocked: %d | Items: %d | Avoidance: %.0f%%\n",
		"TOTAL", totalReqs, totalSuccess, totalBlocked, totalItems, avoidance)
	fmt.Println("══════════════════════════════════════════════════════════════")

	// Save combined
	saveJSON("tests/results/browser_combined_benchmark.json", allBenchmarks)
}

func scrapeGoogleSERP(ctx context.Context, f foxhound.Fetcher, url string) ([]ScrapedItem, *Benchmark) {
	bench := &Benchmark{Target: "Google SERP", URL: url, Mode: "camoufox"}
	start := time.Now()

	resp := doFetch(ctx, f, url, bench)
	if resp == nil {
		bench.DurationMs = time.Since(start).Milliseconds()
		return nil, bench
	}

	var items []ScrapedItem
	doc, err := parse.NewDocument(resp)
	if err == nil {
		// Google SERP selectors for rendered page
		doc.Each("div.g", func(_ int, s *goquery.Selection) {
			title := strings.TrimSpace(s.Find("h3").Text())
			href, _ := s.Find("a").Attr("href")
			snippet := strings.TrimSpace(s.Find("div.VwiC3b, span.aCOpRe").Text())
			if title != "" {
				items = append(items, ScrapedItem{Title: title, URL: href, Snippet: snippet})
			}
		})
		// Fallback: broader selector
		if len(items) == 0 {
			doc.Each("#search h3", func(_ int, s *goquery.Selection) {
				title := strings.TrimSpace(s.Text())
				href, _ := s.Parent().Attr("href")
				if title != "" {
					items = append(items, ScrapedItem{Title: title, URL: href})
				}
			})
		}
	}

	bench.Items = len(items)
	bench.DurationMs = time.Since(start).Milliseconds()
	bench.AvgLatencyMs = bench.DurationMs / int64(max(bench.TotalRequests, 1))
	bench.BlockAvoidance = float64(bench.Successful) / float64(max(bench.TotalRequests, 1)) * 100
	slog.Info("SERP parsed", "items", len(items))
	return items, bench
}

func scrapeGoogleMaps(ctx context.Context, f foxhound.Fetcher, url string) ([]ScrapedItem, *Benchmark) {
	bench := &Benchmark{Target: "Google Maps", URL: url, Mode: "camoufox"}
	start := time.Now()

	resp := doFetch(ctx, f, url, bench)
	if resp == nil {
		bench.DurationMs = time.Since(start).Milliseconds()
		return nil, bench
	}

	var items []ScrapedItem
	doc, err := parse.NewDocument(resp)
	if err == nil {
		// Maps renders place cards with various selectors
		doc.Each("div.Nv2PK, div[role='article'], a[href*='/maps/place/']", func(_ int, s *goquery.Selection) {
			name := strings.TrimSpace(s.Find("div.qBF1Pd, span.fontHeadlineSmall").Text())
			rating := strings.TrimSpace(s.Find("span.MW4etd, span[role='img']").Text())
			addr := strings.TrimSpace(s.Find("div.W4Efsd span, span.W4Efsd").Text())
			if name == "" {
				name = strings.TrimSpace(s.Find("div.fontBodyMedium").First().Text())
			}
			if name != "" && len(items) < 15 {
				items = append(items, ScrapedItem{Title: name, Rating: rating, Address: addr})
			}
		})
	}

	bench.Items = len(items)
	bench.DurationMs = time.Since(start).Milliseconds()
	bench.AvgLatencyMs = bench.DurationMs / int64(max(bench.TotalRequests, 1))
	bench.BlockAvoidance = float64(bench.Successful) / float64(max(bench.TotalRequests, 1)) * 100
	slog.Info("Maps parsed", "items", len(items))
	return items, bench
}

func scrapeAlibaba(ctx context.Context, f foxhound.Fetcher, url string) ([]ScrapedItem, *Benchmark) {
	bench := &Benchmark{Target: "Alibaba", URL: url, Mode: "camoufox"}
	start := time.Now()

	resp := doFetch(ctx, f, url, bench)
	if resp == nil {
		bench.DurationMs = time.Since(start).Milliseconds()
		return nil, bench
	}

	var items []ScrapedItem
	doc, err := parse.NewDocument(resp)
	if err == nil {
		doc.Each(".organic-list .list-no-v2-outter, .J-offer-wrapper, div[data-content='product'], div.fy23-search-card", func(_ int, s *goquery.Selection) {
			title := strings.TrimSpace(s.Find("h2, h3, .elements-title-normal__content, a.elements-title-normal").Text())
			price := strings.TrimSpace(s.Find(".elements-offer-price-normal__price, span.price, .search-card-e-price-main").Text())
			supplier := strings.TrimSpace(s.Find(".seller-name, .search-card-e-company, span.company-name").Text())
			if title != "" && len(items) < 10 {
				items = append(items, ScrapedItem{Title: title, Price: price, Supplier: supplier})
			}
		})
	}

	bench.Items = len(items)
	bench.DurationMs = time.Since(start).Milliseconds()
	bench.AvgLatencyMs = bench.DurationMs / int64(max(bench.TotalRequests, 1))
	bench.BlockAvoidance = float64(bench.Successful) / float64(max(bench.TotalRequests, 1)) * 100
	slog.Info("Alibaba parsed", "items", len(items))
	return items, bench
}

func scrapeYogaAlliance(ctx context.Context, f foxhound.Fetcher, url string) ([]ScrapedItem, *Benchmark) {
	bench := &Benchmark{Target: "Yoga Alliance", URL: url, Mode: "camoufox"}
	start := time.Now()

	resp := doFetch(ctx, f, url, bench)
	if resp == nil {
		bench.DurationMs = time.Since(start).Milliseconds()
		return nil, bench
	}

	var items []ScrapedItem
	doc, err := parse.NewDocument(resp)
	if err == nil {
		// Try multiple selectors for the directory cards
		doc.Each("div.card, div[class*='registrant'], div[class*='school'], tr, li[class*='result']", func(_ int, s *goquery.Selection) {
			name := strings.TrimSpace(s.Find("h3, h4, h5, a.card-title, td:first-child, strong").First().Text())
			loc := strings.TrimSpace(s.Find("span[class*='location'], div[class*='address'], td:nth-child(2), p.card-text").First().Text())
			stype := strings.TrimSpace(s.Find("span[class*='type'], span.badge, td:nth-child(3)").First().Text())
			if name != "" && len(name) > 3 && len(items) < 10 {
				items = append(items, ScrapedItem{Title: name, Location: loc, Type: stype})
			}
		})
	}

	bench.Items = len(items)
	bench.DurationMs = time.Since(start).Milliseconds()
	bench.AvgLatencyMs = bench.DurationMs / int64(max(bench.TotalRequests, 1))
	bench.BlockAvoidance = float64(bench.Successful) / float64(max(bench.TotalRequests, 1)) * 100
	slog.Info("Yoga Alliance parsed", "items", len(items))
	return items, bench
}

// doFetch performs a single fetch and updates benchmark counters.
func doFetch(ctx context.Context, f foxhound.Fetcher, url string, bench *Benchmark) *foxhound.Response {
	job := &foxhound.Job{
		ID:        fmt.Sprintf("browser-%s", bench.Target),
		URL:       url,
		Method:    "GET",
		FetchMode: foxhound.FetchBrowser,
		Domain:    extractDomain(url),
	}

	resp, err := f.Fetch(ctx, job)
	bench.TotalRequests++

	if err != nil {
		bench.Blocked++
		slog.Error("fetch error", "url", url, "err", err)
		return nil
	}

	bench.BytesReceived += int64(len(resp.Body))
	if resp.StatusCode >= 400 {
		bench.Blocked++
		slog.Warn("blocked/error response", "url", url, "status", resp.StatusCode)
	} else {
		bench.Successful++
		slog.Info("fetch OK", "url", url, "status", resp.StatusCode, "bytes", len(resp.Body), "duration", resp.Duration)
	}

	return resp
}

func extractDomain(rawURL string) string {
	parts := strings.SplitN(rawURL, "//", 2)
	if len(parts) < 2 {
		return rawURL
	}
	host := strings.SplitN(parts[1], "/", 2)[0]
	return host
}

func printBenchmark(b *Benchmark) {
	fmt.Println("═══════════════════════════════════════════")
	fmt.Printf("BENCHMARK: %s [%s]\n", b.Target, b.Mode)
	fmt.Println("═══════════════════════════════════════════")
	fmt.Printf("Target:          %s\n", b.URL)
	fmt.Printf("Total Requests:  %d\n", b.TotalRequests)
	fmt.Printf("Successful:      %d (%.1f%%)\n", b.Successful, float64(b.Successful)/float64(max(b.TotalRequests, 1))*100)
	fmt.Printf("Blocked/Error:   %d (%.1f%%)\n", b.Blocked, float64(b.Blocked)/float64(max(b.TotalRequests, 1))*100)
	fmt.Printf("Items Scraped:   %d\n", b.Items)
	fmt.Printf("Total Duration:  %dms\n", b.DurationMs)
	fmt.Printf("Avg Latency:     %dms/request\n", b.AvgLatencyMs)
	if b.DurationMs > 0 {
		fmt.Printf("Throughput:      %.2f items/sec\n", float64(b.Items)/float64(b.DurationMs)*1000)
	}
	fmt.Printf("Bytes Received:  %d\n", b.BytesReceived)
	fmt.Printf("Block Avoidance: %.1f%%\n", b.BlockAvoidance)
	fmt.Println("═══════════════════════════════════════════")
}

func saveJSON(path string, v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	os.WriteFile(path, data, 0644)
	slog.Info("saved", "path", path)
}
