// Package tests contains integration tests that validate the full Foxhound
// pipeline end-to-end without requiring Redis, PostgreSQL, Docker, or any
// external services.
//
// Run with: go test -v ./tests/...
// Verbose:  go test -v -race ./tests/...
package tests

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/behavior"
	"github.com/sadewadee/foxhound/cache"
	"github.com/sadewadee/foxhound/captcha"
	"github.com/sadewadee/foxhound/engine"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
	"github.com/sadewadee/foxhound/middleware"
	"github.com/sadewadee/foxhound/parse"
	"github.com/sadewadee/foxhound/pipeline"
	"github.com/sadewadee/foxhound/pipeline/export"
	"github.com/sadewadee/foxhound/proxy"
	"github.com/sadewadee/foxhound/queue"
)

// ---------------------------------------------------------------------------
// Test 1: Full pipeline — identity → fetch → parse → pipeline → export
// ---------------------------------------------------------------------------

func TestFullPipeline_IdentityToExport(t *testing.T) {
	// Spin up a local HTTP server with a simple product page.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Test Product</title></head>
		<body>
			<h1 class="name">Widget Pro</h1>
			<span class="price">$29.99</span>
			<span class="sku">WP-001</span>
		</body></html>`)
	}))
	defer srv.Close()

	// 1. Generate identity
	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
	)
	if prof.UA == "" {
		t.Fatal("identity: UA is empty")
	}
	if prof.TLSProfile == "" {
		t.Fatal("identity: TLSProfile is empty")
	}
	t.Logf("Identity: %s %s, UA=%s", prof.BrowserName, prof.OS, prof.UA[:60]+"...")

	// 2. Create stealth fetcher with identity
	fetcher := fetch.NewStealth(
		fetch.WithIdentity(prof),
		fetch.WithTimeout(10*time.Second),
	)
	defer fetcher.Close()

	// 3. Fetch the page
	job := &foxhound.Job{
		ID:     "test-1",
		URL:    srv.URL,
		Method: "GET",
		Domain: "localhost",
	}
	resp, err := fetcher.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	t.Logf("Fetch: status=%d, bytes=%d, duration=%s", resp.StatusCode, len(resp.Body), resp.Duration)

	// 4. Parse with goquery
	doc, err := parse.NewDocument(resp)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	item := foxhound.NewItem()
	item.Set("name", doc.Text("h1.name"))
	item.Set("price", doc.Text("span.price"))
	item.Set("sku", doc.Text("span.sku"))
	item.URL = resp.URL

	if v, _ := item.Get("name"); v != "Widget Pro" {
		t.Errorf("expected 'Widget Pro', got %q", v)
	}
	if v, _ := item.Get("price"); v != "$29.99" {
		t.Errorf("expected '$29.99', got %q", v)
	}
	t.Logf("Parsed: name=%v, price=%v, sku=%v", item.Fields["name"], item.Fields["price"], item.Fields["sku"])

	// 5. Pipeline — validate + clean
	stages := pipeline.NewChain(
		&pipeline.Validate{Required: []string{"name", "price"}},
		&pipeline.Clean{TrimWhitespace: true, NormalizePrice: true},
	)
	result, err := stages.Process(context.Background(), item)
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}
	if result == nil {
		t.Fatal("pipeline dropped item")
	}

	// Price should be normalized to float64
	price, _ := result.Get("price")
	if p, ok := price.(float64); !ok || p != 29.99 {
		t.Errorf("expected price=29.99 (float64), got %v (%T)", price, price)
	}
	t.Logf("Pipeline output: %v", result.Fields)

	// 6. Export to CSV
	csvPath := filepath.Join(t.TempDir(), "output.csv")
	csvWriter, err := export.NewCSV(csvPath)
	if err != nil {
		t.Fatalf("csv writer: %v", err)
	}
	if err := csvWriter.Write(context.Background(), result); err != nil {
		t.Fatalf("csv write: %v", err)
	}
	if err := csvWriter.Flush(context.Background()); err != nil {
		t.Fatalf("csv flush: %v", err)
	}
	csvWriter.Close()

	data, _ := os.ReadFile(csvPath)
	if !strings.Contains(string(data), "Widget Pro") {
		t.Errorf("CSV missing 'Widget Pro': %s", data)
	}
	t.Logf("CSV output:\n%s", data)

	// 7. Export to JSONL
	jsonlPath := filepath.Join(t.TempDir(), "output.jsonl")
	jsonWriter, err := export.NewJSON(jsonlPath, export.JSONLines)
	if err != nil {
		t.Fatalf("json writer: %v", err)
	}
	if err := jsonWriter.Write(context.Background(), result); err != nil {
		t.Fatalf("json write: %v", err)
	}
	jsonWriter.Flush(context.Background())
	jsonWriter.Close()

	jsonData, _ := os.ReadFile(jsonlPath)
	if !strings.Contains(string(jsonData), "Widget Pro") {
		t.Errorf("JSONL missing 'Widget Pro': %s", jsonData)
	}
	t.Logf("JSONL output: %s", strings.TrimSpace(string(jsonData)))
}

// ---------------------------------------------------------------------------
// Test 2: Engine Hunt — full crawl with local server
// ---------------------------------------------------------------------------

func TestEngineHunt_LocalCrawl(t *testing.T) {
	// Multi-page local server: homepage with links → detail pages.
	var requestCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/":
			fmt.Fprintf(w, `<html><head><title>Shop</title></head><body>
				<a href="/product/1">Product 1</a>
				<a href="/product/2">Product 2</a>
				<a href="/product/3">Product 3</a>
			</body></html>`)
		default:
			name := strings.TrimPrefix(r.URL.Path, "/product/")
			fmt.Fprintf(w, `<html><head><title>Product %s</title></head><body>
				<h1>Product %s</h1>
				<span class="price">$%s9.99</span>
			</body></html>`, name, name, name)
		}
	}))
	defer srv.Close()

	// Setup components
	q := queue.NewMemoryQueue()
	fetcher := fetch.NewStealth(fetch.WithTimeout(5 * time.Second))

	var itemCount atomic.Int64
	processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
		doc, err := parse.NewDocument(resp)
		if err != nil {
			return &foxhound.Result{}, nil
		}

		item := foxhound.NewItem()
		item.Set("url", resp.URL)
		item.Set("title", strings.TrimSpace(doc.Text("title")))
		item.URL = resp.URL
		itemCount.Add(1)

		// Discover links (only from homepage)
		var jobs []*foxhound.Job
		if resp.Job != nil && resp.Job.Depth == 0 {
			doc.Each("a[href]", func(_ int, s *goquery.Selection) {
				href, exists := s.Attr("href")
				if !exists {
					return
				}
				jobs = append(jobs, &foxhound.Job{
					ID:        fmt.Sprintf("job-%s", href),
					URL:       srv.URL + href,
					Method:    "GET",
					FetchMode: foxhound.FetchAuto,
					Priority:  foxhound.PriorityNormal,
					Depth:     1,
					Domain:    "localhost",
					CreatedAt: time.Now(),
				})
			})
		}

		return &foxhound.Result{
			Items: []*foxhound.Item{item},
			Jobs:  jobs,
		}, nil
	})

	// Seed job
	seed := &foxhound.Job{
		ID:        "seed",
		URL:       srv.URL + "/",
		Method:    "GET",
		FetchMode: foxhound.FetchAuto,
		Priority:  foxhound.PriorityHigh,
		Depth:     0,
		Domain:    "localhost",
		CreatedAt: time.Now(),
	}

	outputPath := filepath.Join(t.TempDir(), "crawl.jsonl")
	writer, err := export.NewJSON(outputPath, export.JSONLines)
	if err != nil {
		t.Fatal(err)
	}

	hunt := engine.NewHunt(engine.HuntConfig{
		Name:      "test-crawl",
		Domain:    "localhost",
		Walkers:   2,
		Seeds:     []*foxhound.Job{seed},
		Processor: processor,
		Fetcher:   fetcher,
		Queue:     q,
		Writers:   []foxhound.Writer{writer},
		Middlewares: []foxhound.Middleware{
			middleware.NewDedup(),
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := hunt.Run(ctx); err != nil {
		t.Fatalf("hunt failed: %v", err)
	}

	writer.Flush(context.Background())
	writer.Close()

	stats := hunt.Stats()
	t.Logf("Hunt stats: %s", stats.Summary())
	t.Logf("Items extracted: %d", itemCount.Load())
	t.Logf("HTTP requests to server: %d", requestCount.Load())

	// Should have fetched homepage + 3 product pages = 4 requests
	if requestCount.Load() < 4 {
		t.Errorf("expected at least 4 requests, got %d", requestCount.Load())
	}
	if itemCount.Load() < 4 {
		t.Errorf("expected at least 4 items, got %d", itemCount.Load())
	}

	// Verify output file
	data, _ := os.ReadFile(outputPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	t.Logf("JSONL lines: %d", len(lines))
	if len(lines) < 4 {
		t.Errorf("expected at least 4 JSONL lines, got %d", len(lines))
	}
}

// ---------------------------------------------------------------------------
// Test 3: Smart fetcher — static with block detection + escalation
// ---------------------------------------------------------------------------

func TestSmartFetcher_BlockEscalation(t *testing.T) {
	// Server that returns 403 on first request (simulating block),
	// then 200 on subsequent requests.
	var callCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			w.WriteHeader(403)
			fmt.Fprint(w, "blocked")
			return
		}
		w.WriteHeader(200)
		fmt.Fprint(w, "<html><title>OK</title></html>")
	}))
	defer srv.Close()

	static := fetch.NewStealth(fetch.WithTimeout(5 * time.Second))
	// Use a second stealth fetcher as "browser" for testing (real would be Camoufox)
	browser := fetch.NewStealth(fetch.WithTimeout(5 * time.Second))

	smart := fetch.NewSmart(static, browser)
	defer smart.Close()

	job := &foxhound.Job{
		ID:        "test",
		URL:       srv.URL,
		Method:    "GET",
		FetchMode: foxhound.FetchAuto,
		Domain:    "localhost",
	}

	resp, err := smart.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("smart fetch failed: %v", err)
	}

	// Should have escalated: first call blocked (403), second call succeeded (200)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 after escalation, got %d", resp.StatusCode)
	}
	if callCount.Load() != 2 {
		t.Errorf("expected 2 server calls (block + escalation), got %d", callCount.Load())
	}
	t.Logf("Smart fetch: escalated after 403, final status=%d", resp.StatusCode)
}

// ---------------------------------------------------------------------------
// Test 4: Identity consistency — all attributes match
// ---------------------------------------------------------------------------

func TestIdentityConsistency(t *testing.T) {
	tests := []struct {
		browser  identity.Browser
		os       identity.OS
		uaCheck  string
		platform string
	}{
		{identity.BrowserFirefox, identity.OSWindows, "Firefox", "Win32"},
		{identity.BrowserFirefox, identity.OSMacOS, "Firefox", "MacIntel"},
		{identity.BrowserFirefox, identity.OSLinux, "Firefox", "Linux x86_64"},
		{identity.BrowserChrome, identity.OSWindows, "Chrome", "Win32"},
		{identity.BrowserChrome, identity.OSMacOS, "Chrome", "MacIntel"},
		{identity.BrowserChrome, identity.OSLinux, "Chrome", "Linux x86_64"},
	}

	for _, tc := range tests {
		name := fmt.Sprintf("%s_%s", tc.browser, tc.os)
		t.Run(name, func(t *testing.T) {
			prof := identity.Generate(
				identity.WithBrowser(tc.browser),
				identity.WithOS(tc.os),
			)

			// UA contains browser name
			if !strings.Contains(prof.UA, tc.uaCheck) {
				t.Errorf("UA %q missing %q", prof.UA, tc.uaCheck)
			}
			// Platform matches OS
			if prof.Platform != tc.platform {
				t.Errorf("expected platform %q, got %q", tc.platform, prof.Platform)
			}
			// TLS profile references browser
			if !strings.Contains(prof.TLSProfile, string(tc.browser)) {
				t.Errorf("TLS profile %q missing browser %q", prof.TLSProfile, tc.browser)
			}
			// Screen dimensions reasonable
			if prof.ScreenW < 1024 || prof.ScreenH < 600 {
				t.Errorf("unreasonable screen: %dx%d", prof.ScreenW, prof.ScreenH)
			}
			// Locale and timezone set
			if prof.Locale == "" || prof.Timezone == "" {
				t.Error("locale or timezone empty")
			}
			// Header order non-empty
			if len(prof.HeaderOrder) == 0 {
				t.Error("header order empty")
			}
			// CamoufoxEnv populated
			if len(prof.CamoufoxEnv) == 0 {
				t.Error("CamoufoxEnv empty")
			}

			t.Logf("OK: UA=%s..., Platform=%s, TLS=%s, Screen=%dx%d, Locale=%s, TZ=%s",
				prof.UA[:50], prof.Platform, prof.TLSProfile, prof.ScreenW, prof.ScreenH, prof.Locale, prof.Timezone)
		})
	}
}

// ---------------------------------------------------------------------------
// Test 5: Middleware chain — ratelimit + dedup + retry
// ---------------------------------------------------------------------------

func TestMiddlewareChain(t *testing.T) {
	var fetchCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		w.WriteHeader(200)
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	base := fetch.NewStealth(fetch.WithTimeout(5 * time.Second))

	// Chain: dedup → ratelimit → retry → base
	chain := middleware.Chain(
		middleware.NewDedup(),
		middleware.NewRateLimit(100, 10), // high limit so test is fast
		middleware.NewRetry(2, 10*time.Millisecond),
	)
	wrapped := chain.Wrap(base)

	ctx := context.Background()

	// First request — should go through
	job := &foxhound.Job{ID: "1", URL: srv.URL + "/page1", Method: "GET", Domain: "localhost"}
	resp, err := wrapped.Fetch(ctx, job)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Duplicate request — should be deduped (status 0)
	resp2, err := wrapped.Fetch(ctx, job)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if resp2.StatusCode != 0 {
		t.Errorf("expected dedup status 0, got %d", resp2.StatusCode)
	}

	// Different URL — should go through
	job2 := &foxhound.Job{ID: "2", URL: srv.URL + "/page2", Method: "GET", Domain: "localhost"}
	resp3, err := wrapped.Fetch(ctx, job2)
	if err != nil {
		t.Fatalf("third fetch: %v", err)
	}
	if resp3.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp3.StatusCode)
	}

	// Server should have been hit exactly 2 times (page1 + page2, not duplicate)
	if fetchCount.Load() != 2 {
		t.Errorf("expected 2 server hits, got %d", fetchCount.Load())
	}
	t.Logf("Middleware chain: 3 requests → %d server hits (dedup filtered 1)", fetchCount.Load())
}

// ---------------------------------------------------------------------------
// Test 6: Proxy pool — rotation and health
// ---------------------------------------------------------------------------

func TestProxyPool_RotationAndHealth(t *testing.T) {
	pool := proxy.NewPool(proxy.Static([]string{
		"http://user:pass@proxy1.example.com:8080",
		"http://user:pass@proxy2.example.com:8080",
		"http://user:pass@proxy3.example.com:8080",
	}))
	defer pool.Close()

	if pool.Len() != 3 {
		t.Fatalf("expected 3 proxies, got %d", pool.Len())
	}

	pool.SetRotation(proxy.PerRequest)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Get 6 proxies — should round-robin through all 3
	seen := make(map[string]int)
	for i := 0; i < 6; i++ {
		px, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("get %d: %v", i, err)
		}
		seen[px.URL]++
		pool.Release(px, true)
	}

	if len(seen) != 3 {
		t.Errorf("expected 3 distinct proxies, got %d", len(seen))
	}
	for url, count := range seen {
		if count != 2 {
			t.Errorf("proxy %s: expected 2 uses, got %d", url, count)
		}
	}
	t.Logf("Round-robin: %v", seen)
}

// ---------------------------------------------------------------------------
// Test 7: Queue — memory queue priority ordering
// ---------------------------------------------------------------------------

func TestMemoryQueue_PriorityOrdering(t *testing.T) {
	q := queue.NewMemoryQueue()
	defer q.Close()

	ctx := context.Background()

	// Push jobs with different priorities
	q.Push(ctx, &foxhound.Job{ID: "low", URL: "http://low", Priority: foxhound.PriorityLow, CreatedAt: time.Now()})
	q.Push(ctx, &foxhound.Job{ID: "high", URL: "http://high", Priority: foxhound.PriorityHigh, CreatedAt: time.Now()})
	q.Push(ctx, &foxhound.Job{ID: "normal", URL: "http://normal", Priority: foxhound.PriorityNormal, CreatedAt: time.Now()})

	// Pop should return high → normal → low
	expected := []string{"high", "normal", "low"}
	for _, exp := range expected {
		job, err := q.Pop(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if job.ID != exp {
			t.Errorf("expected %q, got %q", exp, job.ID)
		}
	}
	t.Log("Priority ordering: high → normal → low ✓")
}

// ---------------------------------------------------------------------------
// Test 8: Cache — memory LRU + TTL
// ---------------------------------------------------------------------------

func TestMemoryCache_LRUAndTTL(t *testing.T) {
	c := cache.NewMemory(3) // max 3 entries
	defer c.Close()

	ctx := context.Background()

	// Fill cache
	c.Set(ctx, "a", []byte("1"), time.Minute)
	c.Set(ctx, "b", []byte("2"), time.Minute)
	c.Set(ctx, "c", []byte("3"), time.Minute)

	// All present
	for _, k := range []string{"a", "b", "c"} {
		if _, ok := c.Get(ctx, k); !ok {
			t.Errorf("expected %q in cache", k)
		}
	}

	// Add 4th — should evict LRU ("a" since it was accessed least recently)
	c.Set(ctx, "d", []byte("4"), time.Minute)
	if _, ok := c.Get(ctx, "a"); ok {
		t.Error("expected 'a' to be evicted")
	}
	if _, ok := c.Get(ctx, "d"); !ok {
		t.Error("expected 'd' in cache")
	}

	// TTL test
	c.Set(ctx, "expires", []byte("soon"), 50*time.Millisecond)
	if _, ok := c.Get(ctx, "expires"); !ok {
		t.Error("expected 'expires' immediately after set")
	}
	time.Sleep(100 * time.Millisecond)
	if _, ok := c.Get(ctx, "expires"); ok {
		t.Error("expected 'expires' to have expired")
	}
	t.Log("LRU eviction + TTL expiry ✓")
}

// ---------------------------------------------------------------------------
// Test 9: Behavior — timing distribution + rhythm
// ---------------------------------------------------------------------------

func TestBehaviorTiming_LogNormal(t *testing.T) {
	timing := behavior.NewTiming(behavior.TimingConfig{
		Mu:    1.0,
		Sigma: 0.8,
		Min:   100 * time.Millisecond,
		Max:   30 * time.Second,
	})

	var total time.Duration
	n := 1000
	for i := 0; i < n; i++ {
		d := timing.Delay()
		if d < 100*time.Millisecond || d > 30*time.Second {
			t.Errorf("delay %s out of bounds", d)
		}
		total += d
	}

	mean := total / time.Duration(n)
	// Log-normal with mu=1.0, sigma=0.8: theoretical mean ≈ 3.7s
	if mean < 1*time.Second || mean > 10*time.Second {
		t.Errorf("mean delay %s outside expected range [1s, 10s]", mean)
	}
	t.Logf("Log-normal: n=%d, mean=%s", n, mean)
}

func TestBehaviorRhythm_BurstPause(t *testing.T) {
	r := behavior.NewRhythm(behavior.DefaultRhythmConfig())

	burstDelays := 0
	pauseHit := false

	for i := 0; i < 30; i++ {
		d := r.Next()
		state := r.State()
		if state == behavior.RhythmBurst {
			burstDelays++
		}
		if state == behavior.RhythmPause || state == behavior.RhythmLongPause {
			pauseHit = true
			if d < 5*time.Second {
				// Pauses should be substantial
				t.Logf("Warning: short pause %s at step %d", d, i)
			}
		}
	}

	if burstDelays == 0 {
		t.Error("no burst delays generated")
	}
	if !pauseHit {
		t.Error("no pause state reached after 30 steps")
	}
	t.Logf("Rhythm: %d burst delays, pause reached: %v", burstDelays, pauseHit)
}

// ---------------------------------------------------------------------------
// Test 10: CAPTCHA detection
// ---------------------------------------------------------------------------

func TestCaptchaDetection(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected captcha.CaptchaType
	}{
		{
			"cloudflare turnstile",
			`<div class="cf-turnstile" data-sitekey="0x4AAAA">`,
			captcha.CaptchaCloudflare,
		},
		{
			"recaptcha",
			`<div class="g-recaptcha" data-sitekey="6Le">`,
			captcha.CaptchaRecaptcha,
		},
		{
			"hcaptcha",
			`<div class="h-captcha" data-sitekey="abc">`,
			captcha.CaptchaHCaptcha,
		},
		{
			"no captcha",
			`<html><body>Normal page</body></html>`,
			captcha.CaptchaNone,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := &foxhound.Response{
				StatusCode: 200,
				Body:       []byte(tc.body),
				URL:        "https://example.com",
			}
			result := captcha.Detect(resp)
			if result.Type != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result.Type)
			}
			t.Logf("Detected: %s (sitekey=%q)", result.Type, result.SiteKey)
		})
	}
}

// ---------------------------------------------------------------------------
// Test 11: Parse — all modes (CSS, JSON, regex, structured)
// ---------------------------------------------------------------------------

func TestParseAllModes(t *testing.T) {
	htmlBody := `<html><head><title>Test</title></head>
	<body>
		<div class="product">
			<h2>Widget</h2>
			<span class="price">$19.99</span>
		</div>
		<div class="product">
			<h2>Gadget</h2>
			<span class="price">$39.99</span>
		</div>
	</body></html>`

	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(htmlBody),
		URL:        "https://example.com/products",
	}

	// CSS selectors
	doc, err := parse.NewDocument(resp)
	if err != nil {
		t.Fatal(err)
	}
	titles := doc.Texts("div.product h2")
	if len(titles) != 2 {
		t.Errorf("expected 2 product titles, got %d", len(titles))
	}
	t.Logf("CSS: found %d products: %v", len(titles), titles)

	// Regex
	prices, err := parse.RegexExtractAll(resp, `\$\d+\.\d+`)
	if err != nil {
		t.Fatal(err)
	}
	if len(prices) != 2 {
		t.Errorf("expected 2 prices, got %d", len(prices))
	}
	t.Logf("Regex: %v", prices)

	// Structured schema
	schema := &parse.Schema{
		Fields: []parse.FieldDef{
			{Name: "title", Selector: "title"},
			{Name: "first_product", Selector: "div.product h2"},
		},
	}
	item, err := schema.Extract(resp)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := item.Get("title"); v != "Test" {
		t.Errorf("expected title 'Test', got %q", v)
	}
	t.Logf("Structured: %v", item.Fields)

	// JSON parse
	jsonResp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(`{"data":{"items":[{"name":"A"},{"name":"B"}]}}`),
		URL:        "https://api.example.com",
	}
	val, err := parse.JSONPath(jsonResp, "data.items")
	if err != nil {
		t.Fatal(err)
	}
	items, ok := val.([]any)
	if !ok || len(items) != 2 {
		t.Errorf("expected 2 JSON items, got %v", val)
	}
	t.Logf("JSONPath: %v", val)
}

// ---------------------------------------------------------------------------
// Test 12: SQLite queue persistence
// ---------------------------------------------------------------------------

func TestSQLiteQueue_Persistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// Write jobs
	q1, err := queue.NewSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	q1.Push(ctx, &foxhound.Job{ID: "j1", URL: "http://1", Priority: foxhound.PriorityHigh, CreatedAt: time.Now()})
	q1.Push(ctx, &foxhound.Job{ID: "j2", URL: "http://2", Priority: foxhound.PriorityNormal, CreatedAt: time.Now()})
	q1.Close()

	// Reopen and verify
	q2, err := queue.NewSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer q2.Close()

	if q2.Len() != 2 {
		t.Errorf("expected 2 pending jobs, got %d", q2.Len())
	}

	job, err := q2.Pop(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "j1" {
		t.Errorf("expected j1 (high priority), got %s", job.ID)
	}
	t.Logf("SQLite persistence: 2 jobs survived close/reopen, popped %s first", job.ID)
}

// ---------------------------------------------------------------------------
// Test 13: GeoIP country lookup
// ---------------------------------------------------------------------------

func TestGeoIP_CountryLookup(t *testing.T) {
	countries := []struct {
		code     string
		timezone string
		locale   string
	}{
		{"US", "America/New_York", "en-US"},
		{"GB", "Europe/London", "en-GB"},
		{"JP", "Asia/Tokyo", "ja-JP"},
		{"DE", "Europe/Berlin", "de-DE"},
	}

	for _, tc := range countries {
		t.Run(tc.code, func(t *testing.T) {
			geo, ok := identity.LookupCountry(tc.code)
			if !ok {
				t.Fatalf("country %q not found", tc.code)
			}
			if geo.Timezone != tc.timezone {
				t.Errorf("expected tz %q, got %q", tc.timezone, geo.Timezone)
			}
			if geo.Locale != tc.locale {
				t.Errorf("expected locale %q, got %q", tc.locale, geo.Locale)
			}
			if geo.Lat == 0 && geo.Lng == 0 {
				t.Error("coordinates are zero")
			}
			t.Logf("%s: tz=%s, locale=%s, lat=%.2f, lng=%.2f", tc.code, geo.Timezone, geo.Locale, geo.Lat, geo.Lng)
		})
	}

	// WithCountry should apply geo to identity
	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
		identity.WithCountry("JP"),
	)
	if prof.Timezone != "Asia/Tokyo" {
		t.Errorf("expected Asia/Tokyo, got %s", prof.Timezone)
	}
	if prof.Locale != "ja-JP" {
		t.Errorf("expected ja-JP, got %s", prof.Locale)
	}
	t.Logf("WithCountry(JP): tz=%s, locale=%s", prof.Timezone, prof.Locale)
}

// ---------------------------------------------------------------------------
// Test 14: Config loading
// ---------------------------------------------------------------------------

func TestConfigLoading(t *testing.T) {
	cfgContent := `
hunt:
  domain: "test.example.com"
  walkers: 5

identity:
  browser: "chrome"
  os: ["windows"]

fetch:
  static:
    timeout: 15s
  browser:
    instances: 0

middleware:
  ratelimit:
    enabled: true
    requests_per_sec: 3.0
    burst_size: 5
  depth_limit:
    max: 4

queue:
  backend: memory

logging:
  level: debug
  format: text
`
	cfgPath := filepath.Join(t.TempDir(), "test-config.yaml")
	os.WriteFile(cfgPath, []byte(cfgContent), 0644)

	cfg, err := foxhound.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Hunt.Domain != "test.example.com" {
		t.Errorf("domain: %q", cfg.Hunt.Domain)
	}
	if cfg.Hunt.Walkers != 5 {
		t.Errorf("walkers: %d", cfg.Hunt.Walkers)
	}
	if cfg.Identity.Browser != "chrome" {
		t.Errorf("browser: %q", cfg.Identity.Browser)
	}
	if cfg.Fetch.Static.Timeout.Duration != 15*time.Second {
		t.Errorf("timeout: %s", cfg.Fetch.Static.Timeout.Duration)
	}
	if cfg.Middleware.RateLimit.RequestsPerSec != 3.0 {
		t.Errorf("rps: %f", cfg.Middleware.RateLimit.RequestsPerSec)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("log level: %q", cfg.Logging.Level)
	}

	t.Logf("Config: domain=%s, walkers=%d, browser=%s, rps=%.1f, depth=%d",
		cfg.Hunt.Domain, cfg.Hunt.Walkers, cfg.Identity.Browser,
		cfg.Middleware.RateLimit.RequestsPerSec, cfg.Middleware.DepthLimit.Max)
}
