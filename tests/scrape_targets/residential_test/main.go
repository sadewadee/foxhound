//go:build playwright

// residential_test — scrape with rotating residential proxy + aggressive wait/scroll
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
	"github.com/playwright-community/playwright-go"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/captcha"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
	"github.com/sadewadee/foxhound/parse"
)

var proxyURL = getEnvOrDefault("FOXHOUND_PROXY", "socks5://user:pass@proxy:port")

type Item struct {
	Title    string `json:"title,omitempty"`
	URL      string `json:"url,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
	Price    string `json:"price,omitempty"`
	Rating   string `json:"rating,omitempty"`
	Supplier string `json:"supplier,omitempty"`
	Location string `json:"location,omitempty"`
	Reviews  string `json:"reviews,omitempty"`
	Source   string `json:"source"`
}

type Benchmark struct {
	Target     string  `json:"target"`
	Requests   int     `json:"requests"`
	Success    int     `json:"success"`
	Blocked    int     `json:"blocked"`
	Captcha    int     `json:"captcha"`
	Items      int     `json:"items"`
	DurationMs int64   `json:"duration_ms"`
	Bytes      int64   `json:"bytes"`
	Avoidance  float64 `json:"avoidance_pct"`
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

	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println("  FOXHOUND — Rotating Residential Proxy Test")
	fmt.Printf("  Proxy:  %s\n", "(from FOXHOUND_PROXY env)")
	fmt.Printf("  UA:     %s\n", prof.UA)
	fmt.Println("══════════════════════════════════════════════════════════════")

	// Use HTTP since Playwright Firefox doesn't support SOCKS5+auth
	httpProxy := proxyURL

	cf, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBlockImages(false),
		fetch.WithHeadless("false"),
		fetch.WithBrowserProxy(httpProxy),
		fetch.WithBrowserTimeout(90*time.Second),
		fetch.WithPersistSession(false), // fresh context per request = new IP per target
	)
	if err != nil {
		slog.Error("launch failed", "err", err)
		os.Exit(1)
	}
	defer cf.Close()

	// We need direct access to playwright for advanced interactions
	// So we'll use the Camoufox fetcher for basic nav, then do custom waits

	var benchmarks []Benchmark

	// ═══════════════════════
	// 1. GOOGLE SERP
	// ═══════════════════════
	fmt.Println("\n[1/4] Google SERP — wisata alam jawa timur")
	b1, items1 := scrapeWithRetry(cf, "Google SERP",
		"https://www.google.com/search?q=wisata+alam+jawa+timur&hl=id&gl=id&num=10",
		parseGoogleSERP, 2)
	benchmarks = append(benchmarks, b1)
	saveJSON("tests/results/resi_google_serp.json", items1)
	humanPause()

	// ═══════════════════════
	// 2. GOOGLE MAPS
	// ═══════════════════════
	fmt.Println("\n[2/4] Google Maps — villa di bali")
	b2, items2 := scrapeWithRetry(cf, "Google Maps",
		"https://www.google.com/maps/search/villa+di+bali/",
		parseGoogleMaps, 1)
	benchmarks = append(benchmarks, b2)
	saveJSON("tests/results/resi_google_maps.json", items2)
	humanPause()

	// ═══════════════════════
	// 3. ALIBABA
	// ═══════════════════════
	fmt.Println("\n[3/4] Alibaba — yoga mat (10 products)")
	b3, items3 := scrapeWithRetry(cf, "Alibaba",
		"https://www.alibaba.com/trade/search?SearchText=yoga+mat&page=1",
		parseAlibaba, 2)
	benchmarks = append(benchmarks, b3)
	saveJSON("tests/results/resi_alibaba.json", items3)
	humanPause()

	// ═══════════════════════
	// 4. YOGA ALLIANCE
	// ═══════════════════════
	fmt.Println("\n[4/4] Yoga Alliance — 10 schools")
	b4, items4 := scrapeWithRetry(cf, "Yoga Alliance",
		"https://app.yogaalliance.org/directoryregistrants?type=School",
		parseYogaAlliance, 2)
	benchmarks = append(benchmarks, b4)
	saveJSON("tests/results/resi_yoga_alliance.json", items4)

	// ═══════════════════════
	// REPORT
	// ═══════════════════════
	fmt.Println("\n\n══════════════════════════════════════════════════════════════════")
	fmt.Println("FINAL REPORT — Rotating Residential Proxy")
	fmt.Println("══════════════════════════════════════════════════════════════════")
	fmt.Printf("%-20s | %4s | %4s | %5s | %5s | %5s | %s\n",
		"Target", "Reqs", "OK", "Block", "CAPT", "Items", "Avoidance")
	fmt.Println("─────────────────────────────────────────────────────────────────")
	tR, tOK, tB, tC, tI := 0, 0, 0, 0, 0
	for _, b := range benchmarks {
		tR += b.Requests; tOK += b.Success; tB += b.Blocked; tC += b.Captcha; tI += b.Items
		fmt.Printf("%-20s | %4d | %4d | %5d | %5d | %5d | %.0f%%\n",
			b.Target, b.Requests, b.Success, b.Blocked, b.Captcha, b.Items, b.Avoidance)
	}
	fmt.Println("─────────────────────────────────────────────────────────────────")
	fmt.Printf("%-20s | %4d | %4d | %5d | %5d | %5d | %.0f%%\n",
		"TOTAL", tR, tOK, tB, tC, tI, float64(tOK)/float64(max(tR, 1))*100)
	fmt.Println("══════════════════════════════════════════════════════════════════")

	saveJSON("tests/results/resi_combined_benchmark.json", benchmarks)
}

type parseFunc func(*foxhound.Response) []Item

func scrapeWithRetry(f foxhound.Fetcher, name, url string, parser parseFunc, maxRetries int) (Benchmark, []Item) {
	b := Benchmark{Target: name}
	start := time.Now()

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			slog.Info("retrying", "target", name, "attempt", attempt)
			time.Sleep(time.Duration(3000+rand.IntN(4000)) * time.Millisecond)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		resp, err := f.Fetch(ctx, &foxhound.Job{
			ID: fmt.Sprintf("%s-%d", name, attempt), URL: url, Method: "GET",
			FetchMode: foxhound.FetchBrowser,
		})
		cancel()
		b.Requests++

		if err != nil {
			b.Blocked++
			slog.Error("fetch error", "target", name, "attempt", attempt, "err", err)
			continue
		}
		b.Bytes += int64(len(resp.Body))

		if resp.StatusCode >= 400 {
			b.Blocked++
			slog.Warn("HTTP error", "target", name, "status", resp.StatusCode)
			continue
		}
		b.Success++

		slog.Info("fetched", "target", name, "status", resp.StatusCode,
			"bytes", len(resp.Body), "duration", resp.Duration)

		// Check CAPTCHA
		if det := captcha.Detect(resp); det.Type != captcha.CaptchaNone {
			b.Captcha++
			slog.Warn("CAPTCHA in response", "target", name, "type", det.Type)
			// reCAPTCHA handler already tried to click — if still present, retry with new IP
			continue
		}

		items := parser(resp)
		if len(items) > 0 {
			b.Items = len(items)
			b.DurationMs = time.Since(start).Milliseconds()
			b.Avoidance = float64(b.Success) / float64(max(b.Requests, 1)) * 100
			fmt.Printf("  ✓ %d items extracted on attempt %d\n", len(items), attempt+1)
			for i, it := range items {
				line := it.Title
				if it.Price != "" { line += " — " + it.Price }
				if it.Rating != "" { line += " (" + it.Rating + "★)" }
				if it.Supplier != "" { line += " [" + it.Supplier + "]" }
				if it.Location != "" { line += " @ " + it.Location }
				fmt.Printf("  [%d] %s\n", i+1, line)
			}
			return b, items
		}

		slog.Warn("no items parsed", "target", name, "attempt", attempt, "bytes", len(resp.Body))
		// Save debug HTML for inspection
		debugPath := fmt.Sprintf("tests/results/debug_%s_attempt%d.html", strings.ReplaceAll(strings.ToLower(name), " ", "_"), attempt)
		os.WriteFile(debugPath, resp.Body, 0644)
	}

	b.DurationMs = time.Since(start).Milliseconds()
	b.Avoidance = float64(b.Success) / float64(max(b.Requests, 1)) * 100
	fmt.Printf("  ✗ 0 items after %d attempts\n", b.Requests)
	return b, nil
}

// ─── PARSERS ───────────────────────────────────────────────

func parseGoogleSERP(resp *foxhound.Response) []Item {
	doc, err := parse.NewDocument(resp)
	if err != nil { return nil }
	var items []Item
	// Try multiple Google SERP selector strategies
	for _, sel := range []string{"div.g", "div.MjjYud div.g", "div[data-hveid]"} {
		doc.Each(sel, func(_ int, s *goquery.Selection) {
			title := strings.TrimSpace(s.Find("h3").Text())
			href, _ := s.Find("a").First().Attr("href")
			snippet := strings.TrimSpace(s.Find("div.VwiC3b, span.aCOpRe, div[data-sncf]").Text())
			if title != "" && href != "" && !strings.HasPrefix(href, "/search") && !strings.HasPrefix(href, "#") {
				if strings.HasPrefix(href, "/url?q=") {
					href = strings.SplitN(strings.TrimPrefix(href, "/url?q="), "&", 2)[0]
				}
				items = append(items, Item{Title: title, URL: href, Snippet: snippet, Source: "google_serp"})
			}
		})
		if len(items) > 0 { break }
	}
	// Dedup by URL
	seen := map[string]bool{}
	var deduped []Item
	for _, it := range items {
		if !seen[it.URL] { seen[it.URL] = true; deduped = append(deduped, it) }
	}
	return deduped
}

func parseGoogleMaps(resp *foxhound.Response) []Item {
	doc, err := parse.NewDocument(resp)
	if err != nil { return nil }
	var items []Item
	seen := map[string]bool{}
	doc.Each("div.Nv2PK, a[aria-label][href*='maps/place']", func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find("div.qBF1Pd, span.fontHeadlineSmall").Text())
		if name == "" { name, _ = s.Attr("aria-label") }
		if name == "" || seen[name] || len(items) >= 10 { return }
		seen[name] = true
		rating := strings.TrimSpace(s.Find("span.MW4etd").Text())
		reviews := strings.TrimSpace(s.Find("span.UY7F9").Text())
		addr := strings.TrimSpace(s.Find("div.W4Efsd:last-child span:last-child").Text())
		items = append(items, Item{Title: name, Rating: rating, Reviews: reviews, Location: addr, Source: "google_maps"})
	})
	return items
}

func parseAlibaba(resp *foxhound.Response) []Item {
	doc, err := parse.NewDocument(resp)
	if err != nil { return nil }
	var items []Item

	// Multiple selector strategies — Alibaba changes frequently
	strategies := []struct{ card, title, price, supplier string }{
		{"div.fy23-search-card", "h2.search-card-e-title a", "div.search-card-e-price-main", "a.search-card-e-company"},
		{"div.organic-list-offer-outter", "h4.list-no-v2-title a", "span.list-no-v2-price", "a.list-no-v2-company"},
		{"div.J-offer-wrapper", "h2 a, h4 a", "span.price", "a.company-name"},
		{"div[class*='ProductCard']", "h2, h3, a[title]", "span[class*='rice'], div[class*='rice']", "span[class*='ompany'], a[class*='ompany']"},
	}
	for _, s := range strategies {
		doc.Each(s.card, func(_ int, sel *goquery.Selection) {
			title := strings.TrimSpace(sel.Find(s.title).Text())
			if title == "" { title, _ = sel.Find(s.title).Attr("title") }
			price := strings.TrimSpace(sel.Find(s.price).Text())
			supplier := strings.TrimSpace(sel.Find(s.supplier).Text())
			if title != "" && len(items) < 10 {
				items = append(items, Item{Title: title, Price: price, Supplier: supplier, Source: "alibaba"})
			}
		})
		if len(items) > 0 { break }
	}

	// Fallback: search for any product-like pattern
	if len(items) == 0 {
		doc.Each("a[href*='/product-detail/'], a[href*='/product/']", func(_ int, s *goquery.Selection) {
			title := strings.TrimSpace(s.Text())
			if title == "" { title, _ = s.Attr("title") }
			href, _ := s.Attr("href")
			if title != "" && len(title) > 10 && len(items) < 10 {
				if !strings.HasPrefix(href, "http") { href = "https://www.alibaba.com" + href }
				items = append(items, Item{Title: title, URL: href, Source: "alibaba"})
			}
		})
	}
	return items
}

func parseYogaAlliance(resp *foxhound.Response) []Item {
	doc, err := parse.NewDocument(resp)
	if err != nil { return nil }
	var items []Item

	// Strategy 1: Card-based layouts
	doc.Each("div[class*='card'], div[class*='registrant'], article[class*='result']", func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find("h3, h4, h5, a[class*='title'], a[class*='name'], strong").First().Text())
		loc := strings.TrimSpace(s.Find("span[class*='location'], span[class*='city'], div[class*='address'], p").First().Text())
		if name != "" && len(name) > 3 && !isNavText(name) && len(items) < 10 {
			items = append(items, Item{Title: name, Location: loc, Source: "yoga_alliance"})
		}
	})

	// Strategy 2: Table rows
	if len(items) == 0 {
		doc.Each("table tbody tr, div[role='row']", func(_ int, s *goquery.Selection) {
			cells := s.Find("td, div[role='cell']")
			if cells.Length() >= 2 {
				name := strings.TrimSpace(cells.First().Text())
				loc := strings.TrimSpace(cells.Eq(1).Text())
				if name != "" && len(name) > 3 && !isNavText(name) && len(items) < 10 {
					items = append(items, Item{Title: name, Location: loc, Source: "yoga_alliance"})
				}
			}
		})
	}

	// Strategy 3: Any list of links that look like school names
	if len(items) == 0 {
		doc.Each("a[href*='school'], a[href*='registrant'], a[href*='profile']", func(_ int, s *goquery.Selection) {
			name := strings.TrimSpace(s.Text())
			href, _ := s.Attr("href")
			if name != "" && len(name) > 5 && !isNavText(name) && len(items) < 10 {
				items = append(items, Item{Title: name, URL: href, Source: "yoga_alliance"})
			}
		})
	}
	return items
}

func isNavText(s string) bool {
	lower := strings.ToLower(s)
	navWords := []string{"cookie", "privacy", "menu", "login", "sign in", "search", "filter", "virtual", "upcoming", "home", "about", "contact", "help", "faq"}
	for _, w := range navWords {
		if strings.Contains(lower, w) { return true }
	}
	return len(s) < 4
}

// ─── HELPERS ───────────────────────────────────────────────

// Suppress unused import
var _ = playwright.Bool
var _ = captcha.CaptchaNone

func humanPause() {
	d := time.Duration(4000+rand.IntN(5000)) * time.Millisecond
	slog.Info("pause", "delay", d)
	time.Sleep(d)
}

func saveJSON(path string, v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	os.WriteFile(path, data, 0644)
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" { return v }
	return fallback
}
