// Scrape Target 4: Yoga Alliance School Directory
//
// https://app.yogaalliance.org/directoryregistrants?type=School
//
// The Yoga Alliance directory is a React SPA. The initial server-side render
// may include a pre-populated list in the HTML (Next.js/SSR) or nothing at all
// (client-side-only render). This scraper:
//
//  1. Attempts a static fetch and tries multiple selector/JSON extraction strategies.
//  2. Falls back to the public API endpoint that the SPA calls internally.
//  3. Tries an alternative static-friendly URL variant.
//
// Up to 10 school entries are extracted and written to tests/results/.
//
// Run:
//
//	go run ./tests/scrape_targets/yoga_alliance/
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
	// Primary: SPA directory page — may have SSR data.
	primaryURL = "https://app.yogaalliance.org/directoryregistrants?type=School"
	// API endpoint used internally by the React app.
	apiURL = "https://app.yogaalliance.org/api/directory/registrants?type=School&page=1&per_page=10"
	// Alternative: static directory search page.
	altURL     = "https://www.yogaalliance.org/Find_a_School"
	maxSchools = 10
	resultsDir = "tests/results"
	outputFile = "tests/results/yoga_alliance.json"
	benchFile  = "tests/results/yoga_alliance_benchmark.json"
	targetName = "Yoga Alliance: school directory"
)

// School holds one extracted school entry.
type School struct {
	Position int    `json:"position"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Type     string `json:"type"`
	URL      string `json:"url,omitempty"`
	Source   string `json:"source"`
}

// RequestRecord captures per-request timing and outcome.
type RequestRecord struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	DurationMS int64  `json:"duration_ms"`
	Blocked    bool   `json:"blocked"`
	BytesRead  int    `json:"bytes_read"`
	Strategy   string `json:"strategy"`
}

// Benchmark holds aggregated metrics.
type Benchmark struct {
	Target         string          `json:"target"`
	TargetURL      string          `json:"target_url"`
	TotalRequests  int             `json:"total_requests"`
	Successful     int             `json:"successful"`
	Blocked        int             `json:"blocked"`
	Items          int             `json:"items"`
	DurationMS     int64           `json:"duration_ms"`
	AvgLatencyMS   int64           `json:"avg_latency_ms"`
	ThroughputIPS  float64         `json:"throughput_items_per_sec"`
	BytesReceived  int64           `json:"bytes_received"`
	BlockAvoidance float64         `json:"block_avoidance_pct"`
	Requests       []RequestRecord `json:"requests"`
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// ------------------------------------------------------------------
	// 1. Identity: US English — Yoga Alliance is a US non-profit; US
	//    locale and timezone match the expected visitor profile.
	// ------------------------------------------------------------------
	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSMacOS),
		identity.WithLocale("en-US", "en-US", "en"),
		identity.WithTimezone("America/Los_Angeles"),
		identity.WithGeo(34.05, -118.24), // Los Angeles
	)
	slog.Info("identity generated", "ua", prof.UA, "locale", prof.Locale)

	fetcher := fetch.NewStealth(
		fetch.WithIdentity(prof),
		fetch.WithTimeout(30*time.Second),
	)
	defer fetcher.Close()

	var bench Benchmark
	bench.Target = targetName
	bench.TargetURL = primaryURL

	runStart := time.Now()
	ctx := context.Background()

	var allSchools []School

	// ------------------------------------------------------------------
	// 2. Strategy 1: static fetch of the SPA page.
	//    Next.js / SSR apps often embed __NEXT_DATA__ or __INITIAL_STATE__
	//    in a <script> tag. We check for that before trying CSS selectors.
	// ------------------------------------------------------------------
	slog.Info("strategy 1: static fetch of SPA page", "url", primaryURL)
	rec1, resp1 := doFetch(ctx, fetcher, primaryURL, "ya-spa", "spa-static")
	bench.Requests = append(bench.Requests, rec1)
	bench.TotalRequests++
	bench.BytesReceived += int64(rec1.BytesRead)

	if rec1.Blocked || resp1 == nil {
		bench.Blocked++
		slog.Warn("SPA static fetch blocked/failed")
	} else {
		bench.Successful++
		schools := parseSPAPage(resp1)
		allSchools = append(allSchools, schools...)
		slog.Info("SPA parse complete", "schools_found", len(schools))
	}

	time.Sleep(2 * time.Second)

	// ------------------------------------------------------------------
	// 3. Strategy 2: internal API endpoint (JSON response).
	// ------------------------------------------------------------------
	if len(allSchools) < maxSchools {
		slog.Info("strategy 2: internal API endpoint", "url", apiURL)
		rec2, resp2 := doFetch(ctx, fetcher, apiURL, "ya-api", "api")
		bench.Requests = append(bench.Requests, rec2)
		bench.TotalRequests++
		bench.BytesReceived += int64(rec2.BytesRead)

		if rec2.Blocked || resp2 == nil {
			bench.Blocked++
			slog.Warn("API fetch blocked/failed")
		} else {
			bench.Successful++
			remaining := maxSchools - len(allSchools)
			schools := parseAPIResponse(resp2, remaining)
			allSchools = append(allSchools, schools...)
			slog.Info("API parse complete", "schools_found", len(schools))

			if len(schools) == 0 {
				snippet := resp2.Body
				if len(snippet) > 512 {
					snippet = snippet[:512]
				}
				slog.Debug("API response snippet", "body", string(snippet))
			}
		}
		time.Sleep(1500 * time.Millisecond)
	}

	// ------------------------------------------------------------------
	// 4. Strategy 3: alternative static directory page on main domain.
	// ------------------------------------------------------------------
	if len(allSchools) < maxSchools {
		slog.Info("strategy 3: static directory page", "url", altURL)
		rec3, resp3 := doFetch(ctx, fetcher, altURL, "ya-alt", "alt-static")
		bench.Requests = append(bench.Requests, rec3)
		bench.TotalRequests++
		bench.BytesReceived += int64(rec3.BytesRead)

		if rec3.Blocked || resp3 == nil {
			bench.Blocked++
			slog.Warn("alt static fetch blocked/failed")
		} else {
			bench.Successful++
			remaining := maxSchools - len(allSchools)
			schools := parseStaticDirectory(resp3, remaining)
			allSchools = append(allSchools, schools...)
			slog.Info("alt static parse complete", "schools_found", len(schools))
		}
		time.Sleep(1500 * time.Millisecond)
	}

	// Cap at maxSchools.
	if len(allSchools) > maxSchools {
		allSchools = allSchools[:maxSchools]
	}
	bench.Items = len(allSchools)

	// ------------------------------------------------------------------
	// 5. Compute metrics.
	// ------------------------------------------------------------------
	bench.DurationMS = time.Since(runStart).Milliseconds()
	if bench.TotalRequests > 0 {
		bench.AvgLatencyMS = bench.DurationMS / int64(bench.TotalRequests)
		bench.BlockAvoidance = float64(bench.Successful) / float64(bench.TotalRequests) * 100
	}
	durationSec := float64(bench.DurationMS) / 1000.0
	if durationSec > 0 && bench.Items > 0 {
		bench.ThroughputIPS = float64(bench.Items) / durationSec
	}

	// ------------------------------------------------------------------
	// 6. Write results.
	// ------------------------------------------------------------------
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		slog.Error("cannot create results dir", "err", err)
	}
	writeJSON(outputFile, allSchools)
	writeJSON(benchFile, bench)

	// ------------------------------------------------------------------
	// 7. Print benchmark.
	// ------------------------------------------------------------------
	printBenchmark(bench)
}

// doFetch executes a single GET request and returns a RequestRecord + Response.
func doFetch(ctx context.Context, fetcher foxhound.Fetcher, rawURL, jobID, strategy string) (RequestRecord, *foxhound.Response) {
	headers := http.Header{
		"Referer": []string{"https://www.yogaalliance.org/"},
	}
	// When hitting the API endpoint, signal that we accept JSON.
	if strings.Contains(rawURL, "/api/") {
		headers.Set("Accept", "application/json, text/plain, */*")
		headers.Set("X-Requested-With", "XMLHttpRequest")
	}

	job := &foxhound.Job{
		ID:        jobID,
		URL:       rawURL,
		Method:    http.MethodGet,
		FetchMode: foxhound.FetchStatic,
		Priority:  foxhound.PriorityNormal,
		Headers:   headers,
		CreatedAt: time.Now(),
	}

	reqCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := fetcher.Fetch(reqCtx, job)
	dur := time.Since(start)

	rec := RequestRecord{
		URL:        rawURL,
		DurationMS: dur.Milliseconds(),
		Strategy:   strategy,
	}

	if err != nil {
		slog.Error("fetch error", "url", rawURL, "strategy", strategy, "err", err)
		rec.Blocked = true
		return rec, nil
	}

	rec.StatusCode = resp.StatusCode
	rec.BytesRead = len(resp.Body)
	slog.Info("fetch complete", "url", rawURL, "status", resp.StatusCode,
		"bytes", len(resp.Body), "duration_ms", dur.Milliseconds(), "strategy", strategy)

	if resp.StatusCode != http.StatusOK {
		slog.Warn("non-200 — counted as block", "status", resp.StatusCode, "url", rawURL)
		rec.Blocked = true
	}

	return rec, resp
}

// parseSPAPage attempts to extract school data from a Next.js / React SPA page.
// It first checks for __NEXT_DATA__ JSON injection, then falls back to CSS selectors.
func parseSPAPage(resp *foxhound.Response) []School {
	var schools []School

	// ---- A. __NEXT_DATA__ JSON blob ----
	doc, err := parse.NewDocument(resp)
	if err != nil {
		slog.Error("HTML parse error", "err", err)
		return nil
	}

	nextDataRaw := strings.TrimSpace(doc.Text("#__NEXT_DATA__"))
	if nextDataRaw != "" {
		slog.Info("found __NEXT_DATA__ — attempting JSON extraction")
		schools = extractFromNextData(nextDataRaw)
		if len(schools) > 0 {
			return schools
		}
	}

	// ---- B. Generic JSON-LD structured data ----
	doc.Each("script[type='application/ld+json']", func(_ int, s *goquery.Selection) {
		var obj map[string]any
		if err := json.Unmarshal([]byte(s.Text()), &obj); err != nil {
			return
		}
		if obj["@type"] != "ItemList" {
			return
		}
		items, _ := obj["itemListElement"].([]any)
		pos := len(schools)
		for _, it := range items {
			m, ok := it.(map[string]any)
			if !ok {
				break
			}
			name, _ := m["name"].(string)
			if name == "" {
				continue
			}
			loc := ""
			if addr, ok := m["address"].(map[string]any); ok {
				city, _ := addr["addressLocality"].(string)
				state, _ := addr["addressRegion"].(string)
				loc = strings.TrimSpace(city + ", " + state)
			}
			url, _ := m["url"].(string)
			pos++
			schools = append(schools, School{
				Position: pos,
				Name:     name,
				Location: loc,
				URL:      url,
				Source:   resp.URL,
			})
		}
	})
	if len(schools) > 0 {
		return schools
	}

	// ---- C. CSS selectors for server-rendered cards ----
	// The directory app uses class names like "school-card", "registrant-card", etc.
	cardSelectors := []string{
		".school-card",
		".registrant-card",
		"[class*='school-item']",
		"[class*='directory-item']",
		"li[class*='result']",
	}
	pos := 0
	for _, sel := range cardSelectors {
		doc.Each(sel, func(_ int, s *goquery.Selection) {
			name := strings.TrimSpace(s.Find("h2, h3, [class*='name'], [class*='title']").First().Text())
			if name == "" {
				return
			}
			loc := strings.TrimSpace(s.Find("[class*='location'], [class*='address'], [class*='city']").First().Text())
			typ := strings.TrimSpace(s.Find("[class*='type'], [class*='style']").First().Text())
			href, _ := s.Find("a[href]").First().Attr("href")
			pos++
			schools = append(schools, School{
				Position: pos,
				Name:     name,
				Location: loc,
				Type:     typ,
				URL:      href,
				Source:   resp.URL,
			})
		})
		if len(schools) > 0 {
			break
		}
	}

	if len(schools) == 0 {
		slog.Warn("no schools from SPA page — site is likely client-side-only rendered")
	}
	return schools
}

// extractFromNextData parses Next.js __NEXT_DATA__ JSON to find school records.
// The structure varies by app version; we walk common paths.
func extractFromNextData(raw string) []School {
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		slog.Error("__NEXT_DATA__ parse error", "err", err)
		return nil
	}

	// Walk: props -> pageProps -> registrants / schools / items / data
	props, _ := root["props"].(map[string]any)
	if props == nil {
		return nil
	}
	pageProps, _ := props["pageProps"].(map[string]any)
	if pageProps == nil {
		return nil
	}

	for _, key := range []string{"registrants", "schools", "items", "data", "results"} {
		raw, ok := pageProps[key]
		if !ok {
			continue
		}
		list, ok := raw.([]any)
		if !ok {
			continue
		}
		var schools []School
		for i, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name := stringField(m, "name", "school_name", "title", "registrant_name")
			if name == "" {
				continue
			}
			loc := buildLocation(m)
			typ := stringField(m, "type", "school_type", "style", "program_type")
			url := stringField(m, "url", "permalink", "profile_url")
			schools = append(schools, School{
				Position: i + 1,
				Name:     name,
				Location: loc,
				Type:     typ,
				URL:      url,
				Source:   "__NEXT_DATA__",
			})
		}
		if len(schools) > 0 {
			slog.Info("extracted schools from __NEXT_DATA__", "key", key, "count", len(schools))
			return schools
		}
	}

	return nil
}

// parseAPIResponse unmarshals a JSON API response from the Yoga Alliance API.
func parseAPIResponse(resp *foxhound.Response, limit int) []School {
	// Try array of objects.
	var list []map[string]any
	if err := json.Unmarshal(resp.Body, &list); err == nil {
		return schoolsFromList(list, limit, resp.URL)
	}

	// Try wrapped response: {"data": [...], "registrants": [...], ...}
	var wrapper map[string]any
	if err := json.Unmarshal(resp.Body, &wrapper); err != nil {
		slog.Warn("API response is not JSON", "content_type",
			resp.Headers.Get("Content-Type"))
		return nil
	}

	for _, key := range []string{"data", "registrants", "schools", "items", "results"} {
		raw, ok := wrapper[key]
		if !ok {
			continue
		}
		list, ok := raw.([]any)
		if !ok {
			continue
		}
		var maps []map[string]any
		for _, v := range list {
			if m, ok := v.(map[string]any); ok {
				maps = append(maps, m)
			}
		}
		schools := schoolsFromList(maps, limit, resp.URL)
		if len(schools) > 0 {
			slog.Info("extracted schools from API", "key", key, "count", len(schools))
			return schools
		}
	}

	slog.Warn("API response has no recognisable school list")
	return nil
}

// schoolsFromList converts a []map[string]any slice (from API JSON) into Schools.
func schoolsFromList(list []map[string]any, limit int, sourceURL string) []School {
	var schools []School
	for i, m := range list {
		if len(schools) >= limit {
			break
		}
		name := stringField(m, "name", "school_name", "title", "registrant_name")
		if name == "" {
			continue
		}
		loc := buildLocation(m)
		typ := stringField(m, "type", "school_type", "style", "program_type")
		url := stringField(m, "url", "permalink", "profile_url")
		schools = append(schools, School{
			Position: i + 1,
			Name:     name,
			Location: loc,
			Type:     typ,
			URL:      url,
			Source:   sourceURL,
		})
	}
	return schools
}

// parseStaticDirectory extracts schools from a traditional server-rendered
// directory page on the yogaalliance.org main domain.
func parseStaticDirectory(resp *foxhound.Response, limit int) []School {
	doc, err := parse.NewDocument(resp)
	if err != nil {
		slog.Error("HTML parse error", "err", err)
		return nil
	}

	var schools []School
	pos := 0

	// Common patterns for static directory tables/lists.
	selectors := []string{
		"table.directory tbody tr",
		".school-listing",
		".directory-result",
		"ul.results li",
		"div.views-row",
	}

	for _, sel := range selectors {
		doc.Each(sel, func(_ int, s *goquery.Selection) {
			if pos >= limit {
				return
			}
			name := strings.TrimSpace(s.Find("a, h3, h4, td:first-child").First().Text())
			if name == "" {
				return
			}
			loc := strings.TrimSpace(s.Find("td:nth-child(2), [class*='location']").First().Text())
			typ := strings.TrimSpace(s.Find("td:nth-child(3), [class*='type']").First().Text())
			href, _ := s.Find("a[href]").First().Attr("href")
			pos++
			schools = append(schools, School{
				Position: pos,
				Name:     name,
				Location: loc,
				Type:     typ,
				URL:      href,
				Source:   resp.URL,
			})
		})
		if len(schools) > 0 {
			break
		}
	}

	if len(schools) == 0 {
		slog.Warn("no schools from static directory page", "url", resp.URL)
	}

	return schools
}

// stringField returns the first non-empty string value found in m at any of keys.
func stringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// buildLocation constructs a city+state/country string from a flat or nested map.
func buildLocation(m map[string]any) string {
	// Flat keys first.
	if loc := stringField(m, "location", "city_state", "full_address"); loc != "" {
		return loc
	}
	// Nested address object.
	if addr, ok := m["address"].(map[string]any); ok {
		city := stringField(addr, "city", "locality")
		state := stringField(addr, "state", "region", "province")
		country := stringField(addr, "country", "country_name")
		parts := []string{}
		if city != "" {
			parts = append(parts, city)
		}
		if state != "" {
			parts = append(parts, state)
		}
		if country != "" {
			parts = append(parts, country)
		}
		return strings.Join(parts, ", ")
	}
	// Flat city/state fields.
	city := stringField(m, "city")
	state := stringField(m, "state", "province", "region")
	country := stringField(m, "country")
	parts := []string{}
	if city != "" {
		parts = append(parts, city)
	}
	if state != "" {
		parts = append(parts, state)
	}
	if country != "" {
		parts = append(parts, country)
	}
	return strings.Join(parts, ", ")
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
