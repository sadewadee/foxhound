// Scrape Target 3: Alibaba — 10 yoga mat products
//
// Alibaba uses Cloudflare-like bot protection. The scraper attempts:
//  1. Primary: standard trade search URL with Firefox stealth headers.
//  2. Fallback: alternative URL pattern without JS-required parameters.
//
// Product data extracted per listing: title, price range, supplier, MOQ.
// Up to 10 products are collected; the run stops once that quota is reached.
//
// Run:
//
//	go run ./tests/scrape_targets/alibaba/
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
	primaryURL  = "https://www.alibaba.com/trade/search?SearchText=yoga+mat&page=1"
	fallbackURL = "https://www.alibaba.com/products/yoga_mat.html"
	maxProducts = 10
	resultsDir  = "tests/results"
	outputFile  = "tests/results/alibaba.json"
	benchFile   = "tests/results/alibaba_benchmark.json"
	targetName  = "Alibaba: yoga mat products"
)

// Product holds one scraped product listing.
type Product struct {
	Position   int    `json:"position"`
	Title      string `json:"title"`
	PriceRange string `json:"price_range"`
	Supplier   string `json:"supplier"`
	MOQ        string `json:"moq"`
	DetailURL  string `json:"detail_url"`
	Source     string `json:"source"`
}

// RequestRecord captures per-request timing and outcome.
type RequestRecord struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	DurationMS int64  `json:"duration_ms"`
	Blocked    bool   `json:"blocked"`
	BytesRead  int    `json:"bytes_read"`
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
	// 1. Identity: US-based buyer profile. Alibaba's primary market is
	//    international B2B buyers from the US/EU — matching the locale
	//    reduces the chance of being shown a challenge page.
	// ------------------------------------------------------------------
	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
		identity.WithLocale("en-US", "en-US", "en"),
		identity.WithTimezone("America/New_York"),
		identity.WithGeo(40.71, -74.00), // New York
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

	var allProducts []Product

	// ------------------------------------------------------------------
	// 2. Primary request.
	// ------------------------------------------------------------------
	slog.Info("attempting primary URL", "url", primaryURL)
	primaryRecord, primaryResp := doFetch(ctx, fetcher, primaryURL, "alibaba-primary")
	bench.Requests = append(bench.Requests, primaryRecord)
	bench.TotalRequests++
	bench.BytesReceived += int64(primaryRecord.BytesRead)

	if primaryRecord.Blocked || primaryResp == nil {
		bench.Blocked++
		slog.Warn("primary blocked or failed", "url", primaryURL)
	} else {
		bench.Successful++
		products := parseAlibabaProducts(primaryResp, primaryURL, maxProducts)
		allProducts = append(allProducts, products...)
		slog.Info("primary parse complete", "products_found", len(products))
	}

	// Human-like delay.
	time.Sleep(2500 * time.Millisecond)

	// ------------------------------------------------------------------
	// 3. Fallback if primary was blocked or returned nothing.
	// ------------------------------------------------------------------
	if len(allProducts) < maxProducts {
		slog.Info("trying fallback URL", "url", fallbackURL,
			"products_so_far", len(allProducts))
		fallbackRecord, fallbackResp := doFetch(ctx, fetcher, fallbackURL, "alibaba-fallback")
		bench.Requests = append(bench.Requests, fallbackRecord)
		bench.TotalRequests++
		bench.BytesReceived += int64(fallbackRecord.BytesRead)

		if fallbackRecord.Blocked || fallbackResp == nil {
			bench.Blocked++
			slog.Warn("fallback blocked or failed", "url", fallbackURL)
		} else {
			bench.Successful++
			remaining := maxProducts - len(allProducts)
			products := parseAlibabaProducts(fallbackResp, fallbackURL, remaining)
			allProducts = append(allProducts, products...)
			slog.Info("fallback parse complete", "products_found", len(products))

			if len(products) == 0 {
				snippet := fallbackResp.Body
				if len(snippet) > 1024 {
					snippet = snippet[:1024]
				}
				slog.Debug("fallback body snippet", "snippet", string(snippet))
			}
		}
		time.Sleep(1500 * time.Millisecond)
	}

	// Cap at maxProducts.
	if len(allProducts) > maxProducts {
		allProducts = allProducts[:maxProducts]
	}
	bench.Items = len(allProducts)

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
	writeJSON(outputFile, allProducts)
	writeJSON(benchFile, bench)

	// ------------------------------------------------------------------
	// 6. Print benchmark.
	// ------------------------------------------------------------------
	printBenchmark(bench)
}

// doFetch executes a single GET and returns a RequestRecord + raw Response.
func doFetch(ctx context.Context, fetcher foxhound.Fetcher, rawURL, jobID string) (RequestRecord, *foxhound.Response) {
	job := &foxhound.Job{
		ID:        jobID,
		URL:       rawURL,
		Method:    http.MethodGet,
		FetchMode: foxhound.FetchStatic,
		Priority:  foxhound.PriorityNormal,
		Headers: http.Header{
			// Alibaba expects a plausible browsing referrer.
			"Referer": []string{"https://www.alibaba.com/"},
			// DNT and Pragma to further match browser fingerprint.
			"DNT":    []string{"1"},
			"Pragma": []string{"no-cache"},
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

	// 403/429/503 are Cloudflare-style blocks; 302 to a challenge page is also common.
	switch resp.StatusCode {
	case http.StatusOK:
		// Fine — parse below.
	case http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable:
		slog.Warn("anti-bot block detected", "status", resp.StatusCode, "url", rawURL)
		rec.Blocked = true
	default:
		slog.Warn("unexpected status — counted as block", "status", resp.StatusCode, "url", rawURL)
		rec.Blocked = true
	}

	return rec, resp
}

// parseAlibabaProducts extracts product cards from an Alibaba search/listing page.
// It tries several selector strategies because Alibaba changes its markup frequently.
// Returns at most limit products.
func parseAlibabaProducts(resp *foxhound.Response, sourceURL string, limit int) []Product {
	doc, err := parse.NewDocument(resp)
	if err != nil {
		slog.Error("HTML parse error", "err", err)
		return nil
	}

	var products []Product
	pos := 0

	addProduct := func(title, price, supplier, moq, href string) {
		if title == "" || pos >= limit {
			return
		}
		pos++
		products = append(products, Product{
			Position:   pos,
			Title:      title,
			PriceRange: price,
			Supplier:   supplier,
			MOQ:        moq,
			DetailURL:  href,
			Source:     sourceURL,
		})
	}

	// Strategy A: organic list items (classic Alibaba search layout).
	// Selector: .organic-list .list-no-v2-outter
	doc.Each(".organic-list .list-no-v2-outter", func(_ int, s *goquery.Selection) {
		if pos >= limit {
			return
		}
		title := strings.TrimSpace(s.Find("h2.title, .product-title, .item-title").First().Text())
		price := strings.TrimSpace(s.Find(".price-range, .price, .price-item").First().Text())
		supplier := strings.TrimSpace(s.Find(".supplier-name a, .company-name a").First().Text())
		moq := strings.TrimSpace(s.Find(".min-order, .moq").First().Text())
		href, _ := s.Find("a[href]").First().Attr("href")
		if !strings.HasPrefix(href, "http") && href != "" {
			href = "https://www.alibaba.com" + href
		}
		addProduct(title, price, supplier, moq, href)
	})

	// Strategy B: J-offer-wrapper items (another common Alibaba layout).
	if len(products) < limit {
		doc.Each(".J-offer-wrapper", func(_ int, s *goquery.Selection) {
			if pos >= limit {
				return
			}
			title := strings.TrimSpace(s.Find(".offer-title, h2, h3").First().Text())
			price := strings.TrimSpace(s.Find(".price, .value").First().Text())
			supplier := strings.TrimSpace(s.Find(".company-name, .supplier").First().Text())
			moq := strings.TrimSpace(s.Find(".min-order").First().Text())
			href, _ := s.Find("a[href]").First().Attr("href")
			if !strings.HasPrefix(href, "http") && href != "" {
				href = "https://www.alibaba.com" + href
			}
			addProduct(title, price, supplier, moq, href)
		})
	}

	// Strategy C: data-trace-log product cards (newer React-rendered layout
	// where server-side HTML still includes basic card markup).
	if len(products) < limit {
		doc.Each("div[data-content='product']", func(_ int, s *goquery.Selection) {
			if pos >= limit {
				return
			}
			title := strings.TrimSpace(s.Find("h2, .product-subject").First().Text())
			price := strings.TrimSpace(s.Find(".price, [class*='price']").First().Text())
			supplier := strings.TrimSpace(s.Find("[class*='company'], [class*='supplier']").First().Text())
			moq := strings.TrimSpace(s.Find("[class*='min-order'], [class*='moq']").First().Text())
			href, _ := s.Find("a[href]").First().Attr("href")
			addProduct(title, price, supplier, moq, href)
		})
	}

	// Strategy D: any <article> with a product-like structure as last resort.
	if len(products) < limit {
		doc.Each("article", func(_ int, s *goquery.Selection) {
			if pos >= limit {
				return
			}
			title := strings.TrimSpace(s.Find("h2, h3, [class*='title']").First().Text())
			price := strings.TrimSpace(s.Find("[class*='price']").First().Text())
			href, _ := s.Find("a[href]").First().Attr("href")
			addProduct(title, price, "", "", href)
		})
	}

	if len(products) == 0 {
		slog.Warn("no products parsed — Alibaba may have blocked the request or changed its layout",
			"url", sourceURL,
		)
	} else {
		slog.Info("parsed products", "count", len(products))
		for i, p := range products {
			if i >= 5 {
				break
			}
			slog.Info("product", "pos", p.Position, "title", p.Title, "price", p.PriceRange)
		}
	}

	return products
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
