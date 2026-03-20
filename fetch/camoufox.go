//go:build !playwright

// camoufox.go — Camoufox browser fetcher stub (default build, no playwright).
//
// This file is compiled when the "playwright" build tag is NOT present.
// It provides the correct public types, option functions, and a CamoufoxFetcher
// whose Fetch method returns a clear, actionable error so callers know exactly
// what is missing.
//
// To use the real playwright-go implementation, compile with:
//
//	go build -tags playwright ./...
//	go test  -tags playwright ./fetch/...
//
// Why Camoufox over Chromium:
//   - Juggler protocol is far less targeted by anti-bot systems than CDP.
//   - Firefox source is open for C++ patching; CAMOU_CONFIG env vars control
//     screen, locale, hardware, and GPU — all surfaced through navigator APIs.
//   - Reference: https://camoufox.com

package fetch

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/identity"
)

// errPlaywrightNotConfigured is returned by the stub Fetch implementation.
var errPlaywrightNotConfigured = errors.New(
	"camoufox: playwright-go not configured — rebuild with: go build -tags playwright ./...\n" +
		"  Then install the browser: go run github.com/playwright-community/playwright-go/cmd/playwright install firefox",
)

// CamoufoxOption is a functional option for configuring a CamoufoxFetcher.
type CamoufoxOption func(*CamoufoxFetcher)

// WithBrowserIdentity sets the identity profile used to configure the Camoufox
// browser context. All CAMOU_CONFIG environment variables are derived from p.
func WithBrowserIdentity(p *identity.Profile) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.identity = p
	}
}

// WithBlockImages controls whether the browser route handler intercepts and
// aborts image/media/font requests. Reduces bandwidth and speeds up navigation
// for content-only scraping.
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
// memory leaks) and rotates the browser fingerprint.
//
// Set n=0 to disable automatic restarts (not recommended for long-running
// hunts). The default is 300.
func WithMaxBrowserRequests(n int) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.maxRequests = n
	}
}

// WithPersistSession controls whether the same BrowserContext is reused
// across requests for the same walker session (cookies and localStorage are
// preserved between fetches). When false (default) a fresh context is created
// per fetch for full isolation between requests.
//
// Use true when scraping sites that require login state to be maintained
// across multiple page visits in a single session.
func WithPersistSession(persist bool) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.persistSession = persist
	}
}

// CamoufoxFetcher drives a Camoufox (Firefox fork) browser instance via the
// Juggler protocol using playwright-go.
//
// Stub build (!playwright tag): Fetch always returns errPlaywrightNotConfigured.
// Real build  ( playwright tag): see camoufox_playwright.go.
type CamoufoxFetcher struct {
	identity       *identity.Profile
	blockImages    bool
	headless       string
	timeout        time.Duration
	proxyURL       string // SOCKS5 or HTTP proxy URL
	extensionPath  string // path to Firefox extension dir (e.g. NopeCHA)
	maxRequests    int    // restart browser after this many requests (0 = disabled)
	persistSession bool   // reuse BrowserContext across requests when true
	initScript     string // JS injected into every new page via AddInitScript
	userDataDir    string // persistent profile dir; triggers LaunchPersistentContext
	cdpURL          string           // connect to an existing browser via CDP instead of launching
	useRealChrome   bool             // use pw.Chromium with channel=chrome instead of Firefox
	capturePatterns []*regexp.Regexp // URL patterns for XHR/fetch response capture
}

// WithBrowserProxy sets the proxy URL for all browser requests.
// Supports SOCKS5 (socks5://user:pass@host:port) and HTTP (http://user:pass@host:port).
func WithBrowserProxy(proxyURL string) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.proxyURL = proxyURL
	}
}

// WithExtensionPath sets the path to a Firefox extension directory to load.
// By default, NopeCHA is auto-downloaded and loaded. Set to "none" to disable.
// In the stub build this stores the value but has no effect — the extension
// is only loaded when compiled with the playwright build tag.
func WithExtensionPath(path string) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.extensionPath = path
	}
}

// WithInitScript sets a JavaScript snippet that is injected into every new
// page before the page's own scripts execute. This is useful for overriding
// navigator properties (e.g. navigator.webdriver) or installing global hooks.
// In the stub build this stores the value but has no effect.
func WithInitScript(script string) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.initScript = script
	}
}

// WithUserDataDir sets a persistent profile directory. When non-empty the real
// build uses LaunchPersistentContext so cookies, localStorage, and cached
// resources survive across browser restarts. In the stub build this stores the
// value but has no effect.
func WithUserDataDir(dir string) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.userDataDir = dir
	}
}

// WithCDPURL sets a Chrome DevTools Protocol endpoint URL (e.g.
// "http://localhost:9222"). When non-empty the real build connects to that
// existing browser instance via pw.Chromium.ConnectOverCDP instead of
// launching a new browser process. In the stub build this stores the value but
// has no effect.
func WithCDPURL(url string) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.cdpURL = url
	}
}

// WithRealChrome switches the real build from Firefox/Camoufox to
// pw.Chromium.Launch with channel="chrome". Use this when you need Chrome's
// rendering behaviour or have a Chrome installation but not Camoufox. Falls
// back to Chromium if the Chrome channel is not installed. In the stub build
// this stores the value but has no effect.
func WithRealChrome(use bool) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.useRealChrome = use
	}
}

// NewCamoufox creates a CamoufoxFetcher. In the stub build no browser is
// launched; the constructor stores the configuration and returns immediately.
func NewCamoufox(opts ...CamoufoxOption) (*CamoufoxFetcher, error) {
	f := &CamoufoxFetcher{
		headless:    "virtual",
		timeout:     60 * time.Second,
		maxRequests: 300,
	}
	for _, opt := range opts {
		opt(f)
	}

	slog.Info("fetch/camoufox: stub initialised (playwright-go not yet configured)",
		"headless", f.headless,
		"block_images", f.blockImages,
	)
	return f, nil
}

// Fetch always returns errPlaywrightNotConfigured in the stub build.
// Rebuild with -tags playwright to enable real browser navigation.
func (f *CamoufoxFetcher) Fetch(_ context.Context, _ *foxhound.Job) (*foxhound.Response, error) {
	return nil, errPlaywrightNotConfigured
}

// Close is a no-op in the stub build; no resources were allocated.
func (f *CamoufoxFetcher) Close() error {
	return nil
}

// detectCloudflare is a no-op stub. Real implementation is in camoufox_playwright.go.
func (f *CamoufoxFetcher) detectCloudflare(_ interface{}) string { return "" }

// handleCloudflare is a no-op stub. Real implementation is in camoufox_playwright.go.
func (f *CamoufoxFetcher) handleCloudflare(_ interface{}) bool { return false }
