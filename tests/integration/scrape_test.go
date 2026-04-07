//go:build integration && playwright

// Package integration contains real-world scraping integration tests for Foxhound.
//
// These tests hit live websites through a real proxy to verify that the framework's
// identity system, TLS impersonation, stealth fetcher, browser fetcher, and
// extraction pipeline work correctly against production anti-bot defenses.
//
// Run with:
//
//	go test -tags "integration playwright" -v -count=1 -timeout 300s ./tests/integration/
//	go test -tags "integration playwright" -run TestBingSearch -v -count=1 ./tests/integration/
package integration

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/captcha"
	"github.com/sadewadee/foxhound/engine"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
	"github.com/sadewadee/foxhound/parse"
	_ "github.com/sadewadee/foxhound/parse" // register HTML selectors
)

// Ensure imports are used (prevents compile errors).
var (
	_ = engine.NewTrail
	_ = fmt.Sprintf
)

const (
	// proxyURL is the proxy used for all integration tests.
	proxyURL = "http://c55eb1863f1a7c35:vebfr4dbsyr7trbhmttf@165.154.202.132:443"
)

func TestMain(m *testing.M) {
	// Enable verbose logging for integration tests.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	fmt.Println("================================================================")
	fmt.Println("  Foxhound Integration Tests - Real Website Scraping")
	fmt.Println("  Proxy: 165.154.202.132:443")
	fmt.Println("================================================================")
	os.Exit(m.Run())
}

// setupIdentity creates a consistent identity profile for US proxy.
func setupIdentity() *identity.Profile {
	return identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
		identity.WithCountry("US"),
	)
}

// setupStealth creates a StealthFetcher with proxy and identity.
func setupStealth(t *testing.T) *fetch.StealthFetcher {
	t.Helper()
	prof := setupIdentity()
	t.Logf("Identity: UA=%s", prof.UA[:80])
	t.Logf("Identity: TLS=%s, Platform=%s, Locale=%s, TZ=%s",
		prof.TLSProfile, prof.Platform, prof.Locale, prof.Timezone)

	f := fetch.NewStealth(
		fetch.WithIdentity(prof),
		fetch.WithProxy(proxyURL),
		fetch.WithTimeout(30*time.Second),
	)
	return f
}

// setupSmart creates a SmartFetcher (static + browser) with proxy.
func setupSmart(t *testing.T) *fetch.SmartFetcher {
	t.Helper()
	prof := setupIdentity()
	t.Logf("Identity: UA=%s", prof.UA[:80])

	static := fetch.NewStealth(
		fetch.WithIdentity(prof),
		fetch.WithProxy(proxyURL),
		fetch.WithTimeout(30*time.Second),
	)

	browser, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBrowserProxy(proxyURL),
		fetch.WithHeadless("true"),
		fetch.WithBrowserTimeout(60*time.Second),
		fetch.WithBlockImages(true),
	)
	if err != nil {
		t.Fatalf("NewCamoufox failed: %v", err)
	}

	smart := fetch.NewSmart(static, browser)
	return smart
}

// assertNotBlocked checks that the response was not blocked or captcha'd.
func assertNotBlocked(t *testing.T, resp *foxhound.Response) {
	t.Helper()
	det := captcha.Detect(resp)
	if det.Type != captcha.CaptchaNone {
		t.Errorf("CAPTCHA detected: type=%s, sitekey=%s", det.Type, det.SiteKey)
		t.Logf("Response body (first 500 chars): %s", truncate(string(resp.Body), 500))
	}
	if resp.StatusCode == 403 || resp.StatusCode == 429 || resp.StatusCode == 503 {
		t.Errorf("Blocked with status %d", resp.StatusCode)
		t.Logf("Response body (first 500 chars): %s", truncate(string(resp.Body), 500))
	}
}

// truncate returns at most n characters of s.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ---------------------------------------------------------------------------
// Test 1: Bing Search (FetchStatic) — easiest target, good for smoke test
// ---------------------------------------------------------------------------

func TestBingSearch(t *testing.T) {
	f := setupStealth(t)
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	job := &foxhound.Job{
		ID:        "bing-search",
		URL:       "https://www.bing.com/search?q=golang+web+scraping",
		Method:    "GET",
		FetchMode: foxhound.FetchStatic,
		Domain:    "www.bing.com",
	}

	t.Logf("Fetching: %s", job.URL)
	resp, err := f.Fetch(ctx, job)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	t.Logf("Status: %d, Body: %d bytes, Duration: %s, Mode: %s",
		resp.StatusCode, len(resp.Body), resp.Duration, resp.FetchMode)

	// Verify success
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d. Body: %s", resp.StatusCode, truncate(string(resp.Body), 300))
	}
	assertNotBlocked(t, resp)

	// Extract results using parse package
	doc, err := parse.NewDocument(resp)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Bing result titles are in <h2> tags inside <li class="b_algo">
	titles := doc.Texts("li.b_algo h2")
	t.Logf("Found %d Bing result titles", len(titles))
	for i, title := range titles {
		if i >= 5 {
			break
		}
		t.Logf("  [%d] %s", i+1, title)
	}

	if len(titles) == 0 {
		// Try alternative selectors
		titles = doc.Texts("h2 a")
		t.Logf("Alt selector found %d titles", len(titles))
	}

	if len(titles) == 0 {
		t.Errorf("No search results found. Body preview: %s", truncate(string(resp.Body), 1000))
	}

	// Extract URLs
	urls := doc.Attrs("li.b_algo h2 a", "href")
	t.Logf("Found %d result URLs", len(urls))
	for i, u := range urls {
		if i >= 3 {
			break
		}
		t.Logf("  URL[%d]: %s", i+1, u)
	}
}

// ---------------------------------------------------------------------------
// Test 2: DuckDuckGo (FetchStatic)
// ---------------------------------------------------------------------------

func TestDuckDuckGo(t *testing.T) {
	f := setupStealth(t)
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// DDG lite/HTML version works without JS
	job := &foxhound.Job{
		ID:        "ddg-search",
		URL:       "https://html.duckduckgo.com/html/?q=golang+web+scraping",
		Method:    "GET",
		FetchMode: foxhound.FetchStatic,
		Domain:    "html.duckduckgo.com",
	}

	t.Logf("Fetching: %s", job.URL)
	resp, err := f.Fetch(ctx, job)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	t.Logf("Status: %d, Body: %d bytes, Duration: %s",
		resp.StatusCode, len(resp.Body), resp.Duration)

	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	assertNotBlocked(t, resp)

	// DDG HTML results are in <a class="result__a">
	doc, err := parse.NewDocument(resp)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	titles := doc.Texts("a.result__a")
	t.Logf("Found %d DDG result titles", len(titles))
	for i, title := range titles {
		if i >= 5 {
			break
		}
		t.Logf("  [%d] %s", i+1, title)
	}

	if len(titles) == 0 {
		// Try alternative: the snippet container
		titles = doc.Texts(".result__title")
		t.Logf("Alt selector found %d titles", len(titles))
	}

	if len(titles) == 0 {
		t.Errorf("No DDG results found. Body preview: %s", truncate(string(resp.Body), 1000))
	}
}

// ---------------------------------------------------------------------------
// Test 3: Kompas.com (FetchStatic — Indonesian news)
// ---------------------------------------------------------------------------

func TestKompas(t *testing.T) {
	f := setupStealth(t)
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	job := &foxhound.Job{
		ID:        "kompas-home",
		URL:       "https://www.kompas.com/",
		Method:    "GET",
		FetchMode: foxhound.FetchStatic,
		Domain:    "www.kompas.com",
	}

	t.Logf("Fetching: %s", job.URL)
	resp, err := f.Fetch(ctx, job)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	t.Logf("Status: %d, Body: %d bytes, Duration: %s",
		resp.StatusCode, len(resp.Body), resp.Duration)

	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	assertNotBlocked(t, resp)

	// Extract headlines
	doc, err := parse.NewDocument(resp)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Kompas uses various headline selectors
	headlines := doc.Texts("h2.title a, h3.article__title a, .article__title a, .most__title a")
	t.Logf("Found %d headlines", len(headlines))
	for i, h := range headlines {
		if i >= 10 {
			break
		}
		t.Logf("  [%d] %s", i+1, strings.TrimSpace(h))
	}

	if len(headlines) < 5 {
		// Try broader selectors
		allLinks := doc.Texts("a[href*='kompas.com']")
		t.Logf("Broad selector found %d links with kompas.com", len(allLinks))
		if len(allLinks) == 0 {
			t.Errorf("No headlines found on Kompas. Body preview: %s", truncate(string(resp.Body), 1000))
		}
	}

	// Verify Indonesian content (common Indonesian words)
	bodyStr := string(resp.Body)
	indonesianWords := []string{"dan", "di", "ini", "yang", "untuk"}
	foundIndonesian := false
	for _, w := range indonesianWords {
		if strings.Contains(strings.ToLower(bodyStr), " "+w+" ") {
			foundIndonesian = true
			break
		}
	}
	if !foundIndonesian {
		t.Log("Warning: no common Indonesian words detected in body")
	} else {
		t.Log("Indonesian content detected")
	}

	// Extract article URLs
	articleURLs := doc.Attrs("a[href*='kompas.com']", "href")
	t.Logf("Found %d article URLs", len(articleURLs))
}

// ---------------------------------------------------------------------------
// Test 4: Kompas Article (FetchAuto — test escalation path)
// ---------------------------------------------------------------------------

func TestKompasArticle(t *testing.T) {
	smart := setupSmart(t)
	defer smart.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// First, fetch the homepage to find an article URL
	homeJob := &foxhound.Job{
		ID:        "kompas-home-for-article",
		URL:       "https://www.kompas.com/",
		Method:    "GET",
		FetchMode: foxhound.FetchStatic,
		Domain:    "www.kompas.com",
	}

	t.Log("Fetching Kompas homepage to find article URL...")
	homeResp, err := smart.Fetch(ctx, homeJob)
	if err != nil {
		t.Fatalf("Homepage fetch failed: %v", err)
	}

	if homeResp.StatusCode != 200 {
		t.Fatalf("Homepage status %d", homeResp.StatusCode)
	}

	// Find an article URL
	doc, err := parse.NewDocument(homeResp)
	if err != nil {
		t.Fatalf("Parse homepage failed: %v", err)
	}

	articleURLs := doc.Attrs("a[href*='read/']", "href")
	if len(articleURLs) == 0 {
		articleURLs = doc.Attrs("a[href*='kompas.com']", "href")
	}

	var articleURL string
	for _, u := range articleURLs {
		if strings.Contains(u, "/read/") || strings.Contains(u, "artikel") {
			articleURL = u
			break
		}
	}

	if articleURL == "" {
		t.Skip("No article URL found on Kompas homepage")
	}
	if !strings.HasPrefix(articleURL, "http") {
		articleURL = "https://www.kompas.com" + articleURL
	}

	t.Logf("Fetching article: %s", articleURL)

	// Fetch the article with FetchAuto
	articleJob := &foxhound.Job{
		ID:        "kompas-article",
		URL:       articleURL,
		Method:    "GET",
		FetchMode: foxhound.FetchAuto,
		Domain:    "www.kompas.com",
	}

	resp, err := smart.Fetch(ctx, articleJob)
	if err != nil {
		t.Fatalf("Article fetch failed: %v", err)
	}

	t.Logf("Article status: %d, Body: %d bytes, Mode used: %s, Duration: %s",
		resp.StatusCode, len(resp.Body), resp.FetchMode, resp.Duration)

	if resp.StatusCode != 200 {
		t.Fatalf("Article returned status %d", resp.StatusCode)
	}
	assertNotBlocked(t, resp)

	// Extract article content
	artDoc, err := parse.NewDocument(resp)
	if err != nil {
		t.Fatalf("Parse article failed: %v", err)
	}

	title := artDoc.Text("h1, .read__title, .article__title")
	t.Logf("Article title: %s", title)

	body := artDoc.Text(".read__content, .article__content, article")
	t.Logf("Article body length: %d chars", len(body))
	if len(body) > 200 {
		t.Logf("Article body preview: %s", truncate(body, 200))
	}

	if title == "" && len(body) == 0 {
		t.Error("No article content extracted")
	}
}

// ---------------------------------------------------------------------------
// Test 5: Google Search (FetchStatic with cookie warmup)
// ---------------------------------------------------------------------------

func TestGoogleSearch(t *testing.T) {
	f := setupStealth(t)
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Step 1: Cookie warmup — visit Google homepage first to establish session
	// cookies (CONSENT, NID, 1P_JAR, AEC). Real browsers always have these
	// before making a search request. Going straight to /search without cookies
	// is a strong bot signal.
	t.Log("Step 1: Cookie warmup — visiting google.com homepage first")
	warmupJob := &foxhound.Job{
		ID:        "google-warmup",
		URL:       "https://www.google.com/?hl=en",
		Method:    "GET",
		FetchMode: foxhound.FetchStatic,
		Domain:    "www.google.com",
	}

	warmupResp, err := f.Fetch(ctx, warmupJob)
	if err != nil {
		t.Fatalf("Warmup fetch failed: %v", err)
	}

	t.Logf("Warmup: status=%d, body=%d bytes, cookies=%d",
		warmupResp.StatusCode, len(warmupResp.Body), len(warmupResp.Cookies))
	for _, c := range warmupResp.Cookies {
		t.Logf("  Cookie: %s=%s", c.Name, truncate(c.Value, 30))
	}

	// Brief pause to simulate human behavior between homepage and search
	time.Sleep(800 * time.Millisecond)

	// Step 2: Actual search request — cookies from warmup are in the session
	t.Log("Step 2: Google Search with session cookies")
	searchJob := &foxhound.Job{
		ID:        "google-search",
		URL:       "https://www.google.com/search?q=golang+web+scraping&hl=en",
		Method:    "GET",
		FetchMode: foxhound.FetchStatic,
		Domain:    "www.google.com",
		Headers: http.Header{
			"Referer":        []string{"https://www.google.com/"},
			"Sec-Fetch-Site": []string{"same-origin"},
			"Sec-Fetch-Mode": []string{"navigate"},
			"Sec-Fetch-User": []string{"?1"},
			"Sec-Fetch-Dest": []string{"document"},
		},
	}

	resp, err := f.Fetch(ctx, searchJob)
	if err != nil {
		t.Fatalf("Search fetch failed: %v", err)
	}

	t.Logf("Search: status=%d, body=%d bytes, duration=%s",
		resp.StatusCode, len(resp.Body), resp.Duration)

	// Log response headers for debugging
	for _, hdr := range []string{"Content-Type", "Set-Cookie", "X-Frame-Options"} {
		if v := resp.Headers.Get(hdr); v != "" {
			t.Logf("  Response header %s: %s", hdr, truncate(v, 80))
		}
	}

	if resp.StatusCode == 429 {
		t.Logf("Still getting 429 — body preview: %s", truncate(string(resp.Body), 500))
		t.Fatalf("Google returned 429 (rate limited/blocked) even with cookie warmup")
	}

	assertNotBlocked(t, resp)

	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d. Body: %s", resp.StatusCode, truncate(string(resp.Body), 500))
	}

	// Extract search results
	doc, err := parse.NewDocument(resp)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	titles := doc.Texts("h3")
	t.Logf("Found %d h3 elements (search results)", len(titles))
	for i, title := range titles {
		if i >= 5 {
			break
		}
		t.Logf("  [%d] %s", i+1, title)
	}

	if len(titles) == 0 {
		bodyStr := string(resp.Body)
		if strings.Contains(bodyStr, "consent") || strings.Contains(bodyStr, "Before you continue") {
			t.Log("Google consent page detected")
		}
		t.Logf("Body preview: %s", truncate(bodyStr, 1000))
		t.Error("No search results found")
	} else {
		t.Logf("SUCCESS: Got %d Google Search results via FetchStatic with warmup", len(titles))
	}
}

// ---------------------------------------------------------------------------
// Test 5b: Google Search Diagnostic — logs full request fingerprint
// ---------------------------------------------------------------------------

func TestGoogleSearchDiagnostic(t *testing.T) {
	f := setupStealth(t)
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Step 1: Check what headers we send using httpbin
	t.Log("=== Diagnostic: Checking request fingerprint via httpbin ===")
	headerJob := &foxhound.Job{
		ID:        "diag-headers",
		URL:       "https://httpbin.org/headers",
		Method:    "GET",
		FetchMode: foxhound.FetchStatic,
		Domain:    "httpbin.org",
	}

	headerResp, err := f.Fetch(ctx, headerJob)
	if err != nil {
		t.Fatalf("Header check failed: %v", err)
	}

	t.Logf("Headers sent to server:\n%s", string(headerResp.Body))

	// Check for problematic headers
	bodyStr := string(headerResp.Body)
	if strings.Contains(bodyStr, "Go-http-client") {
		t.Error("PROBLEM: Go default User-Agent detected — identity not applied")
	}
	if strings.Contains(bodyStr, "Connection") && strings.Contains(bodyStr, "keep-alive") {
		t.Log("NOTE: Connection: keep-alive header present (stripped over HTTP/2)")
	}
	if !strings.Contains(bodyStr, "Te") && !strings.Contains(bodyStr, "TE") {
		t.Log("NOTE: TE header not echoed by httpbin (may be stripped by proxy)")
	}

	// Step 2: Warmup + search
	t.Log("=== Diagnostic: Google warmup ===")
	warmupJob := &foxhound.Job{
		ID:        "diag-warmup",
		URL:       "https://www.google.com/?hl=en",
		Method:    "GET",
		FetchMode: foxhound.FetchStatic,
		Domain:    "www.google.com",
	}

	warmupResp, err := f.Fetch(ctx, warmupJob)
	if err != nil {
		t.Fatalf("Warmup failed: %v", err)
	}

	t.Logf("Warmup status: %d", warmupResp.StatusCode)
	t.Logf("Warmup cookies: %d", len(warmupResp.Cookies))
	for _, c := range warmupResp.Cookies {
		t.Logf("  %s = %s", c.Name, truncate(c.Value, 40))
	}
	t.Logf("Warmup body preview: %s", truncate(string(warmupResp.Body), 500))

	// Check for consent page
	warmupBody := string(warmupResp.Body)
	if strings.Contains(warmupBody, "consent") || strings.Contains(warmupBody, "Before you continue") {
		t.Log("CONSENT page detected on warmup — may need to handle consent flow")
	}

	time.Sleep(1 * time.Second)

	t.Log("=== Diagnostic: Google Search ===")
	searchJob := &foxhound.Job{
		ID:        "diag-search",
		URL:       "https://www.google.com/search?q=test&hl=en&num=10",
		Method:    "GET",
		FetchMode: foxhound.FetchStatic,
		Domain:    "www.google.com",
		Headers: http.Header{
			"Sec-Fetch-Site": []string{"same_origin"},
		},
	}

	searchResp, err := f.Fetch(ctx, searchJob)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	t.Logf("Search status: %d", searchResp.StatusCode)
	t.Logf("Search body size: %d bytes", len(searchResp.Body))
	t.Logf("Search cookies: %d", len(searchResp.Cookies))

	// Log all response headers
	for k, v := range searchResp.Headers {
		t.Logf("  Resp header %s: %s", k, truncate(strings.Join(v, "; "), 80))
	}

	// Detect captcha
	det := captcha.Detect(searchResp)
	if det.Type != captcha.CaptchaNone {
		t.Logf("CAPTCHA detected: type=%s", det.Type)
	}

	searchBody := string(searchResp.Body)
	t.Logf("Body preview: %s", truncate(searchBody, 2000))

	// Check for specific signals
	signals := map[string]string{
		"unusual traffic": "Google detected unusual traffic",
		"captcha":         "CAPTCHA keyword in body",
		"recaptcha":       "reCAPTCHA detected",
		"consent":         "Consent page",
		"sorry":           "Google sorry page",
		"<h3":             "Search result h3 tags present",
		"search-result":   "Search result markers present",
	}
	for keyword, desc := range signals {
		if strings.Contains(strings.ToLower(searchBody), keyword) {
			t.Logf("SIGNAL: %s (keyword: %s)", desc, keyword)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 5c: Google Search via Browser (Camoufox) — homepage warmup → search
// ---------------------------------------------------------------------------

func TestGoogleSearchBrowser(t *testing.T) {
	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
		identity.WithCountry("US"),
	)
	t.Logf("Identity: UA=%s", prof.UA[:80])

	cf, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBrowserProxy(proxyURL),
		fetch.WithHeadless("true"),
		fetch.WithBrowserTimeout(60*time.Second),
		fetch.WithBlockImages(true),
	)
	if err != nil {
		t.Fatalf("NewCamoufox failed: %v", err)
	}
	defer cf.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Use Trail: navigate to google.com → type search → click search button
	// This simulates a real user flow with proper referer chain
	trail := engine.NewTrail("google-search-browser").
		Navigate("https://www.google.com/?hl=en").
		ClickOptional("button#L2AGLb").                    // "I agree" consent
		ClickOptional("div[role='none'] button").          // alternative consent
		WaitOptional("textarea[name='q']", 5*time.Second). // wait for search box
		Fill("textarea[name='q']", "golang web scraping").
		Click("input[name='btnK'], button[name='btnK']"). // "Google Search" button
		WaitOptional("div#search", 10*time.Second)        // wait for results

	jobs := trail.ToJobs()
	if len(jobs) == 0 {
		t.Fatal("Trail.ToJobs() returned no jobs")
	}

	mainJob := jobs[len(jobs)-1]
	t.Logf("Main job URL: %s, Steps: %d", mainJob.URL, len(mainJob.Steps))

	resp, err := cf.Fetch(ctx, mainJob)
	if err != nil {
		t.Logf("Browser fetch error: %v", err)
		t.FailNow()
	}

	t.Logf("Status: %d, Body: %d bytes, Duration: %s", resp.StatusCode, len(resp.Body), resp.Duration)

	if resp.StatusCode == 429 {
		t.Logf("429 even with browser — body preview: %s", truncate(string(resp.Body), 500))
		t.Fatal("Google Search 429 even with Camoufox browser")
	}

	// Extract results
	doc, err := parse.NewDocument(resp)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	titles := doc.Texts("h3")
	t.Logf("Found %d search results (h3 elements)", len(titles))
	for i, title := range titles {
		if i >= 5 {
			break
		}
		t.Logf("  [%d] %s", i+1, title)
	}

	if len(titles) > 0 {
		t.Logf("SUCCESS: Got %d Google Search results via Camoufox browser", len(titles))
	} else {
		bodyPreview := truncate(string(resp.Body), 500)
		t.Logf("No h3 results. Body preview: %s", bodyPreview)
		if strings.Contains(string(resp.Body), "captcha") || strings.Contains(string(resp.Body), "unusual traffic") {
			t.Error("CAPTCHA/block detected even with browser")
		} else {
			t.Log("Page loaded but no h3 elements — might be consent page or JS rendering issue")
		}
	}
}

// ---------------------------------------------------------------------------
// Test 6: Google Maps (FetchBrowser + Trail)
// ---------------------------------------------------------------------------

func TestGoogleMaps(t *testing.T) {
	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSMacOS),
		identity.WithLocale("en-US", "en-US", "en"),
		identity.WithTimezone("America/New_York"),
		identity.WithCountry("US"),
	)
	t.Logf("Identity: UA=%s", prof.UA[:80])

	cf, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBrowserProxy(proxyURL),
		fetch.WithHeadless("true"),
		fetch.WithBrowserTimeout(90*time.Second),
		fetch.WithBlockImages(false), // Maps needs images for rendering
	)
	if err != nil {
		t.Fatalf("NewCamoufox failed: %v", err)
	}
	defer cf.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Use a pre-searched Google Maps URL to avoid consent page issues.
	// Google Maps supports search via URL query parameter which bypasses
	// the need to interact with the search box through a consent overlay.
	searchURL := "https://www.google.com/maps/search/cafe+in+canggu+bali/"

	// Build a Trail that navigates directly to the search results page,
	// dismisses any consent/cookie overlays, then scrolls the feed.
	trail := engine.NewTrail("maps-cafe-canggu").
		Navigate(searchURL).
		ClickOptional("form[action*='consent'] button").  // Google consent "Accept"
		ClickOptional("button[jsaction*='dismiss']").     // cookie dismiss
		WaitOptional("div[role='feed']", 20*time.Second). // wait for results feed
		InfiniteScrollInUntil("div[role='feed']", "div.Nv2PK", 10, 30)

	// Convert Trail to Jobs
	jobs := trail.ToJobs()
	if len(jobs) == 0 {
		t.Fatal("Trail.ToJobs() returned no jobs")
	}
	t.Logf("Trail produced %d job(s)", len(jobs))

	// The main job has all steps attached
	mainJob := jobs[len(jobs)-1] // last job has the browser steps
	t.Logf("Main job URL: %s, Steps: %d, Mode: %s",
		mainJob.URL, len(mainJob.Steps), mainJob.FetchMode)

	// Fetch with browser
	resp, err := cf.Fetch(ctx, mainJob)
	if err != nil {
		// Google Maps browser fetch can fail due to consent pages or geo blocks.
		// Log but don't hard-fail since this is environment-dependent.
		t.Logf("Browser fetch error (may be consent/geo issue): %v", err)
		t.Log("Google Maps browser test is environment-dependent; logging as informational")
		return
	}

	t.Logf("Response: status=%d, body=%d bytes, mode=%s, duration=%s",
		resp.StatusCode, len(resp.Body), resp.FetchMode, resp.Duration)

	if resp.StatusCode != 200 {
		t.Logf("Google Maps returned status %d (may be blocked/redirected)", resp.StatusCode)
		return
	}

	// Extract business data from the page
	doc, err := parse.NewDocument(resp)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Google Maps business cards: .Nv2PK or .fontHeadlineSmall or aria-label on links
	businessNames := doc.Texts(".Nv2PK .fontHeadlineSmall, .Nv2PK .qBF1Pd, .qBF1Pd")
	t.Logf("Found %d business names", len(businessNames))
	for i, name := range businessNames {
		if i >= 10 {
			break
		}
		t.Logf("  [%d] %s", i+1, strings.TrimSpace(name))
	}

	if len(businessNames) == 0 {
		// Try broader selectors: aria-label on feed links
		ariaLabels := doc.Attrs("[role='feed'] a[aria-label]", "aria-label")
		t.Logf("Alt selector (aria-label) found %d items", len(ariaLabels))
		for i, name := range ariaLabels {
			if i >= 5 {
				break
			}
			t.Logf("  alt[%d] %s", i+1, strings.TrimSpace(name))
		}
		businessNames = ariaLabels
	}

	// Extract ratings
	ratings := doc.Texts(".MW4etd, span[role='img']")
	t.Logf("Found %d rating elements", len(ratings))

	// We want at least 3 results
	totalResults := len(businessNames)
	if totalResults == 0 {
		// Count by feed items in raw body
		bodyStr := string(resp.Body)
		totalResults = strings.Count(bodyStr, "Nv2PK")
		t.Logf("Counted %d Nv2PK markers in body", totalResults)
	}

	if totalResults < 3 {
		t.Logf("Only got %d results (expected 3+). Google Maps may have shown consent page.", totalResults)
		t.Logf("Body preview: %s", truncate(string(resp.Body), 2000))
	} else {
		t.Logf("SUCCESS: Got %d Google Maps results", totalResults)
	}

	// Check step results (from Evaluate steps)
	if len(resp.StepResults) > 0 {
		t.Logf("Step results: %v", resp.StepResults)
	}
}

// ---------------------------------------------------------------------------
// Test 7: Verify proxy is actually being used
// ---------------------------------------------------------------------------

func TestProxyUsed(t *testing.T) {
	f := setupStealth(t)
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// httpbin.org/ip returns the client IP — should show the proxy IP
	job := &foxhound.Job{
		ID:        "check-ip",
		URL:       "https://httpbin.org/ip",
		Method:    "GET",
		FetchMode: foxhound.FetchStatic,
		Domain:    "httpbin.org",
	}

	t.Logf("Fetching IP check via proxy...")
	resp, err := f.Fetch(ctx, job)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	body := string(resp.Body)
	t.Logf("IP check response: %s", strings.TrimSpace(body))

	// Verify it contains an IP address (basic sanity)
	if !strings.Contains(body, "origin") {
		t.Error("Response does not contain 'origin' field")
	}

	// Also check headers to verify proxy sent proper headers
	headersJob := &foxhound.Job{
		ID:        "check-headers",
		URL:       "https://httpbin.org/headers",
		Method:    "GET",
		FetchMode: foxhound.FetchStatic,
		Domain:    "httpbin.org",
	}

	headersResp, err := f.Fetch(ctx, headersJob)
	if err != nil {
		t.Fatalf("Headers fetch failed: %v", err)
	}

	headersBody := string(headersResp.Body)
	t.Logf("Headers response: %s", truncate(headersBody, 500))

	// Verify User-Agent is set (not Go default)
	if strings.Contains(headersBody, "Go-http-client") {
		t.Error("User-Agent is default Go client — identity not applied")
	}
	if strings.Contains(headersBody, "Firefox") || strings.Contains(headersBody, "Mozilla") {
		t.Log("User-Agent correctly set to Firefox/Mozilla")
	}
}

// ---------------------------------------------------------------------------
// Test 8: Identity consistency in real request
// ---------------------------------------------------------------------------

func TestIdentityConsistencyInRequest(t *testing.T) {
	prof := setupIdentity()
	f := fetch.NewStealth(
		fetch.WithIdentity(prof),
		fetch.WithProxy(proxyURL),
		fetch.WithTimeout(30*time.Second),
	)
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Fetch a page that echoes headers
	job := &foxhound.Job{
		ID:        "identity-check",
		URL:       "https://httpbin.org/headers",
		Method:    "GET",
		FetchMode: foxhound.FetchStatic,
		Domain:    "httpbin.org",
	}

	resp, err := f.Fetch(ctx, job)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	body := string(resp.Body)

	// Verify UA matches identity
	if !strings.Contains(body, "Firefox") {
		t.Error("Firefox not in echoed User-Agent header")
	}

	// Verify Accept-Language is set
	if !strings.Contains(body, "Accept-Language") {
		t.Error("Accept-Language header not sent")
	}

	// Verify the UA in the request matches what we generated
	if !strings.Contains(body, prof.UA[:30]) {
		t.Logf("Warning: echoed UA may not match generated UA exactly")
		t.Logf("Generated UA: %s", prof.UA)
	} else {
		t.Log("UA matches generated identity profile")
	}

	// Log all echoed headers for debugging
	t.Logf("Echoed headers: %s", truncate(body, 800))
}

// ---------------------------------------------------------------------------
// Test 9: Response.CSS() convenience selectors
// ---------------------------------------------------------------------------

func TestResponseCSSSelectors(t *testing.T) {
	f := setupStealth(t)
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use a well-structured site
	job := &foxhound.Job{
		ID:        "css-selectors",
		URL:       "https://books.toscrape.com/",
		Method:    "GET",
		FetchMode: foxhound.FetchStatic,
		Domain:    "books.toscrape.com",
	}

	resp, err := f.Fetch(ctx, job)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	// Test Response.CSS() convenience method
	titles := resp.CSS("article.product_pod h3 a").Attrs("title")
	t.Logf("Found %d book titles via Response.CSS()", len(titles))
	for i, title := range titles {
		if i >= 5 {
			break
		}
		t.Logf("  [%d] %s", i+1, title)
	}

	prices := resp.CSS("article.product_pod .price_color").Texts()
	t.Logf("Found %d prices", len(prices))

	if len(titles) == 0 || len(prices) == 0 {
		t.Error("CSS selectors returned no results on books.toscrape.com")
	}

	// Verify count
	count := resp.CSS("article.product_pod").Len()
	t.Logf("Product count: %d", count)
	if count < 10 {
		t.Errorf("Expected at least 10 products, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Test 10: Timeout handling
// ---------------------------------------------------------------------------

func TestTimeoutHandling(t *testing.T) {
	f := fetch.NewStealth(
		fetch.WithIdentity(setupIdentity()),
		fetch.WithProxy(proxyURL),
		fetch.WithTimeout(2*time.Second), // very short timeout
	)
	defer f.Close()

	// Use a very short context timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	job := &foxhound.Job{
		ID:        "timeout-test",
		URL:       "https://httpbin.org/delay/10", // 10 second delay
		Method:    "GET",
		FetchMode: foxhound.FetchStatic,
		Domain:    "httpbin.org",
	}

	t.Log("Fetching with 1s context timeout against 10s delay...")
	_, err := f.Fetch(ctx, job)

	if err == nil {
		t.Error("Expected timeout error, got nil")
	} else {
		t.Logf("Got expected error: %v", err)
		if !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "deadline") {
			t.Logf("Warning: error may not be a timeout: %v", err)
		}
	}
}
