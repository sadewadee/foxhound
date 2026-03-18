//go:build playwright

// full_test runs all 4 scrape targets with Camoufox browser mode + proxy + human simulation.
// This is the definitive test that validates the complete anti-detection stack.
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
	"github.com/sadewadee/foxhound/captcha"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
	"github.com/sadewadee/foxhound/parse"
)

type Item struct {
	Title    string `json:"title,omitempty"`
	URL      string `json:"url,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
	Price    string `json:"price,omitempty"`
	Rating   string `json:"rating,omitempty"`
	Address  string `json:"address,omitempty"`
	Supplier string `json:"supplier,omitempty"`
	Location string `json:"location,omitempty"`
	Reviews  string `json:"reviews,omitempty"`
	Source   string `json:"source"`
}

type Benchmark struct {
	Target         string  `json:"target"`
	URL            string  `json:"url"`
	Requests       int     `json:"requests"`
	Success        int     `json:"success"`
	Blocked        int     `json:"blocked"`
	CaptchaHit     int     `json:"captcha_detected"`
	Items          int     `json:"items"`
	DurationMs     int64   `json:"duration_ms"`
	AvgLatencyMs   int64   `json:"avg_latency_ms"`
	Bytes          int64   `json:"bytes"`
	BlockAvoidance float64 `json:"block_avoidance_pct"`
}

var proxyURL = getEnvOrDefault("FOXHOUND_PROXY", "socks5://user:pass@proxy:port")

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	os.MkdirAll("tests/results", 0755)

	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
		identity.WithLocale("en-US", "en-US", "en"),
		identity.WithTimezone("America/New_York"),
	)
	slog.Info("identity", "ua", prof.UA[:60]+"...", "locale", prof.Locale)

	slog.Info("launching Camoufox with human simulation...")
	cf, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBlockImages(false), // keep images for more realistic browsing
		fetch.WithHeadless("true"),
		fetch.WithBrowserTimeout(60*time.Second),
		fetch.WithPersistSession(true), // persist cookies across requests
	)
	if err != nil {
		slog.Error("camoufox launch failed", "err", err)
		os.Exit(1)
	}
	defer cf.Close()
	slog.Info("Camoufox ready")

	// Also create stealth fetcher with proxy for static requests
	stealth := fetch.NewStealth(
		fetch.WithIdentity(prof),
		fetch.WithTimeout(30*time.Second),
		fetch.WithProxy(proxyURL),
	)
	defer stealth.Close()

	var allBenchmarks []Benchmark

	// ═══════════════════════════════════════
	// TARGET 1: Google SERP via proxy + stealth
	// ═══════════════════════════════════════
	fmt.Println("\n═══════════════════════════════════════════════════")
	fmt.Println("TARGET 1: Google SERP — wisata alam jawa timur")
	fmt.Println("═══════════════════════════════════════════════════")
	b1, items1 := scrapeGoogleSERP(stealth, cf)
	allBenchmarks = append(allBenchmarks, b1)
	saveJSON("tests/results/final_google_serp.json", items1)

	humanPause()

	// ═══════════════════════════════════════
	// TARGET 2: Google Maps — villa di bali
	// ═══════════════════════════════════════
	fmt.Println("\n═══════════════════════════════════════════════════")
	fmt.Println("TARGET 2: Google Maps — villa di bali")
	fmt.Println("═══════════════════════════════════════════════════")
	b2, items2 := scrapeGoogleMaps(cf)
	allBenchmarks = append(allBenchmarks, b2)
	saveJSON("tests/results/final_google_maps.json", items2)

	humanPause()

	// ═══════════════════════════════════════
	// TARGET 3: Alibaba — 10 yoga mat products
	// ═══════════════════════════════════════
	fmt.Println("\n═══════════════════════════════════════════════════")
	fmt.Println("TARGET 3: Alibaba — yoga mat products")
	fmt.Println("═══════════════════════════════════════════════════")
	b3, items3 := scrapeAlibaba(cf)
	allBenchmarks = append(allBenchmarks, b3)
	saveJSON("tests/results/final_alibaba.json", items3)

	humanPause()

	// ═══════════════════════════════════════
	// TARGET 4: Yoga Alliance — 10 schools
	// ═══════════════════════════════════════
	fmt.Println("\n═══════════════════════════════════════════════════")
	fmt.Println("TARGET 4: Yoga Alliance — school directory")
	fmt.Println("═══════════════════════════════════════════════════")
	b4, items4 := scrapeYogaAlliance(cf)
	allBenchmarks = append(allBenchmarks, b4)
	saveJSON("tests/results/final_yoga_alliance.json", items4)

	// ═══════════════════════════════════════
	// COMBINED REPORT
	// ═══════════════════════════════════════
	fmt.Println("\n\n══════════════════════════════════════════════════════════════════════")
	fmt.Println("FINAL BENCHMARK REPORT — Foxhound Full Anti-Detection Stack")
	fmt.Println("══════════════════════════════════════════════════════════════════════")
	fmt.Printf("%-20s | %4s | %4s | %4s | %5s | %7s | %s\n",
		"Target", "Reqs", "OK", "Block", "CAPTCHA", "Items", "Avoidance")
	fmt.Println("──────────────────────────────────────────────────────────────────────")

	totalReqs, totalOK, totalBlocked, totalCaptcha, totalItems := 0, 0, 0, 0, 0
	for _, b := range allBenchmarks {
		totalReqs += b.Requests
		totalOK += b.Success
		totalBlocked += b.Blocked
		totalCaptcha += b.CaptchaHit
		totalItems += b.Items
		fmt.Printf("%-20s | %4d | %4d | %4d | %5d  | %7d | %.0f%%\n",
			b.Target, b.Requests, b.Success, b.Blocked, b.CaptchaHit, b.Items, b.BlockAvoidance)
	}
	fmt.Println("──────────────────────────────────────────────────────────────────────")
	avoidance := float64(totalOK) / float64(max(totalReqs, 1)) * 100
	fmt.Printf("%-20s | %4d | %4d | %4d | %5d  | %7d | %.0f%%\n",
		"TOTAL", totalReqs, totalOK, totalBlocked, totalCaptcha, totalItems, avoidance)
	fmt.Println("══════════════════════════════════════════════════════════════════════")

	saveJSON("tests/results/final_combined_benchmark.json", allBenchmarks)
}

func scrapeGoogleSERP(stealth foxhound.Fetcher, browser foxhound.Fetcher) (Benchmark, []Item) {
	b := Benchmark{Target: "Google SERP", URL: "https://www.google.com/search?q=wisata+alam+jawa+timur&hl=id&gl=id&num=20"}
	start := time.Now()

	// Try stealth+proxy first
	slog.Info("trying stealth+proxy...")
	resp := doFetch(stealth, b.URL, &b)
	var items []Item

	if resp != nil {
		items = parseGoogleSERP(resp)
		// Check for CAPTCHA
		if det := captcha.Detect(resp); det.Type != captcha.CaptchaNone {
			slog.Warn("CAPTCHA detected on stealth", "type", det.Type)
			b.CaptchaHit++
			items = nil
		}
	}

	// If stealth failed or got CAPTCHA, try browser
	if len(items) == 0 {
		slog.Info("stealth yielded no results, trying Camoufox browser...")
		humanPause()
		resp = doFetch(browser, b.URL, &b)
		if resp != nil {
			if det := captcha.Detect(resp); det.Type != captcha.CaptchaNone {
				slog.Warn("CAPTCHA detected on browser too", "type", det.Type)
				b.CaptchaHit++
			} else {
				items = parseGoogleSERP(resp)
			}
		}
	}

	b.Items = len(items)
	b.DurationMs = time.Since(start).Milliseconds()
	b.AvgLatencyMs = b.DurationMs / int64(max(b.Requests, 1))
	b.BlockAvoidance = float64(b.Success) / float64(max(b.Requests, 1)) * 100
	printBenchmark(b)
	for i, it := range items {
		fmt.Printf("  [%d] %s\n      %s\n", i+1, it.Title, it.URL)
	}
	return b, items
}

func parseGoogleSERP(resp *foxhound.Response) []Item {
	doc, err := parse.NewDocument(resp)
	if err != nil {
		return nil
	}
	var items []Item

	// Multiple selector strategies for Google's varying layouts
	selectors := []string{"div.g", "div[data-hveid]", "div.MjjYud"}
	for _, sel := range selectors {
		doc.Each(sel, func(_ int, s *goquery.Selection) {
			h3 := s.Find("h3")
			title := strings.TrimSpace(h3.Text())
			href, _ := s.Find("a").First().Attr("href")
			snippet := strings.TrimSpace(s.Find("div.VwiC3b, span.aCOpRe, div[data-sncf]").Text())

			if title != "" && href != "" && !strings.HasPrefix(href, "/search") {
				if strings.HasPrefix(href, "/url?q=") {
					href = strings.SplitN(strings.TrimPrefix(href, "/url?q="), "&", 2)[0]
				}
				items = append(items, Item{Title: title, URL: href, Snippet: snippet, Source: "google_serp"})
			}
		})
		if len(items) > 0 {
			break
		}
	}
	return items
}

func scrapeGoogleMaps(browser foxhound.Fetcher) (Benchmark, []Item) {
	b := Benchmark{Target: "Google Maps", URL: "https://www.google.com/maps/search/villa+di+bali/"}
	start := time.Now()

	resp := doFetch(browser, b.URL, &b)
	var items []Item
	if resp != nil {
		doc, err := parse.NewDocument(resp)
		if err == nil {
			doc.Each("div.Nv2PK, div[role='article'], a[aria-label]", func(_ int, s *goquery.Selection) {
				name := strings.TrimSpace(s.Find("div.qBF1Pd, span.fontHeadlineSmall").Text())
				if name == "" {
					name, _ = s.Attr("aria-label")
				}
				rating := strings.TrimSpace(s.Find("span.MW4etd").Text())
				reviews := strings.TrimSpace(s.Find("span.UY7F9").Text())
				addr := strings.TrimSpace(s.Find("div.W4Efsd:last-child").Text())

				if name != "" && len(items) < 15 {
					items = append(items, Item{Title: name, Rating: rating, Reviews: reviews, Address: addr, Source: "google_maps"})
				}
			})
		}
	}

	b.Items = len(items)
	b.DurationMs = time.Since(start).Milliseconds()
	b.AvgLatencyMs = b.DurationMs / int64(max(b.Requests, 1))
	b.BlockAvoidance = float64(b.Success) / float64(max(b.Requests, 1)) * 100
	printBenchmark(b)
	for i, it := range items {
		fmt.Printf("  [%d] %s (rating: %s, reviews: %s)\n", i+1, it.Title, it.Rating, it.Reviews)
	}
	return b, items
}

func scrapeAlibaba(browser foxhound.Fetcher) (Benchmark, []Item) {
	b := Benchmark{Target: "Alibaba", URL: "https://www.alibaba.com/trade/search?SearchText=yoga+mat&page=1"}
	start := time.Now()

	resp := doFetch(browser, b.URL, &b)
	var items []Item

	if resp != nil {
		if det := captcha.Detect(resp); det.Type != captcha.CaptchaNone {
			slog.Warn("CAPTCHA on Alibaba", "type", det.Type)
			b.CaptchaHit++
		} else {
			doc, err := parse.NewDocument(resp)
			if err == nil {
				// Try multiple selector patterns
				selectorSets := [][]string{
					{"div.fy23-search-card", "h2.search-card-e-title", "div.search-card-e-price-main", "a.search-card-e-company"},
					{"div.J-offer-wrapper", "h4.offer-title", "span.price", "a.company-name"},
					{"div[data-content='product']", "h2, h3", "span.price, div.price", "span.company, a.company"},
				}
				for _, sels := range selectorSets {
					doc.Each(sels[0], func(_ int, s *goquery.Selection) {
						title := strings.TrimSpace(s.Find(sels[1]).Text())
						price := strings.TrimSpace(s.Find(sels[2]).Text())
						supplier := strings.TrimSpace(s.Find(sels[3]).Text())
						if title != "" && len(items) < 10 {
							items = append(items, Item{Title: title, Price: price, Supplier: supplier, Source: "alibaba"})
						}
					})
					if len(items) > 0 {
						break
					}
				}
			}
		}
	}

	b.Items = len(items)
	b.DurationMs = time.Since(start).Milliseconds()
	b.AvgLatencyMs = b.DurationMs / int64(max(b.Requests, 1))
	b.BlockAvoidance = float64(b.Success) / float64(max(b.Requests, 1)) * 100
	printBenchmark(b)
	for i, it := range items {
		fmt.Printf("  [%d] %s — %s (%s)\n", i+1, it.Title, it.Price, it.Supplier)
	}
	return b, items
}

func scrapeYogaAlliance(browser foxhound.Fetcher) (Benchmark, []Item) {
	b := Benchmark{Target: "Yoga Alliance", URL: "https://app.yogaalliance.org/directoryregistrants?type=School"}
	start := time.Now()

	resp := doFetch(browser, b.URL, &b)
	var items []Item

	if resp != nil {
		doc, err := parse.NewDocument(resp)
		if err == nil {
			// Yoga Alliance is a Salesforce LWR SPA — try multiple card patterns
			doc.Each("div[class*='card'], div[class*='result'], article, li[class*='item'], tr[class*='row']", func(_ int, s *goquery.Selection) {
				name := strings.TrimSpace(s.Find("h3, h4, h5, a[class*='title'], strong, span[class*='name']").First().Text())
				loc := strings.TrimSpace(s.Find("span[class*='location'], div[class*='address'], p[class*='text'], span[class*='city']").First().Text())
				if name != "" && len(name) > 3 && !strings.Contains(strings.ToLower(name), "cookie") && len(items) < 10 {
					items = append(items, Item{Title: name, Location: loc, Source: "yoga_alliance"})
				}
			})
		}
	}

	b.Items = len(items)
	b.DurationMs = time.Since(start).Milliseconds()
	b.AvgLatencyMs = b.DurationMs / int64(max(b.Requests, 1))
	b.BlockAvoidance = float64(b.Success) / float64(max(b.Requests, 1)) * 100
	printBenchmark(b)
	for i, it := range items {
		fmt.Printf("  [%d] %s — %s\n", i+1, it.Title, it.Location)
	}
	return b, items
}

func doFetch(f foxhound.Fetcher, url string, b *Benchmark) *foxhound.Response {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	job := &foxhound.Job{
		ID: fmt.Sprintf("scrape-%d", time.Now().UnixNano()), URL: url, Method: "GET",
		FetchMode: foxhound.FetchBrowser,
	}
	resp, err := f.Fetch(ctx, job)
	b.Requests++
	if err != nil {
		b.Blocked++
		slog.Error("fetch error", "url", url, "err", err)
		return nil
	}
	b.Bytes += int64(len(resp.Body))
	if resp.StatusCode >= 400 {
		b.Blocked++
		slog.Warn("HTTP error", "url", url, "status", resp.StatusCode)
	} else {
		b.Success++
		slog.Info("fetched", "url", url, "status", resp.StatusCode, "bytes", len(resp.Body), "duration", resp.Duration)
	}
	return resp
}

func humanPause() {
	d := time.Duration(3000+rand.IntN(4000)) * time.Millisecond
	slog.Info("human pause", "delay", d)
	time.Sleep(d)
}

func printBenchmark(b Benchmark) {
	fmt.Printf("  Requests: %d | OK: %d | Blocked: %d | CAPTCHA: %d | Items: %d | Avoidance: %.0f%%\n",
		b.Requests, b.Success, b.Blocked, b.CaptchaHit, b.Items, b.BlockAvoidance)
}

func saveJSON(path string, v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	os.WriteFile(path, data, 0644)
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" { return v }
	return fallback
}
