//go:build playwright

// maps_benchmark — Google Maps scraping performance test
// Measures: requests/min, items/min, items/hour projection, latency, success rate
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
	"github.com/sadewadee/foxhound/parse"
)

const proxyURL = "http://REDACTED_USER:REDACTED_PASS@REDACTED_HOST:80"

type Villa struct {
	Title   string `json:"title"`
	Rating  string `json:"rating,omitempty"`
	Reviews string `json:"reviews,omitempty"`
	Address string `json:"address,omitempty"`
	Query   string `json:"query"`
}

type RequestLog struct {
	Query    string        `json:"query"`
	Status   int           `json:"status"`
	Items    int           `json:"items"`
	Bytes    int           `json:"bytes"`
	Latency  time.Duration `json:"latency_ms"`
	Error    string        `json:"error,omitempty"`
	Blocked  bool          `json:"blocked"`
}

type BenchmarkResult struct {
	TotalRequests   int           `json:"total_requests"`
	Successful      int           `json:"successful"`
	Failed          int           `json:"failed"`
	TotalItems      int           `json:"total_items"`
	TotalBytes      int64         `json:"total_bytes"`
	TotalDuration   time.Duration `json:"total_duration_ms"`
	AvgLatency      time.Duration `json:"avg_latency_ms"`
	MinLatency      time.Duration `json:"min_latency_ms"`
	MaxLatency      time.Duration `json:"max_latency_ms"`
	RequestsPerMin  float64       `json:"requests_per_min"`
	ItemsPerMin     float64       `json:"items_per_min"`
	ItemsPerHour    float64       `json:"items_per_hour"`
	SuccessRate     float64       `json:"success_rate_pct"`
	AvgItemsPerReq  float64       `json:"avg_items_per_request"`
	Requests        []RequestLog  `json:"requests"`
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	os.MkdirAll("tests/results", 0755)

	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSMacOS),
		identity.WithLocale("en-US", "en-US", "en"),
		identity.WithTimezone("America/New_York"),
	)

	fmt.Println("══════════════════════════════════════════════════════════")
	fmt.Println("  Google Maps Scraping Performance Benchmark")
	fmt.Println("  Camoufox Binary + Rotating Residential Proxy")
	fmt.Println("══════════════════════════════════════════════════════════")

	cf, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBlockImages(true), // save bandwidth
		fetch.WithHeadless("true"),  // headless for speed
		fetch.WithBrowserProxy(proxyURL),
		fetch.WithBrowserTimeout(60*time.Second),
		fetch.WithPersistSession(false),
	)
	if err != nil {
		slog.Error("launch failed", "err", err)
		os.Exit(1)
	}
	defer cf.Close()

	// Different queries to test variety
	queries := []struct {
		query string
		url   string
	}{
		{"villa di bali", "https://www.google.com/maps/search/villa+di+bali/"},
		{"hotel di ubud", "https://www.google.com/maps/search/hotel+di+ubud/"},
		{"restaurant di seminyak", "https://www.google.com/maps/search/restaurant+di+seminyak/"},
		{"spa di kuta bali", "https://www.google.com/maps/search/spa+di+kuta+bali/"},
		{"yoga studio di canggu", "https://www.google.com/maps/search/yoga+studio+di+canggu/"},
		{"cafe di sanur bali", "https://www.google.com/maps/search/cafe+di+sanur+bali/"},
		{"villa di lombok", "https://www.google.com/maps/search/villa+di+lombok/"},
		{"resort di nusa dua", "https://www.google.com/maps/search/resort+di+nusa+dua/"},
		{"diving center bali", "https://www.google.com/maps/search/diving+center+bali/"},
		{"surfing school bali", "https://www.google.com/maps/search/surfing+school+bali/"},
	}

	var allVillas []Villa
	var logs []RequestLog
	var totalLatency time.Duration
	var minLat, maxLat time.Duration
	totalSuccess := 0
	totalItems := 0
	var totalBytes int64

	benchStart := time.Now()

	for i, q := range queries {
		fmt.Printf("\n[%d/%d] %s\n", i+1, len(queries), q.query)

		// Human-like delay between requests (2-5 seconds)
		if i > 0 {
			delay := time.Duration(2000+rand.IntN(3000)) * time.Millisecond
			time.Sleep(delay)
		}

		reqStart := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		resp, err := cf.Fetch(ctx, &foxhound.Job{
			ID: fmt.Sprintf("maps-%d", i), URL: q.url, Method: "GET",
			FetchMode: foxhound.FetchBrowser,
		})
		cancel()
		latency := time.Since(reqStart)

		log := RequestLog{Query: q.query, Latency: latency}

		if err != nil {
			log.Error = err.Error()
			log.Blocked = true
			logs = append(logs, log)
			fmt.Printf("  ERROR: %v (%s)\n", err, latency.Round(time.Millisecond))
			continue
		}

		log.Status = resp.StatusCode
		log.Bytes = len(resp.Body)
		totalBytes += int64(len(resp.Body))

		if resp.StatusCode >= 400 {
			log.Blocked = true
			logs = append(logs, log)
			fmt.Printf("  HTTP %d (%s)\n", resp.StatusCode, latency.Round(time.Millisecond))
			continue
		}

		// Parse villas
		items := parseMaps(resp, q.query)
		log.Items = len(items)
		logs = append(logs, log)

		totalSuccess++
		totalItems += len(items)
		totalLatency += latency

		if minLat == 0 || latency < minLat { minLat = latency }
		if latency > maxLat { maxLat = latency }

		allVillas = append(allVillas, items...)

		fmt.Printf("  ✓ %d items | %d bytes | %s\n", len(items), len(resp.Body), latency.Round(time.Millisecond))
		for j, v := range items {
			if j >= 3 { fmt.Printf("  ... +%d more\n", len(items)-3); break }
			line := v.Title
			if v.Rating != "" { line += " (" + v.Rating + "★)" }
			fmt.Printf("  [%d] %s\n", j+1, line)
		}
	}

	totalDuration := time.Since(benchStart)

	// Calculate benchmark
	result := BenchmarkResult{
		TotalRequests:  len(queries),
		Successful:     totalSuccess,
		Failed:         len(queries) - totalSuccess,
		TotalItems:     totalItems,
		TotalBytes:     totalBytes,
		TotalDuration:  totalDuration,
		MinLatency:     minLat,
		MaxLatency:     maxLat,
		Requests:       logs,
	}

	if totalSuccess > 0 {
		result.AvgLatency = totalLatency / time.Duration(totalSuccess)
		result.AvgItemsPerReq = float64(totalItems) / float64(totalSuccess)
	}
	if totalDuration.Seconds() > 0 {
		mins := totalDuration.Minutes()
		result.RequestsPerMin = float64(len(queries)) / mins
		result.ItemsPerMin = float64(totalItems) / mins
		result.ItemsPerHour = float64(totalItems) / mins * 60
	}
	result.SuccessRate = float64(totalSuccess) / float64(max(len(queries), 1)) * 100

	// Print report
	fmt.Println("\n\n══════════════════════════════════════════════════════════════")
	fmt.Println("  GOOGLE MAPS SCRAPING PERFORMANCE REPORT")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Printf("  Total Requests:      %d\n", result.TotalRequests)
	fmt.Printf("  Successful:          %d (%.0f%%)\n", result.Successful, result.SuccessRate)
	fmt.Printf("  Failed:              %d\n", result.Failed)
	fmt.Printf("  Total Items:         %d\n", result.TotalItems)
	fmt.Printf("  Total Duration:      %s\n", totalDuration.Round(time.Second))
	fmt.Printf("  Total Bandwidth:     %.1f MB\n", float64(totalBytes)/1024/1024)
	fmt.Println("──────────────────────────────────────────────────────────────")
	fmt.Printf("  Avg Latency:         %s/request\n", result.AvgLatency.Round(time.Millisecond))
	fmt.Printf("  Min Latency:         %s\n", result.MinLatency.Round(time.Millisecond))
	fmt.Printf("  Max Latency:         %s\n", result.MaxLatency.Round(time.Millisecond))
	fmt.Printf("  Avg Items/Request:   %.1f\n", result.AvgItemsPerReq)
	fmt.Println("──────────────────────────────────────────────────────────────")
	fmt.Printf("  Requests/Minute:     %.1f\n", result.RequestsPerMin)
	fmt.Printf("  Items/Minute:        %.1f\n", result.ItemsPerMin)
	fmt.Printf("  Items/Hour:          %.0f (projected)\n", result.ItemsPerHour)
	fmt.Println("══════════════════════════════════════════════════════════════")

	// Per-query breakdown
	fmt.Println("\n  Per-Query Breakdown:")
	fmt.Printf("  %-30s | %5s | %5s | %s\n", "Query", "Items", "Bytes", "Latency")
	fmt.Println("  " + strings.Repeat("─", 65))
	for _, l := range logs {
		status := "✓"
		if l.Blocked || l.Error != "" { status = "✗" }
		fmt.Printf("  %s %-28s | %5d | %5d | %s\n",
			status, l.Query, l.Items, l.Bytes, l.Latency.Round(time.Millisecond))
	}

	// Save results
	saveJSON("tests/results/maps_benchmark_result.json", result)
	saveJSON("tests/results/maps_benchmark_villas.json", allVillas)
	fmt.Printf("\n  Results saved: %d villas total\n", len(allVillas))
}

func parseMaps(resp *foxhound.Response, query string) []Villa {
	doc, err := parse.NewDocument(resp)
	if err != nil { return nil }
	var items []Villa
	seen := map[string]bool{}
	doc.Each("div.Nv2PK, a[aria-label][href*='maps/place']", func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find("div.qBF1Pd, span.fontHeadlineSmall").Text())
		if name == "" { name, _ = s.Attr("aria-label") }
		if name == "" || seen[name] || len(items) >= 10 { return }
		lower := strings.ToLower(name)
		if strings.Contains(lower, "input tools") || strings.Contains(lower, "reklam") ||
			strings.Contains(lower, "iklan") || strings.Contains(lower, "adlı") { return }
		seen[name] = true
		rating := strings.TrimSpace(s.Find("span.MW4etd").Text())
		reviews := strings.TrimSpace(s.Find("span.UY7F9").Text())
		addr := ""
		s.Find("div.W4Efsd").Each(func(_ int, d *goquery.Selection) {
			t := strings.TrimSpace(d.Text())
			if strings.Contains(t, "·") || strings.Contains(t, ",") { addr = t }
		})
		items = append(items, Villa{Title: name, Rating: rating, Reviews: reviews, Address: addr, Query: query})
	})
	return items
}

func saveJSON(path string, v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	os.WriteFile(path, data, 0644)
}
