//go:build playwright

// camoufox_final — test Google SERP, Google Maps, Yoga Alliance
// with real Camoufox binary + rotating residential proxy
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

const proxyURL = "http://REDACTED_USER:REDACTED_PASS@REDACTED_HOST:80"

type Item struct {
	Title   string `json:"title,omitempty"`
	URL     string `json:"url,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	Rating  string `json:"rating,omitempty"`
	Reviews string `json:"reviews,omitempty"`
	Address string `json:"address,omitempty"`
	Location string `json:"location,omitempty"`
	Source  string `json:"source"`
}

type Benchmark struct {
	Target  string `json:"target"`
	Reqs    int    `json:"requests"`
	OK      int    `json:"success"`
	Blocked int    `json:"blocked"`
	Captcha int    `json:"captcha"`
	Items   int    `json:"items"`
	Ms      int64  `json:"duration_ms"`
	Bytes   int64  `json:"bytes"`
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

	fmt.Println("══════════════════════════════════════════════════════")
	fmt.Println("  Camoufox Binary + Rotating Residential Proxy")
	fmt.Printf("  Proxy: %s\n", "http://***-rotate@REDACTED_HOST:80")
	fmt.Printf("  UA:    %s\n", prof.UA)
	fmt.Println("══════════════════════════════════════════════════════")

	cf, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBlockImages(false),
		fetch.WithHeadless("false"),
		fetch.WithBrowserProxy(proxyURL),
		fetch.WithBrowserTimeout(90*time.Second),
		fetch.WithPersistSession(false), // fresh context per target = new proxy IP
	)
	if err != nil {
		slog.Error("launch failed", "err", err)
		os.Exit(1)
	}
	defer cf.Close()

	var benchmarks []Benchmark

	// ═══════════════════════════
	// 1. GOOGLE SERP
	// ═══════════════════════════
	fmt.Println("\n[1/3] Google SERP — wisata alam jawa timur")
	b1, items1 := scrape(cf, "Google SERP",
		"https://www.google.com/search?q=wisata+alam+jawa+timur&hl=id&gl=id&num=10",
		parseGoogleSERP, 3)
	benchmarks = append(benchmarks, b1)
	saveJSON("tests/results/camoufox_google_serp.json", items1)
	pause()

	// ═══════════════════════════
	// 2. GOOGLE MAPS
	// ═══════════════════════════
	fmt.Println("\n[2/3] Google Maps — villa di bali")
	b2, items2 := scrape(cf, "Google Maps",
		"https://www.google.com/maps/search/villa+di+bali/",
		parseGoogleMaps, 2)
	benchmarks = append(benchmarks, b2)
	saveJSON("tests/results/camoufox_google_maps.json", items2)
	pause()

	// ═══════════════════════════
	// 3. YOGA ALLIANCE
	// ═══════════════════════════
	fmt.Println("\n[3/3] Yoga Alliance — school directory")
	b3, items3 := scrape(cf, "Yoga Alliance",
		"https://app.yogaalliance.org/directoryregistrants?type=School",
		parseYogaAlliance, 3)
	benchmarks = append(benchmarks, b3)
	saveJSON("tests/results/camoufox_yoga_alliance.json", items3)

	// ═══════════════════════════
	// REPORT
	// ═══════════════════════════
	fmt.Println("\n\n══════════════════════════════════════════════════════════════")
	fmt.Println("FINAL REPORT — Camoufox Binary + Residential Proxy")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Printf("%-20s | %4s | %3s | %5s | %5s | %5s\n", "Target", "Reqs", "OK", "Block", "CAPT", "Items")
	fmt.Println("──────────────────────────────────────────────────────────────")
	tR, tOK, tB, tC, tI := 0, 0, 0, 0, 0
	for _, b := range benchmarks {
		tR += b.Reqs; tOK += b.OK; tB += b.Blocked; tC += b.Captcha; tI += b.Items
		fmt.Printf("%-20s | %4d | %3d | %5d | %5d | %5d\n",
			b.Target, b.Reqs, b.OK, b.Blocked, b.Captcha, b.Items)
	}
	fmt.Println("──────────────────────────────────────────────────────────────")
	fmt.Printf("%-20s | %4d | %3d | %5d | %5d | %5d\n", "TOTAL", tR, tOK, tB, tC, tI)
	fmt.Println("══════════════════════════════════════════════════════════════")

	saveJSON("tests/results/camoufox_final_benchmark.json", benchmarks)
}

type parseFn func(*foxhound.Response) []Item

func scrape(f foxhound.Fetcher, name, url string, parser parseFn, maxRetries int) (Benchmark, []Item) {
	b := Benchmark{Target: name}
	start := time.Now()

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(3000+rand.IntN(5000)) * time.Millisecond
			slog.Info("retrying with new proxy IP", "target", name, "attempt", attempt, "wait", wait)
			time.Sleep(wait)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		resp, err := f.Fetch(ctx, &foxhound.Job{
			ID: fmt.Sprintf("%s-%d", name, attempt), URL: url, Method: "GET",
			FetchMode: foxhound.FetchBrowser,
		})
		cancel()
		b.Reqs++

		if err != nil {
			b.Blocked++
			slog.Error("fetch error", "target", name, "err", err)
			continue
		}
		b.Bytes += int64(len(resp.Body))

		if resp.StatusCode >= 400 {
			b.Blocked++
			slog.Warn("HTTP error", "target", name, "status", resp.StatusCode)
			continue
		}
		b.OK++
		slog.Info("fetched", "target", name, "status", resp.StatusCode, "bytes", len(resp.Body))

		// Save debug HTML
		debugPath := fmt.Sprintf("tests/results/camoufox_%s_attempt%d.html",
			strings.ReplaceAll(strings.ToLower(name), " ", "_"), attempt)
		os.WriteFile(debugPath, resp.Body, 0644)

		// Check CAPTCHA
		if det := captcha.Detect(resp); det.Type != captcha.CaptchaNone {
			b.Captcha++
			slog.Warn("CAPTCHA", "target", name, "type", det.Type)
			continue // retry with new proxy IP
		}

		items := parser(resp)
		if len(items) > 0 {
			b.Items = len(items)
			b.Ms = time.Since(start).Milliseconds()
			fmt.Printf("  ✓ %d items on attempt %d\n", len(items), attempt+1)
			for i, it := range items {
				line := it.Title
				if it.Rating != "" { line += " (" + it.Rating + "★)" }
				if it.Address != "" { line += " — " + it.Address }
				if it.Location != "" { line += " @ " + it.Location }
				if it.URL != "" && len(it.URL) < 80 { line += "\n      " + it.URL }
				fmt.Printf("  [%d] %s\n", i+1, line)
			}
			return b, items
		}
		slog.Warn("0 items parsed", "target", name, "bytes", len(resp.Body))
	}

	b.Ms = time.Since(start).Milliseconds()
	fmt.Printf("  ✗ 0 items after %d attempts\n", b.Reqs)
	return b, nil
}

func parseGoogleSERP(resp *foxhound.Response) []Item {
	doc, err := parse.NewDocument(resp)
	if err != nil { return nil }
	var items []Item
	seen := map[string]bool{}
	for _, sel := range []string{"div.g", "div.MjjYud div.g", "div[data-hveid]"} {
		doc.Each(sel, func(_ int, s *goquery.Selection) {
			title := strings.TrimSpace(s.Find("h3").Text())
			href, _ := s.Find("a").First().Attr("href")
			snippet := strings.TrimSpace(s.Find("div.VwiC3b, span.aCOpRe, div[data-sncf]").Text())
			if title == "" || href == "" || strings.HasPrefix(href, "/search") || strings.HasPrefix(href, "#") { return }
			if strings.HasPrefix(href, "/url?q=") {
				href = strings.SplitN(strings.TrimPrefix(href, "/url?q="), "&", 2)[0]
			}
			if seen[title] { return }
			seen[title] = true
			items = append(items, Item{Title: title, URL: href, Snippet: snippet, Source: "google_serp"})
		})
		if len(items) > 0 { break }
	}
	return items
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
		// Skip non-place items
		if strings.Contains(strings.ToLower(name), "input tools") ||
			strings.Contains(strings.ToLower(name), "reklam") ||
			strings.Contains(strings.ToLower(name), "iklan") { return }
		seen[name] = true
		rating := strings.TrimSpace(s.Find("span.MW4etd").Text())
		reviews := strings.TrimSpace(s.Find("span.UY7F9").Text())
		addr := ""
		s.Find("div.W4Efsd").Each(func(_ int, d *goquery.Selection) {
			t := strings.TrimSpace(d.Text())
			if strings.Contains(t, "·") || strings.Contains(t, ",") {
				addr = t
			}
		})
		items = append(items, Item{Title: name, Rating: rating, Reviews: reviews, Address: addr, Source: "google_maps"})
	})
	return items
}

func parseYogaAlliance(resp *foxhound.Response) []Item {
	doc, err := parse.NewDocument(resp)
	if err != nil { return nil }
	var items []Item
	// Strategy 1: card/list elements
	doc.Each("div[class*='card'], div[class*='registrant'], article, a[href*='school'], a[href*='profile']", func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find("h3, h4, h5, strong, a[class*='title'], span[class*='name']").First().Text())
		if name == "" { name = strings.TrimSpace(s.Text()) }
		loc := strings.TrimSpace(s.Find("span[class*='location'], span[class*='city'], p").First().Text())
		if name == "" || len(name) < 4 || len(name) > 200 { return }
		if isNavText(name) || len(items) >= 10 { return }
		items = append(items, Item{Title: name, Location: loc, Source: "yoga_alliance"})
	})
	// Strategy 2: table rows
	if len(items) == 0 {
		doc.Each("table tbody tr, div[role='row']", func(_ int, s *goquery.Selection) {
			cells := s.Find("td, div[role='cell']")
			if cells.Length() >= 2 {
				name := strings.TrimSpace(cells.First().Text())
				loc := strings.TrimSpace(cells.Eq(1).Text())
				if name != "" && len(name) > 4 && !isNavText(name) && len(items) < 10 {
					items = append(items, Item{Title: name, Location: loc, Source: "yoga_alliance"})
				}
			}
		})
	}
	return items
}

func isNavText(s string) bool {
	l := strings.ToLower(s)
	for _, w := range []string{"cookie", "privacy", "menu", "login", "sign", "search", "filter", "virtual", "upcoming", "home", "about", "contact", "help", "faq", "copyright", "terms"} {
		if strings.Contains(l, w) { return true }
	}
	return false
}

func pause() {
	d := time.Duration(5000+rand.IntN(5000)) * time.Millisecond
	slog.Info("pause", "delay", d)
	time.Sleep(d)
}

func saveJSON(path string, v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	os.WriteFile(path, data, 0644)
}
