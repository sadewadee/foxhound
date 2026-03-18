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
	"math/rand/v2"
	neturl "net/url"
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

// WithPersistSession controls whether the same BrowserContext is reused
// across requests for the same walker session. When true, cookies and
// localStorage are preserved between fetches — necessary for sites that
// require login state to be maintained across page visits. When false
// (default) a fresh isolated context is created per fetch.
func WithPersistSession(persist bool) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.persistSession = persist
	}
}

// CamoufoxFetcher drives a Camoufox (Firefox fork) browser instance via the
// Juggler protocol using playwright-go.
//
// A single browser instance is shared; each Fetch call creates an isolated
// BrowserContext so cookies and sessions never bleed across requests.
// The browser is automatically restarted every maxRequests fetches to clear
// accumulated state and rotate the fingerprint.
//
// When persistSession=true a single BrowserContext is reused across all Fetch
// calls so cookies, localStorage, and session state are preserved between
// requests — useful for sites that require login state.
type CamoufoxFetcher struct {
	pw             *playwright.Playwright
	browser        playwright.Browser
	identity       *identity.Profile
	blockImages    bool
	headless       string
	timeout        time.Duration
	proxyURL       string       // SOCKS5 or HTTP proxy URL
	maxRequests    int          // restart after this many fetches (0 = disabled)
	requestCount   atomic.Int64 // total fetches served by the current browser instance
	mu             sync.Mutex   // serialises browser restart
	persistSession bool
	sessionCtx     playwright.BrowserContext // reused when persistSession=true
	sessionMu      sync.Mutex               // guards sessionCtx lifecycle
}

// WithBrowserProxy sets the proxy URL for all browser requests.
// Supports SOCKS5 (socks5://user:pass@host:port) and HTTP (http://user:pass@host:port).
func WithBrowserProxy(proxyURL string) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.proxyURL = proxyURL
	}
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

	// Set proxy at browser launch level — Firefox requires this for SOCKS5.
	if f.proxyURL != "" {
		proxyOpt := parsePlaywrightProxy(f.proxyURL)
		launchOpts.Proxy = proxyOpt
		slog.Info("fetch/camoufox: browser launching with proxy", "server", proxyOpt.Server)
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

// getOrCreateContext returns the BrowserContext to use for this request.
// When persistSession=false a new isolated context is created (caller must
// close it). When persistSession=true the shared sessionCtx is initialised
// on first call and reused; the caller must NOT close it.
// Returns (ctx, shouldClose, err).
func (f *CamoufoxFetcher) getOrCreateContext() (playwright.BrowserContext, bool, error) {
	if !f.persistSession {
		bctx, err := f.browser.NewContext(f.buildContextOptions())
		return bctx, true, err // caller should close
	}

	f.sessionMu.Lock()
	defer f.sessionMu.Unlock()

	if f.sessionCtx == nil {
		bctx, err := f.browser.NewContext(f.buildContextOptions())
		if err != nil {
			return nil, false, err
		}
		f.sessionCtx = bctx
	}
	return f.sessionCtx, false, nil // caller must NOT close
}

// simulateHumanBehavior performs lightweight mouse and scroll actions after a
// page has finished loading. These interactions prove to behavioural anti-bot
// systems that a real user is present and interacting with the page.
//
// Actions performed (all are best-effort; errors are silently ignored so the
// underlying content extraction is not blocked by a failed gesture):
//  1. Random reading pause after page load (1-3 s)
//  2. Move mouse to a random viewport position
//  3. Scroll down 200-600 px
//  4. Optionally scroll back up (30 % probability, natural reading pattern)
//  5. Move mouse to a second random viewport position
func (f *CamoufoxFetcher) simulateHumanBehavior(page playwright.Page) {
	// 1. Reading pause — simulates time-to-first-interaction after load.
	time.Sleep(time.Duration(1000+rand.IntN(2000)) * time.Millisecond)

	// 2. Move mouse to a random position in the visible viewport.
	viewportSize := page.ViewportSize()
	if viewportSize != nil && viewportSize.Width > 100 && viewportSize.Height > 100 {
		x := float64(rand.IntN(viewportSize.Width-100) + 50)
		y := float64(rand.IntN(viewportSize.Height-100) + 50)
		if err := page.Mouse().Move(x, y); err != nil {
			slog.Debug("fetch/camoufox: simulateHuman: mouse move 1 failed", "err", err)
		}
		time.Sleep(time.Duration(200+rand.IntN(300)) * time.Millisecond)
	}

	// 3. Scroll down — shows the user is reading past the fold.
	scrollY := float64(200 + rand.IntN(400))
	if err := page.Mouse().Wheel(0, scrollY); err != nil {
		slog.Debug("fetch/camoufox: simulateHuman: scroll down failed", "err", err)
	}
	time.Sleep(time.Duration(500+rand.IntN(1000)) * time.Millisecond)

	// 4. Occasionally scroll back up — natural reading / re-reading behaviour.
	if rand.Float64() < 0.3 {
		scrollUp := float64(100 + rand.IntN(200))
		if err := page.Mouse().Wheel(0, -scrollUp); err != nil {
			slog.Debug("fetch/camoufox: simulateHuman: scroll up failed", "err", err)
		}
		time.Sleep(time.Duration(300+rand.IntN(500)) * time.Millisecond)
	}

	// 5. Move mouse to a second random viewport position.
	if viewportSize != nil && viewportSize.Width > 100 && viewportSize.Height > 100 {
		x := float64(rand.IntN(viewportSize.Width-100) + 50)
		y := float64(rand.IntN(viewportSize.Height-100) + 50)
		if err := page.Mouse().Move(x, y); err != nil {
			slog.Debug("fetch/camoufox: simulateHuman: mouse move 2 failed", "err", err)
		}
	}
}

// handleCookieConsent attempts to click common cookie-consent accept buttons.
// It tries each selector in order, clicking the first visible match and
// returning immediately. If no consent button is found the call is a no-op.
//
// Accepting cookie banners prevents the banner from obscuring page content
// and matches the behaviour of a real user who dismisses these prompts.
func (f *CamoufoxFetcher) handleCookieConsent(page playwright.Page) {
	consentSelectors := []string{
		"button[id*='accept']",
		"button[class*='accept']",
		"button[id*='consent']",
		"a[id*='accept']",
		"[data-testid*='accept']",
		"button:has-text('Accept')",
		"button:has-text('I agree')",
		"button:has-text('Accept all')",
		"button:has-text('Accept All')",
		"button:has-text('Terima')",  // Indonesian
		"button:has-text('Setuju')",  // Indonesian
		"#onetrust-accept-btn-handler",
		".cookie-accept",
		"#CybotCookiebotDialogBodyLevelButtonLevelOptinAllowAll",
	}

	for _, sel := range consentSelectors {
		el, err := page.QuerySelector(sel)
		if err != nil || el == nil {
			continue
		}
		visible, visErr := el.IsVisible()
		if visErr != nil || !visible {
			continue
		}
		if clickErr := el.Click(); clickErr != nil {
			slog.Debug("fetch/camoufox: cookie consent click failed",
				"selector", sel, "err", clickErr)
			continue
		}
		slog.Debug("fetch/camoufox: clicked cookie consent", "selector", sel)
		time.Sleep(500 * time.Millisecond)
		return
	}
}

// handleRecaptcha detects and attempts to solve reCAPTCHA v2 checkbox challenges.
// reCAPTCHA renders inside an iframe — we locate the iframe, find the checkbox,
// move the mouse naturally toward it, and click. If Google's behavioral score
// (based on prior mouse movements + timing) is good enough, the checkbox
// resolves immediately without an image challenge.
func (f *CamoufoxFetcher) handleRecaptcha(page playwright.Page) {
	// Check if page contains reCAPTCHA indicators
	hasRecaptcha := false
	indicators := []string{
		"iframe[src*='recaptcha']",
		"iframe[src*='google.com/recaptcha']",
		"#captcha-form",
		".g-recaptcha",
		"div[class*='recaptcha']",
	}
	for _, sel := range indicators {
		el, err := page.QuerySelector(sel)
		if err == nil && el != nil {
			hasRecaptcha = true
			break
		}
	}
	if !hasRecaptcha {
		return
	}

	slog.Info("fetch/camoufox: reCAPTCHA detected, attempting to solve...")

	// Small random delay before interacting — a real user pauses to read
	time.Sleep(time.Duration(1500+rand.IntN(2000)) * time.Millisecond)

	// Strategy 1: Find the reCAPTCHA iframe and click the checkbox inside it
	iframeSelectors := []string{
		"iframe[src*='recaptcha/api2/anchor']",
		"iframe[src*='recaptcha/enterprise/anchor']",
		"iframe[title*='reCAPTCHA']",
		"iframe[src*='google.com/recaptcha']",
	}

	for _, iframeSel := range iframeSelectors {
		iframeEl, err := page.QuerySelector(iframeSel)
		if err != nil || iframeEl == nil {
			continue
		}

		frame, err := iframeEl.ContentFrame()
		if err != nil || frame == nil {
			continue
		}

		// The checkbox inside the reCAPTCHA iframe
		checkboxSelectors := []string{
			"#recaptcha-anchor",
			".recaptcha-checkbox",
			"span[role='checkbox']",
			"div.recaptcha-checkbox-border",
		}

		for _, cbSel := range checkboxSelectors {
			cb, err := frame.QuerySelector(cbSel)
			if err != nil || cb == nil {
				continue
			}

			visible, _ := cb.IsVisible()
			if !visible {
				continue
			}

			// Move mouse toward the checkbox with human-like trajectory
			box, err := cb.BoundingBox()
			if err == nil && box != nil {
				// Move to a random point near the checkbox (not dead center)
				targetX := box.X + box.Width*0.3 + float64(rand.IntN(int(box.Width*0.4)))
				targetY := box.Y + box.Height*0.3 + float64(rand.IntN(int(box.Height*0.4)))

				// Slow, multi-step mouse movement (human-like)
				currentX := float64(200 + rand.IntN(400))
				currentY := float64(200 + rand.IntN(200))
				steps := 8 + rand.IntN(8)
				for step := 1; step <= steps; step++ {
					t := float64(step) / float64(steps)
					// Ease-in-out curve
					t = t * t * (3 - 2*t)
					mx := currentX + (targetX-currentX)*t + float64(rand.IntN(3)-1)
					my := currentY + (targetY-currentY)*t + float64(rand.IntN(3)-1)
					page.Mouse().Move(mx, my)
					time.Sleep(time.Duration(20+rand.IntN(40)) * time.Millisecond)
				}

				// Small pause before click (human reaction time)
				time.Sleep(time.Duration(200+rand.IntN(300)) * time.Millisecond)
			}

			// Click the checkbox
			if err := cb.Click(); err != nil {
				slog.Debug("fetch/camoufox: reCAPTCHA checkbox click failed", "err", err)
				continue
			}

			slog.Info("fetch/camoufox: reCAPTCHA checkbox clicked, waiting for verification...")

			// Wait for Google to verify — this takes 2-5 seconds
			time.Sleep(time.Duration(3000+rand.IntN(3000)) * time.Millisecond)

			// Check if solved (checkbox gets aria-checked="true")
			checked, _ := cb.GetAttribute("aria-checked")
			if checked == "true" {
				slog.Info("fetch/camoufox: reCAPTCHA SOLVED via checkbox click!")

				// If there's a submit button after solving, click it
				f.submitAfterCaptcha(page)

				// Wait for page to load after form submission
				time.Sleep(time.Duration(2000+rand.IntN(2000)) * time.Millisecond)
				page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
					State: playwright.LoadStateNetworkidle,
				})
				return
			}

			slog.Info("fetch/camoufox: reCAPTCHA checkbox clicked but not solved (may need image challenge)")
			// Even if not solved, we tried — image challenges need external solvers
			return
		}
	}

	// Strategy 2: Some pages embed reCAPTCHA inline (not in iframe)
	inlineSelectors := []string{
		"#recaptcha-anchor",
		".recaptcha-checkbox",
		"span[role='checkbox'][aria-labelledby*='recaptcha']",
	}
	for _, sel := range inlineSelectors {
		el, err := page.QuerySelector(sel)
		if err != nil || el == nil {
			continue
		}
		visible, _ := el.IsVisible()
		if !visible {
			continue
		}
		slog.Info("fetch/camoufox: clicking inline reCAPTCHA checkbox")
		el.Click()
		time.Sleep(time.Duration(3000+rand.IntN(3000)) * time.Millisecond)
		return
	}

	slog.Warn("fetch/camoufox: reCAPTCHA found but could not locate clickable checkbox")
}

// submitAfterCaptcha looks for and clicks a submit button after CAPTCHA is solved.
func (f *CamoufoxFetcher) submitAfterCaptcha(page playwright.Page) {
	submitSelectors := []string{
		"#captcha-form input[type='submit']",
		"#captcha-form button[type='submit']",
		"button:has-text('Submit')",
		"button:has-text('Continue')",
		"button:has-text('Verify')",
		"input[type='submit']",
	}
	for _, sel := range submitSelectors {
		el, err := page.QuerySelector(sel)
		if err != nil || el == nil {
			continue
		}
		visible, _ := el.IsVisible()
		if !visible {
			continue
		}
		time.Sleep(time.Duration(500+rand.IntN(500)) * time.Millisecond)
		el.Click()
		slog.Info("fetch/camoufox: submitted form after CAPTCHA solve", "selector", sel)
		return
	}
}

// navigate performs the actual playwright navigation. It is called from a
// goroutine so that context cancellation can abort it cleanly.
func (f *CamoufoxFetcher) navigate(job *foxhound.Job) (*foxhound.Response, error) {
	// Obtain a BrowserContext — either a fresh one or the shared session
	// context, depending on the persistSession configuration.
	bctx, shouldClose, err := f.getOrCreateContext()
	if err != nil {
		return nil, fmt.Errorf("fetch/camoufox: creating browser context for %s: %w", job.URL, err)
	}
	if shouldClose {
		defer func() {
			if closeErr := bctx.Close(); closeErr != nil {
				slog.Warn("fetch/camoufox: error closing browser context",
					"url", job.URL,
					"err", closeErr,
				)
			}
		}()
	}

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

	// Human behaviour simulation — makes page interaction look natural to
	// anti-bot behavioural analysis (Layer 4 defence). These actions run after
	// the network becomes idle so they do not interfere with page rendering.
	f.simulateHumanBehavior(page)

	// Dismiss cookie consent banners if present. A real user accepts these
	// prompts; leaving them open can obscure content and signal automation.
	f.handleCookieConsent(page)

	// Attempt to solve reCAPTCHA v2 checkbox ("I'm not a robot").
	// Must happen AFTER human simulation so Google's behavioral score
	// considers the mouse movements and delays before the click.
	f.handleRecaptcha(page)

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

// parsePlaywrightProxy converts a proxy URL like
// "socks5://user:pass@host:port" into a playwright.Proxy struct
// with Server, Username, and Password separated (required by Playwright).
func parsePlaywrightProxy(rawURL string) *playwright.Proxy {
	proxy := &playwright.Proxy{}

	// Parse the URL to extract components
	u, err := neturl.Parse(rawURL)
	if err != nil {
		// Fallback: use as-is
		proxy.Server = rawURL
		return proxy
	}

	// Server = scheme://host:port (no auth)
	proxy.Server = fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	// Extract username and password separately
	if u.User != nil {
		username := u.User.Username()
		proxy.Username = &username
		if pass, ok := u.User.Password(); ok {
			proxy.Password = &pass
		}
	}

	return proxy
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
// Resources are released in order: persistent session context (if any),
// browser, then the playwright process. Errors from each step are logged but
// do not prevent the subsequent steps from running.
func (f *CamoufoxFetcher) Close() error {
	var firstErr error

	// Close the persistent session context first so its resources are freed
	// before the browser itself is torn down.
	f.sessionMu.Lock()
	if f.sessionCtx != nil {
		if closeErr := f.sessionCtx.Close(); closeErr != nil {
			if !strings.Contains(closeErr.Error(), "Target closed") &&
				!strings.Contains(closeErr.Error(), "Browser has been closed") {
				slog.Warn("fetch/camoufox: error closing persistent session context", "err", closeErr)
				firstErr = fmt.Errorf("fetch/camoufox: closing session context: %w", closeErr)
			}
		}
		f.sessionCtx = nil
	}
	f.sessionMu.Unlock()

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
