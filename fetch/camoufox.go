package fetch

// camoufox.go — Camoufox browser fetcher (Phase 1 stub).
//
// This file contains the correct types and interface for the Camoufox browser
// fetcher. The real implementation requires playwright-go and a Camoufox
// (Firefox fork) binary, neither of which is wired up in Phase 1.
//
// Real implementation plan (Phase 2):
//   1. Import "github.com/playwright-community/playwright-go".
//   2. In NewCamoufox, call playwright.Run() to install/launch Camoufox via:
//        pw, err := playwright.Run()
//        browser, err := pw.Firefox.Launch(playwright.BrowserTypeLaunchOptions{
//            ExecutablePath: camoufoxBinaryPath(),
//            Headless:       playwright.Bool(headless == "true"),
//            Args: []string{"--juggler=9222"},  // Juggler, NOT CDP
//        })
//   3. For each Fetch call, create a BrowserContext using CAMOU_CONFIG env vars
//      from identity.Profile.CamoufoxEnv (screen, locale, timezone, GPU etc.),
//      then open a Page, navigate, wait for networkidle, extract content.
//   4. Block images/media via route interception when blockImages=true.
//   5. Use a context pool to avoid re-launching for every request.
//   6. In Close, call browser.Close() then pw.Stop().
//
// Why Camoufox over Chromium:
//   - Juggler protocol is far less targeted by anti-bot systems than CDP.
//   - Firefox source is open for C++ patching (CAMOU_CONFIG env vars control
//     screen, locale, hardware, GPU — all sent in navigator APIs).
//   - Reference: https://camoufox.com

import (
	"context"
	"errors"
	"log/slog"

	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/identity"
)

// errPlaywrightNotConfigured is returned by the stub Fetch implementation.
var errPlaywrightNotConfigured = errors.New(
	"camoufox: playwright-go not configured, install with: " +
		"go run github.com/playwright-community/playwright-go/cmd/playwright install firefox",
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
// aborts image/media requests. Reduces bandwidth and speeds up navigation for
// content-only scraping.
func WithBlockImages(block bool) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.blockImages = block
	}
}

// WithHeadless sets the headless mode for the Camoufox browser.
//   - "virtual":  use Xvfb virtual display (recommended for servers without GPU)
//   - "true":     native headless mode
//   - "false":    full visible browser (useful for debugging)
func WithHeadless(mode string) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.headless = mode
	}
}

// CamoufoxFetcher drives a Camoufox (Firefox fork) browser instance via the
// Juggler protocol using playwright-go.
//
// Phase 1: stub — Fetch always returns errPlaywrightNotConfigured.
// Phase 2: real implementation wires in playwright.Playwright + playwright.Browser.
type CamoufoxFetcher struct {
	identity    *identity.Profile
	blockImages bool
	// headless controls display mode: "virtual", "true", or "false".
	headless string

	// TODO(phase2): add the following fields once playwright-go is available:
	//   pw      *playwright.Playwright
	//   browser playwright.Browser
}

// NewCamoufox creates a CamoufoxFetcher. In Phase 1 no browser is launched;
// the constructor simply stores the configuration for when playwright-go is
// wired in during Phase 2.
//
// In Phase 2 this function will:
//   1. Call playwright.Run() to ensure Camoufox is installed.
//   2. Launch a Firefox (Camoufox) browser instance with Juggler enabled.
//   3. Return an error if the binary is not found.
func NewCamoufox(opts ...CamoufoxOption) (*CamoufoxFetcher, error) {
	f := &CamoufoxFetcher{
		headless: "virtual",
	}
	for _, opt := range opts {
		opt(f)
	}

	slog.Info("fetch/camoufox: stub initialised (playwright-go not yet configured)",
		"headless", f.headless,
		"block_images", f.blockImages,
	)

	// TODO(phase2): launch Camoufox here.
	// pw, err := playwright.Run()
	// if err != nil {
	//     return nil, fmt.Errorf("fetch/camoufox: launching playwright: %w", err)
	// }
	// browser, err := pw.Firefox.Launch(playwright.BrowserTypeLaunchOptions{...})
	// if err != nil {
	//     pw.Stop()
	//     return nil, fmt.Errorf("fetch/camoufox: launching camoufox: %w", err)
	// }
	// f.pw = pw
	// f.browser = browser

	return f, nil
}

// Fetch navigates to job.URL using the Camoufox browser and returns the page
// content. The response FetchMode is FetchBrowser.
//
// Phase 1 stub: always returns errPlaywrightNotConfigured.
//
// Phase 2 real implementation will:
//  1. Create a BrowserContext with CAMOU_CONFIG env vars from identity.CamoufoxEnv.
//  2. Set extra HTTP headers (User-Agent, Accept-Language) on the context.
//  3. If blockImages=true, install a route handler to abort image/media requests.
//  4. Open a Page and call page.Goto(job.URL, playwright.PageGotoOptions{
//     WaitUntil: playwright.WaitUntilStateNetworkidle}).
//  5. Extract page.Content() as the response body.
//  6. Collect response status and headers from the network response event.
//  7. Close the context (not the browser) to release resources.
func (f *CamoufoxFetcher) Fetch(_ context.Context, _ *foxhound.Job) (*foxhound.Response, error) {
	// TODO(phase2): implement real browser navigation.
	// ctx, err := f.browser.NewContext(playwright.BrowserNewContextOptions{
	//     UserAgent:   f.identity.UA,
	//     Locale:      f.identity.Locale,
	//     TimezoneId:  f.identity.Timezone,
	//     Geolocation: &playwright.Geolocation{Latitude: f.identity.Lat, Longitude: f.identity.Lng},
	//     ExtraHttpHeaders: map[string]string{
	//         "Accept-Language": buildAcceptLanguage(f.identity.Languages),
	//     },
	// })
	// ...
	return nil, errPlaywrightNotConfigured
}

// Close releases browser resources. In Phase 1 this is a no-op.
//
// Phase 2 will call browser.Close() and pw.Stop().
func (f *CamoufoxFetcher) Close() error {
	// TODO(phase2): release playwright resources.
	// if f.browser != nil { _ = f.browser.Close() }
	// if f.pw != nil { _ = f.pw.Stop() }
	return nil
}
