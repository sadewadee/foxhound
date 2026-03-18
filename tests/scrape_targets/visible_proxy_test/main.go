//go:build playwright

// visible_proxy_test runs all 4 scrape targets with:
//   - headless: false (visible browser window)
//   - proxy from FOXHOUND_PROXY env
//   - human simulation (mouse, scroll, delays)
//   - persistent session (cookies survive across pages)
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

// Firefox/Playwright doesn't support SOCKS5 with auth — use HTTP proxy instead.
var proxyURL = getEnvOrDefault("FOXHOUND_PROXY", "http://user:pass@proxy:port")

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
	Bytes          int64   `json:"bytes"`
	BlockAvoidance float64 `json:"block_avoidance_pct"`
	Mode           string  `json:"mode"`
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
	fmt.Println("  FOXHOUND — Visible Browser + SOCKS5 Proxy Test")
	fmt.Println("  Headless: FALSE (you will see the browser window)")
	fmt.Printf("  Proxy:    %s\n", "(from FOXHOUND_PROXY env)")
	fmt.Printf("  UA:       %s\n", prof.UA)
	fmt.Println("══════════════════════════════════════════════════════════════")

	slog.Info("launching visible Camoufox with proxy...")
	cf, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBlockImages(false),
		fetch.WithHeadless("false"),        // VISIBLE browser
		fetch.WithBrowserProxy(proxyURL),   // SOCKS5 proxy
		fetch.WithBrowserTimeout(60*time.Second),
		fetch.WithPersistSession(true),
	)
	if err != nil {
		slog.Error("camoufox launch failed", "err", err)
		os.Exit(1)
	}
	defer cf.Close()
	slog.Info("Camoufox visible browser ready with proxy")

	var allBenchmarks []Benchmark

	// ═══════════════════════════════════════
	// TARGET 1: Google SERP
	// ═══════════════════════════════════════
	fmt.Println("\n[1/4] Google SERP — wisata alam jawa timur")
	b1, items1 := scrapeTarget(cf, "Google SERP",
		"https://www.google.com/search?q=wisata+alam+jawa+timur&hl=id&gl=id&num=10",
		parseGoogleSERP)
	allBenchmarks = append(allBenchmarks, b1)
	saveJSON("tests/results/visible_google_serp.json", items1)
	humanPause(4, 7)

	// ═══════════════════════════════════════
	// TARGET 2: Google Maps
	// ═══════════════════════════════════════
	fmt.Println("\n[2/4] Google Maps — villa di bali")
	b2, items2 := scrapeTarget(cf, "Google Maps",
		"https://www.google.com/maps/search/villa+di+bali/",
		parseGoogleMaps)
	allBenchmarks = append(allBenchmarks, b2)
	saveJSON("tests/results/visible_google_maps.json", items2)
	humanPause(4, 7)

	// ═══════════════════════════════════════
	// TARGET 3: Alibaba
	// ═══════════════════════════════════════
	fmt.Println("\n[3/4] Alibaba — yoga mat")
	b3, items3 := scrapeTarget(cf, "Alibaba",
		"https://www.alibaba.com/trade/search?SearchText=yoga+mat&page=1",
		parseAlibaba)
	allBenchmarks = append(allBenchmarks, b3)
	saveJSON("tests/results/visible_alibaba.json", items3)
	humanPause(4, 7)

	// ═══════════════════════════════════════
	// TARGET 4: Yoga Alliance
	// ═══════════════════════════════════════
	fmt.Println("\n[4/4] Yoga Alliance — school directory")
	b4, items4 := scrapeTarget(cf, "Yoga Alliance",
		"https://app.yogaalliance.org/directoryregistrants?type=School",
		parseYogaAlliance)
	allBenchmarks = append(allBenchmarks, b4)
	saveJSON("tests/results/visible_yoga_alliance.json", items4)

	// ═══════════════════════════════════════
	// REPORT
	// ═══════════════════════════════════════
	fmt.Println("\n\n══════════════════════════════════════════════════════════════════════════")
	fmt.Println("BENCHMARK REPORT — Visible Browser + SOCKS5 Proxy")
	fmt.Println("══════════════════════════════════════════════════════════════════════════")
	fmt.Printf("%-20s | %4s | %4s | %5s | %5s | %7s | %s\n",
		"Target", "Reqs", "OK", "Block", "CAPTCHA", "Items", "Avoidance")
	fmt.Println("──────────────────────────────────────────────────────────────────────────")

	totalReqs, totalOK, totalBlocked, totalCaptcha, totalItems := 0, 0, 0, 0, 0
	for _, b := range allBenchmarks {
		totalReqs += b.Requests
		totalOK += b.Success
		totalBlocked += b.Blocked
		totalCaptcha += b.CaptchaHit
		totalItems += b.Items
		fmt.Printf("%-20s | %4d | %4d | %5d | %5d   | %7d | %.0f%%\n",
			b.Target, b.Requests, b.Success, b.Blocked, b.CaptchaHit, b.Items, b.BlockAvoidance)
	}
	fmt.Println("──────────────────────────────────────────────────────────────────────────")
	avoidance := float64(totalOK) / float64(max(totalReqs, 1)) * 100
	fmt.Printf("%-20s | %4d | %4d | %5d | %5d   | %7d | %.0f%%\n",
		"TOTAL", totalReqs, totalOK, totalBlocked, totalCaptcha, totalItems, avoidance)
	fmt.Println("══════════════════════════════════════════════════════════════════════════")

	saveJSON("tests/results/visible_combined_benchmark.json", allBenchmarks)
}

type parseFunc func(resp *foxhound.Response) []Item

func scrapeTarget(f foxhound.Fetcher, name, url string, parser parseFunc) (Benchmark, []Item) {
	b := Benchmark{Target: name, URL: url, Mode: "visible+proxy"}
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	job := &foxhound.Job{
		ID: fmt.Sprintf("visible-%s-%d", name, time.Now().UnixNano()),
		URL: url, Method: "GET", FetchMode: foxhound.FetchBrowser,
	}

	resp, err := f.Fetch(ctx, job)
	b.Requests++

	if err != nil {
		b.Blocked++
		slog.Error("fetch failed", "target", name, "err", err)
		b.DurationMs = time.Since(start).Milliseconds()
		b.BlockAvoidance = 0
		printResult(b, nil)
		return b, nil
	}

	b.Bytes += int64(len(resp.Body))

	if resp.StatusCode >= 400 {
		b.Blocked++
		slog.Warn("HTTP error", "target", name, "status", resp.StatusCode)
	} else {
		b.Success++
	}

	slog.Info("fetched", "target", name, "status", resp.StatusCode,
		"bytes", len(resp.Body), "duration", resp.Duration)

	// Check CAPTCHA
	if det := captcha.Detect(resp); det.Type != captcha.CaptchaNone {
		slog.Warn("CAPTCHA detected", "target", name, "type", det.Type, "sitekey", det.SiteKey)
		b.CaptchaHit++
		b.DurationMs = time.Since(start).Milliseconds()
		b.BlockAvoidance = float64(b.Success) / float64(max(b.Requests, 1)) * 100
		printResult(b, nil)
		return b, nil
	}

	items := parser(resp)
	b.Items = len(items)
	b.DurationMs = time.Since(start).Milliseconds()
	b.BlockAvoidance = float64(b.Success) / float64(max(b.Requests, 1)) * 100

	printResult(b, items)
	return b, items
}

func parseGoogleSERP(resp *foxhound.Response) []Item {
	doc, err := parse.NewDocument(resp)
	if err != nil {
		return nil
	}
	var items []Item
	for _, sel := range []string{"div.g", "div.MjjYud", "div[data-hveid]"} {
		doc.Each(sel, func(_ int, s *goquery.Selection) {
			title := strings.TrimSpace(s.Find("h3").Text())
			href, _ := s.Find("a").First().Attr("href")
			snippet := strings.TrimSpace(s.Find("div.VwiC3b, span.aCOpRe").Text())
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

func parseGoogleMaps(resp *foxhound.Response) []Item {
	doc, err := parse.NewDocument(resp)
	if err != nil {
		return nil
	}
	var items []Item
	doc.Each("div.Nv2PK, a[aria-label]", func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find("div.qBF1Pd, span.fontHeadlineSmall").Text())
		if name == "" {
			name, _ = s.Attr("aria-label")
		}
		rating := strings.TrimSpace(s.Find("span.MW4etd").Text())
		reviews := strings.TrimSpace(s.Find("span.UY7F9").Text())
		if name != "" && len(items) < 10 {
			items = append(items, Item{Title: name, Rating: rating, Reviews: reviews, Source: "google_maps"})
		}
	})
	return items
}

func parseAlibaba(resp *foxhound.Response) []Item {
	doc, err := parse.NewDocument(resp)
	if err != nil {
		return nil
	}
	var items []Item
	for _, sels := range [][]string{
		{"div.fy23-search-card", "h2.search-card-e-title", "div.search-card-e-price-main", "a.search-card-e-company"},
		{"div.J-offer-wrapper", "h4.offer-title", "span.price", "a.company-name"},
	} {
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
	return items
}

func parseYogaAlliance(resp *foxhound.Response) []Item {
	doc, err := parse.NewDocument(resp)
	if err != nil {
		return nil
	}
	var items []Item
	doc.Each("div[class*='card'], article, li[class*='item']", func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find("h3, h4, h5, strong, a[class*='title']").First().Text())
		loc := strings.TrimSpace(s.Find("span[class*='location'], p[class*='text']").First().Text())
		if name != "" && len(name) > 3 && !strings.Contains(strings.ToLower(name), "cookie") && len(items) < 10 {
			items = append(items, Item{Title: name, Location: loc, Source: "yoga_alliance"})
		}
	})
	return items
}

func printResult(b Benchmark, items []Item) {
	fmt.Printf("  Status: %d req, %d OK, %d blocked, %d CAPTCHA | %d items | avoidance %.0f%%\n",
		b.Requests, b.Success, b.Blocked, b.CaptchaHit, b.Items, b.BlockAvoidance)
	for i, it := range items {
		detail := it.Title
		if it.Price != "" {
			detail += " — " + it.Price
		}
		if it.Rating != "" {
			detail += " (" + it.Rating + "★)"
		}
		if it.Supplier != "" {
			detail += " [" + it.Supplier + "]"
		}
		fmt.Printf("  [%d] %s\n", i+1, detail)
	}
}

func humanPause(minSec, maxSec int) {
	d := time.Duration(minSec*1000+rand.IntN((maxSec-minSec)*1000)) * time.Millisecond
	slog.Info("human pause", "delay", d)
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
