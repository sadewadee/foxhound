//go:build playwright

// camoufox_playwright.go — real Camoufox browser fetcher using playwright-go.
//
// Compiled only when the "playwright" build tag is present:
//
//	go build -tags playwright ./...
//	go test  -tags playwright ./fetch/...
//
// Prerequisites:
//  1. Add playwright-go to the module:
//     go get github.com/playwright-community/playwright-go@latest
//  2. Install the Firefox (Camoufox) binary once per environment:
//     go run github.com/playwright-community/playwright-go/cmd/playwright install firefox
//
// Design notes:
//   - One playwright.Playwright + one playwright.Browser is shared across all
//     Fetch calls; a fresh playwright.BrowserContext is created per Fetch to
//     provide per-request cookie/session isolation.
//   - Camoufox CAMOU_CONFIG environment vars come from identity.Profile.CamoufoxEnv
//     and are injected via playwright BrowserTypeLaunchOptions.Env.
//   - Images, media, and fonts are intercepted and aborted when blockImages=true
//     to reduce bandwidth for content-only scraping.
//   - Navigation uses WaitUntilStateNetworkidle so dynamic JS-rendered pages are
//     fully resolved before content is extracted.

package fetch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/identity"
	"github.com/playwright-community/playwright-go"
)

// defaultBrowserTimeout is the per-navigation ceiling when no explicit timeout
// is set on the CamoufoxFetcher.
const defaultBrowserTimeout = 60 * time.Second

// resourceBlockPatterns is the route glob list aborted when blockImages=true.
// Blocking binary resources cuts typical page-load time by 30–70 % for
// content-only scraping.
var resourceBlockPatterns = []string{
	"**/*.{png,jpg,jpeg,gif,svg,webp,ico,avif,bmp,tiff}",
	"**/*.{mp4,webm,ogg,mp3,wav,flac}",
	"**/*.{woff,woff2,ttf,otf,eot}",
}

// CamoufoxOption is a functional option for configuring a CamoufoxFetcher.
type CamoufoxOption func(*CamoufoxFetcher)

// WithBrowserIdentity sets the identity profile used to configure the Camoufox
// browser context and CAMOU_CONFIG environment variables.
func WithBrowserIdentity(p *identity.Profile) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.identity = p
	}
}

// WithBlockImages controls whether image/media/font requests are intercepted
// and aborted to reduce bandwidth for content-only scraping.
func WithBlockImages(block bool) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.blockImages = block
	}
}

// WithHeadless sets the display mode for the Camoufox browser:
//   - "virtual":  Xvfb virtual display (recommended on servers without a GPU)
//   - "true":     native headless mode
//   - "false":    full visible browser (useful for local debugging)
func WithHeadless(mode string) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.headless = mode
	}
}

// WithBrowserTimeout overrides the default per-navigation timeout.
func WithBrowserTimeout(d time.Duration) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.timeout = d
	}
}

// WithMaxBrowserRequests configures the browser instance to be restarted
// after serving n requests. This clears accumulated state (cookies, cache,
// memory leaks) and effectively rotates the browser fingerprint over long runs.
//
// Set n=0 to disable automatic restarts. The default is 300.
func WithMaxBrowserRequests(n int) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.maxRequests = n
	}
}

// CamoufoxFetcher drives a Camoufox (Firefox fork) browser instance via the
// Juggler protocol using playwright-go.
//
// A single browser instance is shared; each Fetch call creates an isolated
// BrowserContext so cookies and sessions never bleed across requests.
// The browser is automatically restarted every maxRequests fetches to clear
// accumulated state and rotate the fingerprint.
type CamoufoxFetcher struct {
	pw           *playwright.Playwright
	browser      playwright.Browser
	identity     *identity.Profile
	blockImages  bool
	headless     string
	timeout      time.Duration
	maxRequests  int           // restart after this many fetches (0 = disabled)
	requestCount atomic.Int64  // total fetches served by the current browser instance
	mu           sync.Mutex    // serialises browser restart
}

// NewCamoufox initialises playwright, applies the supplied options, launches a
// Firefox (Camoufox) browser, and returns a ready-to-use CamoufoxFetcher.
//
// The browser is kept alive until Close is called. If the playwright runtime or
// the Firefox binary is not installed, NewCamoufox returns a descriptive error.
func NewCamoufox(opts ...CamoufoxOption) (*CamoufoxFetcher, error) {
	f := &CamoufoxFetcher{
		headless:    "virtual",
		timeout:     defaultBrowserTimeout,
		maxRequests: 300,
	}
	for _, opt := range opts {
		opt(f)
	}

	// Start the playwright runtime. playwright.Run() downloads the driver on
	// first call; subsequent calls reuse the already-installed driver.
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("fetch/camoufox: starting playwright runtime: %w", err)
	}

	// Build CAMOU_CONFIG environment overrides from the identity profile so
	// Camoufox reports the correct screen size, locale, GPU, and hardware.
	env := camoufoxEnvFromProfile(f.identity)

	headlessBool := f.headless != "false"

	launchOpts := playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(headlessBool),
		Env:      env,
		// Juggler is the Firefox-specific remote-debugging protocol used by
		// Camoufox.  Passing an empty Args slice keeps defaults; Juggler is
		// automatically selected by playwright-go for Firefox.
	}

	browser, err := pw.Firefox.Launch(launchOpts)
	if err != nil {
		// Auto-install Firefox if not found. This covers first-run scenarios
		// so users never need to run `playwright install firefox` manually.
		slog.Info("fetch/camoufox: Firefox not found, auto-installing...")
		if installErr := playwright.Install(&playwright.RunOptions{
			Browsers: []string{"firefox"},
		}); installErr != nil {
			_ = pw.Stop()
			return nil, fmt.Errorf("fetch/camoufox: auto-install firefox failed: %w (original launch error: %v)", installErr, err)
		}
		slog.Info("fetch/camoufox: Firefox installed successfully, retrying launch")
		browser, err = pw.Firefox.Launch(launchOpts)
		if err != nil {
			_ = pw.Stop()
			return nil, fmt.Errorf("fetch/camoufox: launching firefox after auto-install: %w", err)
		}
	}

	f.pw = pw
	f.browser = browser

	slog.Info("fetch/camoufox: browser launched",
		"headless", f.headless,
		"block_images", f.blockImages,
		"timeout", f.timeout,
	)
	return f, nil
}

// Fetch navigates to job.URL inside a fresh, isolated BrowserContext, waits
// for the network to become idle, extracts the fully-rendered HTML, and returns
// a foxhound.Response with FetchMode=FetchBrowser.
//
// Each call creates a new BrowserContext (and therefore a new cookie jar,
// localStorage, and cache partition) so requests cannot share state unless the
// caller deliberately reuses a context — which this implementation intentionally
// avoids to prevent identity leakage across scraping targets.
//
// When the number of completed fetches reaches maxRequests, the browser is
// automatically restarted before continuing. The restart is serialised under a
// mutex so concurrent Fetch calls never race on the browser handle.
func (f *CamoufoxFetcher) Fetch(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
	if job == nil {
		return nil, fmt.Errorf("fetch/camoufox: job must not be nil")
	}
	if job.URL == "" {
		return nil, fmt.Errorf("fetch/camoufox: job URL must not be empty")
	}

	// Lifecycle: restart the browser after maxRequests fetches.
	if f.maxRequests > 0 {
		count := f.requestCount.Add(1)
		if count > int64(f.maxRequests) {
			if err := f.restart(); err != nil {
				// Non-fatal: log and continue with the existing browser.
				slog.Warn("fetch/camoufox: browser restart failed, continuing with existing instance",
					"err", err,
					"request_count", count,
				)
			}
		}
	}

	// Propagate context cancellation into playwright by running navigation in a
	// goroutine and aborting via page.Close on context done.
	type result struct {
		resp *foxhound.Response
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		resp, err := f.navigate(job)
		ch <- result{resp, err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("fetch/camoufox: context cancelled for %s: %w", job.URL, ctx.Err())
	case res := <-ch:
		return res.resp, res.err
	}
}

// navigate performs the actual playwright navigation. It is called from a
// goroutine so that context cancellation can abort it cleanly.
func (f *CamoufoxFetcher) navigate(job *foxhound.Job) (*foxhound.Response, error) {
	// Create an isolated context for this request so sessions never bleed.
	bctx, err := f.browser.NewContext(f.buildContextOptions())
	if err != nil {
		return nil, fmt.Errorf("fetch/camoufox: creating browser context for %s: %w", job.URL, err)
	}
	defer func() {
		if closeErr := bctx.Close(); closeErr != nil {
			slog.Warn("fetch/camoufox: error closing browser context",
				"url", job.URL,
				"err", closeErr,
			)
		}
	}()

	// Inject Accept-Language from the identity profile as an extra header so
	// the browser reports the correct locale to the server.
	if f.identity != nil && len(f.identity.Languages) > 0 {
		if err := bctx.SetExtraHTTPHeaders(map[string]string{
			"Accept-Language": buildAcceptLanguage(f.identity.Languages),
		}); err != nil {
			slog.Warn("fetch/camoufox: could not set Accept-Language header",
				"url", job.URL,
				"err", err,
			)
		}
	}

	page, err := bctx.NewPage()
	if err != nil {
		return nil, fmt.Errorf("fetch/camoufox: opening page for %s: %w", job.URL, err)
	}
	defer func() {
		if closeErr := page.Close(); closeErr != nil {
			slog.Warn("fetch/camoufox: error closing page",
				"url", job.URL,
				"err", closeErr,
			)
		}
	}()

	// Block binary resources to reduce bandwidth for content-only scraping.
	if f.blockImages {
		for _, pattern := range resourceBlockPatterns {
			p := pattern // capture for closure
			if routeErr := page.Route(p, func(route playwright.Route) {
				if abortErr := route.Abort(); abortErr != nil {
					slog.Debug("fetch/camoufox: route abort error",
						"pattern", p,
						"err", abortErr,
					)
				}
			}); routeErr != nil {
				slog.Warn("fetch/camoufox: could not install route handler",
					"pattern", p,
					"err", routeErr,
				)
			}
		}
	}

	timeoutMs := float64(f.timeout.Milliseconds())

	slog.Debug("fetch/camoufox: navigating",
		"url", job.URL,
		"job_id", job.ID,
		"timeout_ms", timeoutMs,
	)

	start := time.Now()
	navResp, err := page.Goto(job.URL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
		Timeout:   playwright.Float(timeoutMs),
	})
	duration := time.Since(start)

	if err != nil {
		return nil, fmt.Errorf("fetch/camoufox: navigating to %s: %w", job.URL, err)
	}

	// Extract the fully-rendered HTML. page.Content() returns the live DOM
	// after all JS has executed, which is the primary reason to use a browser.
	content, err := page.Content()
	if err != nil {
		return nil, fmt.Errorf("fetch/camoufox: extracting content from %s: %w", job.URL, err)
	}

	// Collect status code and final URL from the navigation response.
	statusCode := 200
	finalURL := job.URL
	if navResp != nil {
		statusCode = navResp.Status()
		if u := navResp.URL(); u != "" {
			finalURL = u
		}
	}
	// page.URL() gives the current address after all client-side redirects,
	// which is more accurate than the navigation response URL for SPAs.
	if pageURL := page.URL(); pageURL != "" && pageURL != "about:blank" {
		finalURL = pageURL
	}

	slog.Debug("fetch/camoufox: navigation complete",
		"status", statusCode,
		"url", finalURL,
		"duration_ms", duration.Milliseconds(),
		"body_bytes", len(content),
		"job_id", job.ID,
	)

	return &foxhound.Response{
		StatusCode: statusCode,
		Headers:    make(map[string][]string),
		Body:       []byte(content),
		URL:        finalURL,
		FetchMode:  foxhound.FetchBrowser,
		Duration:   duration,
		Job:        job,
	}, nil
}

// buildContextOptions constructs playwright.BrowserNewContextOptions from the
// identity profile, applying all relevant attributes: UA, viewport, locale,
// timezone, geolocation, pixel ratio, and colour scheme.
//
// When no identity is set, a sensible Firefox-on-Windows default is returned so
// the browser still presents a plausible fingerprint.
func (f *CamoufoxFetcher) buildContextOptions() playwright.BrowserNewContextOptions {
	opts := playwright.BrowserNewContextOptions{
		ColorScheme: playwright.ColorSchemeLight,
	}

	if f.identity == nil {
		opts.UserAgent = playwright.String(
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:136.0) Gecko/20100101 Firefox/136.0",
		)
		opts.Viewport = &playwright.Size{Width: 1920, Height: 1080}
		opts.Locale = playwright.String("en-US")
		opts.TimezoneId = playwright.String("America/New_York")
		return opts
	}

	p := f.identity

	opts.UserAgent = playwright.String(p.UA)
	opts.Viewport = &playwright.Size{
		Width:  p.ScreenW,
		Height: p.ScreenH,
	}
	opts.Locale = playwright.String(p.Locale)
	opts.TimezoneId = playwright.String(p.Timezone)

	if p.PixelRatio != 0 {
		opts.DeviceScaleFactor = playwright.Float(p.PixelRatio)
	}

	// Only attach geolocation when real coordinates are present; a zero-zero
	// coordinate (null island) is worse than no geolocation because it is an
	// obvious detection signal.
	if p.Lat != 0 || p.Lng != 0 {
		opts.Geolocation = &playwright.Geolocation{
			Latitude:  p.Lat,
			Longitude: p.Lng,
		}
		opts.Permissions = []string{"geolocation"}
	}

	return opts
}

// camoufoxEnvFromProfile returns the CAMOU_CONFIG environment map from the
// identity profile, or an empty map when profile is nil.
// BrowserTypeLaunchOptions.Env takes map[string]string, which matches the
// Profile.CamoufoxEnv field directly — no copy is needed because the map is
// only ever read after the browser has been launched.
func camoufoxEnvFromProfile(p *identity.Profile) map[string]string {
	if p == nil || len(p.CamoufoxEnv) == 0 {
		return map[string]string{}
	}
	return p.CamoufoxEnv
}

// restart closes the current browser instance and launches a new one, resetting
// the request counter. It is serialised under mu so that concurrent Fetch calls
// do not race on the browser handle. If the new browser fails to launch the
// old instance (if still alive) is used and an error is returned.
func (f *CamoufoxFetcher) restart() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Double-check: another goroutine may have already restarted the browser
	// between the counter check and acquiring the lock.
	if f.requestCount.Load() <= int64(f.maxRequests) {
		return nil
	}

	slog.Info("fetch/camoufox: restarting browser instance",
		"request_count", f.requestCount.Load(),
		"max_requests", f.maxRequests,
	)

	// Close the existing browser (best-effort; errors are logged only).
	if f.browser != nil {
		if err := f.browser.Close(); err != nil {
			if !strings.Contains(err.Error(), "Target closed") &&
				!strings.Contains(err.Error(), "Browser has been closed") {
				slog.Warn("fetch/camoufox: error closing browser during restart", "err", err)
			}
		}
		f.browser = nil
	}

	// Launch a fresh browser with the same configuration.
	env := camoufoxEnvFromProfile(f.identity)
	headlessBool := f.headless != "false"
	browser, err := f.pw.Firefox.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(headlessBool),
		Env:      env,
	})
	if err != nil {
		return fmt.Errorf("fetch/camoufox: launching replacement browser: %w", err)
	}

	f.browser = browser
	f.requestCount.Store(0)

	slog.Info("fetch/camoufox: browser restarted successfully")
	return nil
}

// Close gracefully shuts down the browser and stops the playwright runtime.
// Resources are released in order: browser context (if any), browser, then the
// playwright process. Errors from each step are logged but do not prevent the
// subsequent steps from running.
func (f *CamoufoxFetcher) Close() error {
	var firstErr error

	if f.browser != nil {
		if err := f.browser.Close(); err != nil {
			// Filter out "Target closed" noise which is benign on shutdown.
			if !strings.Contains(err.Error(), "Target closed") &&
				!strings.Contains(err.Error(), "Browser has been closed") {
				slog.Error("fetch/camoufox: error closing browser", "err", err)
				firstErr = fmt.Errorf("fetch/camoufox: closing browser: %w", err)
			}
		}
		f.browser = nil
	}

	if f.pw != nil {
		if err := f.pw.Stop(); err != nil {
			slog.Error("fetch/camoufox: error stopping playwright runtime", "err", err)
			if firstErr == nil {
				firstErr = fmt.Errorf("fetch/camoufox: stopping playwright: %w", err)
			}
		}
		f.pw = nil
	}

	return firstErr
}
