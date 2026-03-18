// Scrape Target 1: Google SERP — "wisata alam jawa timur"
//
// Tests Foxhound's StealthFetcher against Google Search with Indonesian locale.
// Google is moderately protected — may return CAPTCHA, redirect, or 429.
// The scraper counts blocks honestly and reports benchmark metrics at exit.
//
// Run:
//
//	go run ./tests/scrape_targets/google_serp/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
	"github.com/sadewadee/foxhound/parse"

	"github.com/PuerkitoBio/goquery"
)

const (
	targetURL    = "https://www.google.com/search?q=wisata+alam+jawa+timur&hl=id&gl=id&num=20"
	resultsDir   = "tests/results"
	outputFile   = "tests/results/google_serp.json"
	benchFile    = "tests/results/google_serp_benchmark.json"
	targetName   = "Google SERP: wisata alam jawa timur"
)

// OrganicResult holds one parsed search result entry.
type OrganicResult struct {
	Position int    `json:"position"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Snippet  string `json:"snippet"`
}

// Benchmark holds the timing and success metrics for the run.
type Benchmark struct {
	Target         string  `json:"target"`
	TargetURL      string  `json:"target_url"`
	TotalRequests  int     `json:"total_requests"`
	Successful     int     `json:"successful"`
	Blocked        int     `json:"blocked"`
	Items          int     `json:"items"`
	DurationMS     int64   `json:"duration_ms"`
	AvgLatencyMS   int64   `json:"avg_latency_ms"`
	ThroughputIPS  float64 `json:"throughput_items_per_sec"`
	BytesReceived  int64   `json:"bytes_received"`
	BlockAvoidance float64 `json:"block_avoidance_pct"`
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// ------------------------------------------------------------------
	// 1. Generate a Firefox identity with Indonesian locale to match the
	//    target language. Proxy geo and locale must be consistent.
	// ------------------------------------------------------------------
	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
		identity.WithLocale("id-ID", "id-ID", "id", "en"),
		identity.WithTimezone("Asia/Jakarta"),
		identity.WithGeo(-7.25, 112.75), // Surabaya, East Java
	)
	slog.Info("identity generated", "ua", prof.UA, "locale", prof.Locale)

	// ------------------------------------------------------------------
	// 2. Build a StealthFetcher with the generated identity.
	// ------------------------------------------------------------------
	fetcher := fetch.NewStealth(
		fetch.WithIdentity(prof),
		fetch.WithTimeout(30*time.Second),
	)
	defer fetcher.Close()

	var bench Benchmark
	bench.Target = targetName
	bench.TargetURL = targetURL

	runStart := time.Now()

	// ------------------------------------------------------------------
	// 3. Single fetch — Google search page.
	// ------------------------------------------------------------------
	job := &foxhound.Job{
		ID:        "google-serp-wisata-alam-jawa-timur",
		URL:       targetURL,
		Method:    http.MethodGet,
		FetchMode: foxhound.FetchStatic,
		Priority:  foxhound.PriorityNormal,
		Headers: http.Header{
			"Accept-Language": []string{"id-ID,id;q=0.9,en;q=0.7"},
			"Referer":         []string{"https://www.google.com/"},
		},
		CreatedAt: time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	slog.Info("fetching", "url", targetURL)
	reqStart := time.Now()
	resp, err := fetcher.Fetch(ctx, job)
	reqDuration := time.Since(reqStart)
	bench.TotalRequests++

	var results []OrganicResult

	if err != nil {
		slog.Error("fetch error", "err", err, "duration_ms", reqDuration.Milliseconds())
		bench.Blocked++
	} else {
		bench.BytesReceived += int64(len(resp.Body))
		slog.Info("response received",
			"status", resp.StatusCode,
			"bytes", len(resp.Body),
			"duration_ms", reqDuration.Milliseconds(),
		)

		// Google blocks often manifest as 429, 302-to-consent, or a page with
		// no organic results. Treat anything other than 200 as a block.
		if resp.StatusCode != http.StatusOK {
			slog.Warn("non-200 response — counted as block",
				"status", resp.StatusCode,
				"url", resp.URL,
			)
			bench.Blocked++
			// Log a snippet of the body to help diagnose what Google returned.
			snippet := resp.Body
			if len(snippet) > 512 {
				snippet = snippet[:512]
			}
			slog.Debug("body snippet", "body", string(snippet))
		} else {
			bench.Successful++
			results = parseGoogleSERP(resp, bench.TotalRequests)
			bench.Items = len(results)
		}
	}

	// ------------------------------------------------------------------
	// 4. Human-like pause before any follow-up work (none here, but
	//    the delay models what a real session would do).
	// ------------------------------------------------------------------
	time.Sleep(1500 * time.Millisecond)

	// ------------------------------------------------------------------
	// 5. Compute benchmark metrics.
	// ------------------------------------------------------------------
	bench.DurationMS = time.Since(runStart).Milliseconds()
	if bench.TotalRequests > 0 {
		bench.AvgLatencyMS = bench.DurationMS / int64(bench.TotalRequests)
	}
	durationSec := float64(bench.DurationMS) / 1000.0
	if durationSec > 0 && bench.Items > 0 {
		bench.ThroughputIPS = float64(bench.Items) / durationSec
	}
	if bench.TotalRequests > 0 {
		bench.BlockAvoidance = float64(bench.Successful) / float64(bench.TotalRequests) * 100
	}

	// ------------------------------------------------------------------
	// 6. Write output files.
	// ------------------------------------------------------------------
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		slog.Error("cannot create results dir", "err", err)
	}

	writeJSON(outputFile, results)
	writeJSON(benchFile, bench)

	// ------------------------------------------------------------------
	// 7. Print benchmark table.
	// ------------------------------------------------------------------
	printBenchmark(bench)
}

// parseGoogleSERP extracts organic search results from a Google SERP response.
// Google's HTML is not stable; we try multiple known selector patterns and fall
// back gracefully, logging a body snippet when nothing matches.
func parseGoogleSERP(resp *foxhound.Response, _ int) []OrganicResult {
	doc, err := parse.NewDocument(resp)
	if err != nil {
		slog.Error("HTML parse error", "err", err)
		return nil
	}

	var results []OrganicResult
	position := 0

	// Pattern A: div.g containers (classic layout, still used for many locales).
	doc.Each("div.g", func(_ int, s *goquery.Selection) {
		title := strings.TrimSpace(s.Find("h3").First().Text())
		if title == "" {
			return
		}
		href, _ := s.Find("a[href]").First().Attr("href")
		href = cleanGoogleHref(href)
		snippet := strings.TrimSpace(s.Find("div.VwiC3b, span.st, div[data-snc]").First().Text())
		position++
		results = append(results, OrganicResult{
			Position: position,
			Title:    title,
			URL:      href,
			Snippet:  snippet,
		})
	})

	// Pattern B: data-hveid containers (newer Google layouts).
	if len(results) == 0 {
		doc.Each("div[data-hveid]", func(_ int, s *goquery.Selection) {
			title := strings.TrimSpace(s.Find("h3").First().Text())
			if title == "" {
				return
			}
			href, _ := s.Find("a[href]").First().Attr("href")
			href = cleanGoogleHref(href)
			snippet := strings.TrimSpace(s.Find("div.VwiC3b, div[data-snc]").First().Text())
			position++
			results = append(results, OrganicResult{
				Position: position,
				Title:    title,
				URL:      href,
				Snippet:  snippet,
			})
		})
	}

	// Pattern C: any h3 inside a search-results wrapper as last resort.
	if len(results) == 0 {
		doc.Each("#search h3", func(_ int, s *goquery.Selection) {
			title := strings.TrimSpace(s.Text())
			if title == "" {
				return
			}
			href, _ := s.Parent().Find("a[href]").First().Attr("href")
			href = cleanGoogleHref(href)
			position++
			results = append(results, OrganicResult{
				Position: position,
				Title:    title,
				URL:      href,
			})
		})
	}

	if len(results) == 0 {
		// Log a body snippet so the caller can see what Google actually returned.
		snippet := resp.Body
		if len(snippet) > 1024 {
			snippet = snippet[:1024]
		}
		slog.Warn("no organic results parsed — possible block or layout change",
			"body_snippet", string(snippet),
		)
	} else {
		slog.Info("parsed organic results", "count", len(results))
		for i, r := range results {
			if i >= 5 {
				break
			}
			slog.Info("result", "pos", r.Position, "title", r.Title, "url", r.URL)
		}
	}

	return results
}

// cleanGoogleHref strips the /url?q= redirect wrapper Google adds to hrefs.
func cleanGoogleHref(href string) string {
	if strings.HasPrefix(href, "/url?q=") {
		href = strings.TrimPrefix(href, "/url?q=")
		if idx := strings.Index(href, "&"); idx > 0 {
			href = href[:idx]
		}
	}
	return href
}

// writeJSON marshals v to a JSON file at path.
func writeJSON(path string, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		slog.Error("json marshal error", "path", path, "err", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		slog.Error("write file error", "path", path, "err", err)
		return
	}
	slog.Info("results written", "path", path)
}

func printBenchmark(b Benchmark) {
	total := b.TotalRequests
	if total == 0 {
		total = 1
	}
	dur := time.Duration(b.DurationMS) * time.Millisecond
	avg := time.Duration(b.AvgLatencyMS) * time.Millisecond

	fmt.Println("═══════════════════════════════════════════")
	fmt.Printf("BENCHMARK: %s\n", b.Target)
	fmt.Println("═══════════════════════════════════════════")
	fmt.Printf("Target:          %s\n", b.TargetURL)
	fmt.Printf("Total Requests:  %d\n", b.TotalRequests)
	fmt.Printf("Successful:      %d (%.1f%%)\n", b.Successful, float64(b.Successful)/float64(total)*100)
	fmt.Printf("Blocked/Error:   %d (%.1f%%)\n", b.Blocked, float64(b.Blocked)/float64(total)*100)
	fmt.Printf("Items Scraped:   %d\n", b.Items)
	fmt.Printf("Total Duration:  %s\n", dur.Round(time.Millisecond))
	fmt.Printf("Avg Latency:     %s/request\n", avg.Round(time.Millisecond))
	fmt.Printf("Throughput:      %.2f items/sec\n", b.ThroughputIPS)
	fmt.Printf("Bytes Received:  %d\n", b.BytesReceived)
	fmt.Printf("Block Avoidance: %.1f%%\n", b.BlockAvoidance)
	fmt.Println("═══════════════════════════════════════════")
}
