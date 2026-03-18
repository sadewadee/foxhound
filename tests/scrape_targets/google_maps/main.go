// Scrape Target 2: Google Maps — "villa di bali"
//
// Google Maps is JS-rendered; a static HTTP fetch returns either the pre-render
// skeleton or a consent/redirect page rather than populated place cards.
//
// Strategy:
//  1. Primary attempt: Google local-results search page with tbm=lcl, which is
//     more static-friendly than the full Maps SPA and often contains structured
//     JSON-LD or visible place data.
//  2. Secondary attempt: a plain web search with "villa di bali" site:maps.google
//     is tried when the local results page yields nothing — this is used only to
//     measure block rates, not to parse real place data.
//
// Both attempts are benchmarked together. Each request is tracked individually
// so the block avoidance metric is meaningful.
//
// Run:
//
//	go run ./tests/scrape_targets/google_maps/
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
	// Primary: local results view — more static-friendly than the Maps SPA.
	primaryURL = "https://www.google.com/search?q=villa+di+bali&tbm=lcl&hl=id&gl=id"
	// Fallback: plain search; used only for block-rate measurement.
	fallbackURL  = "https://www.google.com/maps/search/villa+di+bali/"
	resultsDir   = "tests/results"
	outputFile   = "tests/results/google_maps.json"
	benchFile    = "tests/results/google_maps_benchmark.json"
	targetName   = "Google Maps: villa di bali"
)

// PlaceResult holds one extracted place listing.
type PlaceResult struct {
	Position    int    `json:"position"`
	Name        string `json:"name"`
	Rating      string `json:"rating"`
	ReviewCount string `json:"review_count"`
	Address     string `json:"address"`
	Category    string `json:"category"`
	Source      string `json:"source"` // which URL this came from
}

// RequestRecord captures per-request timing and outcome for the benchmark.
type RequestRecord struct {
	URL        string        `json:"url"`
	StatusCode int           `json:"status_code"`
	DurationMS int64         `json:"duration_ms"`
	Blocked    bool          `json:"blocked"`
	BytesRead  int           `json:"bytes_read"`
}

// Benchmark holds aggregated metrics across all requests in the run.
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
	// 1. Identity: Balinese locale, Indonesian timezone — matches the
	//    search query locale so headers are internally consistent.
	// ------------------------------------------------------------------
	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
		identity.WithLocale("id-ID", "id-ID", "id", "en"),
		identity.WithTimezone("Asia/Makassar"), // Bali is on WITA
		identity.WithGeo(-8.34, 115.09),        // Denpasar, Bali
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

	var allPlaces []PlaceResult

	// ------------------------------------------------------------------
	// 2. Primary request: Google local results (tbm=lcl).
	// ------------------------------------------------------------------
	primaryRecord, primaryResp := doFetch(ctx, fetcher, primaryURL, "google-maps-primary")
	bench.Requests = append(bench.Requests, primaryRecord)
	bench.TotalRequests++
	bench.BytesReceived += int64(primaryRecord.BytesRead)

	if primaryRecord.Blocked || primaryResp == nil {
		bench.Blocked++
		slog.Warn("primary request blocked or failed", "url", primaryURL)
	} else {
		bench.Successful++
		places := parseLocalResults(primaryResp, primaryURL)
		allPlaces = append(allPlaces, places...)
		slog.Info("primary parse complete", "places_found", len(places))
	}

	// Human-like delay between requests.
	time.Sleep(2 * time.Second)

	// ------------------------------------------------------------------
	// 3. Fallback request: raw Maps SPA URL.
	//    We still attempt this to measure whether the Maps URL is blocked
	//    independently of the search page. Results here will typically be
	//    empty (JS-rendered), but the HTTP outcome is still recorded.
	// ------------------------------------------------------------------
	if len(allPlaces) == 0 {
		slog.Info("primary yielded no places, attempting fallback", "url", fallbackURL)
		fallbackRecord, fallbackResp := doFetch(ctx, fetcher, fallbackURL, "google-maps-fallback")
		bench.Requests = append(bench.Requests, fallbackRecord)
		bench.TotalRequests++
		bench.BytesReceived += int64(fallbackRecord.BytesRead)

		if fallbackRecord.Blocked || fallbackResp == nil {
			bench.Blocked++
			slog.Warn("fallback request blocked or failed", "url", fallbackURL)
		} else {
			bench.Successful++
			// Maps SPA skeleton — parse what little structure is present.
			places := parseLocalResults(fallbackResp, fallbackURL)
			allPlaces = append(allPlaces, places...)
			slog.Info("fallback parse complete", "places_found", len(places))

			if len(places) == 0 {
				snippet := fallbackResp.Body
				if len(snippet) > 1024 {
					snippet = snippet[:1024]
				}
				slog.Debug("fallback body snippet (JS-rendered page, likely empty)",
					"snippet", string(snippet),
				)
			}
		}

		time.Sleep(1500 * time.Millisecond)
	}

	bench.Items = len(allPlaces)

	// ------------------------------------------------------------------
	// 4. Compute metrics.
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
	// 5. Write results.
	// ------------------------------------------------------------------
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		slog.Error("cannot create results dir", "err", err)
	}
	writeJSON(outputFile, allPlaces)
	writeJSON(benchFile, bench)

	// ------------------------------------------------------------------
	// 6. Print benchmark.
	// ------------------------------------------------------------------
	printBenchmark(bench)
}

// doFetch performs a single GET with standard browser headers and returns a
// RequestRecord plus the raw Response (nil on transport error).
func doFetch(ctx context.Context, fetcher foxhound.Fetcher, rawURL, jobID string) (RequestRecord, *foxhound.Response) {
	job := &foxhound.Job{
		ID:        jobID,
		URL:       rawURL,
		Method:    http.MethodGet,
		FetchMode: foxhound.FetchStatic,
		Priority:  foxhound.PriorityNormal,
		Headers: http.Header{
			"Accept-Language": []string{"id-ID,id;q=0.9,en;q=0.7"},
			"Referer":         []string{"https://www.google.com/"},
		},
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
	}

	if err != nil {
		slog.Error("fetch error", "url", rawURL, "err", err, "duration_ms", dur.Milliseconds())
		rec.Blocked = true
		return rec, nil
	}

	rec.StatusCode = resp.StatusCode
	rec.BytesRead = len(resp.Body)
	slog.Info("fetch complete", "url", rawURL, "status", resp.StatusCode,
		"bytes", len(resp.Body), "duration_ms", dur.Milliseconds())

	if resp.StatusCode != http.StatusOK {
		slog.Warn("non-200 — counted as block", "status", resp.StatusCode)
		rec.Blocked = true
	}

	return rec, resp
}

// parseLocalResults attempts to extract place data from a Google local-results
// or Maps response using several CSS selector strategies.
func parseLocalResults(resp *foxhound.Response, sourceURL string) []PlaceResult {
	doc, err := parse.NewDocument(resp)
	if err != nil {
		slog.Error("HTML parse error", "err", err, "url", sourceURL)
		return nil
	}

	var results []PlaceResult
	pos := 0

	// Strategy A: Local results pack (tbm=lcl) — div.VkpGBb wrappers.
	doc.Each("div.VkpGBb", func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find("div.dbg0pd span").First().Text())
		if name == "" {
			name = strings.TrimSpace(s.Find("span[role='heading']").First().Text())
		}
		if name == "" {
			return
		}
		rating := strings.TrimSpace(s.Find("span.yi40Hd").First().Text())
		reviews := strings.TrimSpace(s.Find("span.RDApEe").First().Text())
		addr := strings.TrimSpace(s.Find("span.rllt__details div").Last().Text())
		cat := strings.TrimSpace(s.Find("div.rllt__details div").First().Text())
		pos++
		results = append(results, PlaceResult{
			Position:    pos,
			Name:        name,
			Rating:      rating,
			ReviewCount: reviews,
			Address:     addr,
			Category:    cat,
			Source:      sourceURL,
		})
	})

	// Strategy B: Local knowledge panel items — div[data-attrid="kc:/local:..."].
	if len(results) == 0 {
		doc.Each("div[jscontroller]", func(_ int, s *goquery.Selection) {
			heading := strings.TrimSpace(s.Find("h3, [role='heading']").First().Text())
			if heading == "" {
				return
			}
			rating := strings.TrimSpace(s.Find("[aria-label*='stars']").First().Text())
			addr := strings.TrimSpace(s.Find("span[jsl]").First().Text())
			pos++
			results = append(results, PlaceResult{
				Position: pos,
				Name:     heading,
				Rating:   rating,
				Address:  addr,
				Source:   sourceURL,
			})
		})
	}

	// Strategy C: JSON-LD structured data embedded in the page.
	if len(results) == 0 {
		doc.Each("script[type='application/ld+json']", func(_ int, s *goquery.Selection) {
			raw := s.Text()
			if !strings.Contains(raw, "LodgingBusiness") && !strings.Contains(raw, "Hotel") {
				return
			}
			var ld map[string]any
			if err := json.Unmarshal([]byte(raw), &ld); err != nil {
				return
			}
			name, _ := ld["name"].(string)
			if name == "" {
				return
			}
			addr := ""
			if addrObj, ok := ld["address"].(map[string]any); ok {
				addr, _ = addrObj["streetAddress"].(string)
			}
			rating := ""
			if agg, ok := ld["aggregateRating"].(map[string]any); ok {
				rv, _ := agg["ratingValue"].(string)
				rating = rv
			}
			pos++
			results = append(results, PlaceResult{
				Position: pos,
				Name:     name,
				Rating:   rating,
				Address:  addr,
				Source:   sourceURL,
			})
		})
	}

	if len(results) == 0 {
		slog.Warn("no place results parsed — Google Maps is JS-rendered; static fetch yields skeleton only",
			"url", sourceURL,
		)
	}

	return results
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
