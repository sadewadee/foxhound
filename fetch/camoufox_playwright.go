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
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	neturl "net/url"
	"regexp"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/behavior"
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
	pw              *playwright.Playwright
	browser         playwright.Browser
	identity        *identity.Profile
	blockImages     bool
	headless        string
	timeout         time.Duration
	proxyURL        string       // SOCKS5 or HTTP proxy URL
	extensionPath   string       // path to Firefox extension dir (e.g. NopeCHA)
	maxRequests     int          // restart after this many fetches (0 = disabled)
	requestCount    atomic.Int64 // total fetches served by the current browser instance
	mu              sync.Mutex   // serialises browser restart
	persistSession  bool
	sessionCtx      playwright.BrowserContext // reused when persistSession=true
	sessionMu       sync.Mutex               // guards sessionCtx lifecycle
	hasExtension    bool                      // true when solver extension is loaded
	initScript      string                    // JS injected into every page via AddInitScript
	userDataDir     string                    // persistent profile dir; uses LaunchPersistentContext
	persistCtx      playwright.BrowserContext // context from LaunchPersistentContext
	cdpURL          string                    // connect to existing browser via CDP
	useRealChrome   bool                      // use pw.Chromium with channel=chrome
	capturePatterns []*regexp.Regexp          // URL patterns for XHR/fetch response capture
}

// WithBrowserProxy sets the proxy URL for all browser requests.
// Supports SOCKS5 (socks5://user:pass@host:port) and HTTP (http://user:pass@host:port).
func WithBrowserProxy(proxyURL string) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.proxyURL = proxyURL
	}
}

// WithExtensionPath sets the path to a Firefox extension directory to load.
// The extension is installed as a temporary addon when the browser launches.
// This is used to load NopeCHA or similar solver extensions that auto-solve
// CAPTCHA challenges the browser encounters.
func WithExtensionPath(path string) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.extensionPath = path
	}
}

// WithInitScript sets a JavaScript snippet that is injected into every new
// page before the page's own scripts execute, via page.AddInitScript. Useful
// for overriding navigator properties (e.g. navigator.webdriver=false) or
// installing global stubs before any site JS runs.
func WithInitScript(script string) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.initScript = script
	}
}

// WithUserDataDir sets a persistent profile directory. When non-empty,
// NewCamoufox uses pw.Firefox.LaunchPersistentContext (or
// pw.Chromium.LaunchPersistentContext when WithRealChrome is true) so cookies,
// localStorage, and cached resources survive across browser restarts.
func WithUserDataDir(dir string) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.userDataDir = dir
	}
}

// WithCDPURL sets a Chrome DevTools Protocol endpoint URL (e.g.
// "http://localhost:9222"). When non-empty, NewCamoufox connects to an
// existing browser process via pw.Chromium.ConnectOverCDP instead of launching
// a new browser. Useful for remote debugging setups or distributed scraping
// where the browser lifecycle is managed externally.
func WithCDPURL(url string) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.cdpURL = url
	}
}

// WithRealChrome switches from Firefox/Camoufox to pw.Chromium.Launch with
// channel="chrome". Use when Chrome rendering is required or when a Chrome
// installation is available but Camoufox is not. Falls back silently to
// Chromium if the Chrome channel binary is not installed.
func WithRealChrome(use bool) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.useRealChrome = use
	}
}

// NewCamoufox initialises playwright, applies the supplied options, launches a
// Firefox (Camoufox) browser, and returns a ready-to-use CamoufoxFetcher.
//
// The browser is kept alive until Close is called. If the playwright runtime or
// the Camoufox binary is not installed, NewCamoufox auto-downloads it via
// `python3 -m camoufox fetch`. No manual setup is required.
func NewCamoufox(opts ...CamoufoxOption) (*CamoufoxFetcher, error) {
	f := &CamoufoxFetcher{
		headless:    "virtual",
		timeout:     defaultBrowserTimeout,
		maxRequests: 300,
	}
	for _, opt := range opts {
		opt(f)
	}

	// Start the playwright runtime.
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("fetch/camoufox: starting playwright runtime: %w", err)
	}

	// Locate or auto-install the Camoufox binary.
	// Camoufox is a C++ patched Firefox with built-in anti-fingerprinting:
	// canvas, WebGL, audio, font, navigator — all spoofed at engine level.
	execPath, err := findOrInstallCamoufox()
	if err != nil {
		slog.Warn("fetch/camoufox: Camoufox binary not available, falling back to plain Firefox",
			"err", err)
		// Fall back to plain Firefox (playwright's bundled version)
		execPath = ""
	}

	headlessBool := f.headless != "false"

	launchOpts := playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(headlessBool),
	}

	// Use Camoufox binary if found — this enables ALL C++ level anti-detection:
	// canvas spoofing, WebGL spoofing, font spoofing, navigator.webdriver=false,
	// WebRTC IP protection, audio context spoofing, BrowserForge fingerprints.
	if execPath != "" {
		launchOpts.ExecutablePath = playwright.String(execPath)
		slog.Info("fetch/camoufox: using Camoufox binary", "path", execPath)
	} else {
		slog.Warn("fetch/camoufox: using plain Firefox — anti-fingerprint features disabled")
	}

	// Set proxy at browser launch level.
	if f.proxyURL != "" {
		proxyOpt := parsePlaywrightProxy(f.proxyURL)
		launchOpts.Proxy = proxyOpt
		slog.Info("fetch/camoufox: proxy configured", "server", proxyOpt.Server)
	}

	// Auto-load NopeCHA extension by default unless explicitly disabled.
	// If extensionPath is empty, auto-download NopeCHA to cache.
	// If extensionPath is "none", skip extension loading entirely.
	if f.extensionPath == "" {
		nopechaPath, nopechaErr := ensureNopeCHA()
		if nopechaErr != nil {
			slog.Warn("fetch/camoufox: NopeCHA auto-download failed, continuing without extension",
				"err", nopechaErr)
		} else {
			f.extensionPath = nopechaPath
			slog.Info("fetch/camoufox: NopeCHA extension auto-loaded", "path", nopechaPath)
		}
	}

	// When an extension path is set AND Camoufox binary is available, inject
	// the addon via CAMOU_CONFIG env var. Camoufox natively loads unpacked
	// addons listed in CAMOU_CONFIG_* environment variables — no persistent
	// context or profile hacking needed.
	if f.extensionPath != "" && f.extensionPath != "none" && execPath != "" {
		// Resolve to absolute path.
		absExt, _ := filepath.Abs(f.extensionPath)
		// If .xpi, it must be extracted first — Camoufox expects directories.
		if strings.HasSuffix(absExt, ".xpi") {
			extractDir, tmpErr := os.MkdirTemp("", "foxhound-addon-*")
			if tmpErr == nil {
				unzipCmd := exec.Command("unzip", "-o", absExt, "-d", extractDir)
				if uzErr := unzipCmd.Run(); uzErr != nil {
					slog.Warn("fetch/camoufox: failed to extract .xpi", "err", uzErr)
				} else {
					absExt = extractDir
					slog.Info("fetch/camoufox: extracted .xpi", "dir", extractDir)
				}
			}
		}

		// Build CAMOU_CONFIG JSON with addons array.
		camoConfig := map[string]any{
			"addons": []string{absExt},
		}
		configJSON, _ := json.Marshal(camoConfig)
		if launchOpts.Env == nil {
			launchOpts.Env = map[string]string{}
		}
		launchOpts.Env["CAMOU_CONFIG_1"] = string(configJSON)
		f.hasExtension = true

		slog.Info("fetch/camoufox: addon injected via CAMOU_CONFIG",
			"path", absExt)
	}

	// --- Browser acquisition: CDP connect > persistent context > normal launch ---

	// Mode 1: connect to an existing browser over CDP (e.g. remote Chrome).
	// cdpURL takes precedence over all other launch options.
	if f.cdpURL != "" {
		browser, cdpErr := pw.Chromium.ConnectOverCDP(f.cdpURL)
		if cdpErr != nil {
			_ = pw.Stop()
			return nil, fmt.Errorf("fetch/camoufox: ConnectOverCDP(%s): %w", f.cdpURL, cdpErr)
		}
		f.pw = pw
		f.browser = browser
		slog.Info("fetch/camoufox: connected to existing browser via CDP", "url", f.cdpURL)
		return f, nil
	}

	// Mode 2: real Chrome channel (pw.Chromium with channel="chrome").
	if f.useRealChrome {
		chromeLaunchOpts := playwright.BrowserTypeLaunchOptions{
			Headless: playwright.Bool(f.headless != "false"),
			Channel:  playwright.String("chrome"),
		}
		if f.proxyURL != "" {
			chromeLaunchOpts.Proxy = parsePlaywrightProxy(f.proxyURL)
		}

		// Persistent context takes priority when userDataDir is also set.
		if f.userDataDir != "" {
			persistOpts := playwright.BrowserTypeLaunchPersistentContextOptions{
				Headless: playwright.Bool(f.headless != "false"),
				Channel:  playwright.String("chrome"),
			}
			if f.proxyURL != "" {
				persistOpts.Proxy = parsePlaywrightProxy(f.proxyURL)
			}
			bctx, persistErr := pw.Chromium.LaunchPersistentContext(f.userDataDir, persistOpts)
			if persistErr != nil {
				// Fall back to regular Chrome launch.
				slog.Warn("fetch/camoufox: LaunchPersistentContext (Chrome) failed, falling back",
					"err", persistErr)
			} else {
				f.pw = pw
				f.persistCtx = bctx
				// Obtain the browser handle from the context for restart logic.
				f.browser = bctx.Browser()
				slog.Info("fetch/camoufox: Chrome with persistent context ready",
					"dir", f.userDataDir)
				return f, nil
			}
		}

		browser, chromeErr := pw.Chromium.Launch(chromeLaunchOpts)
		if chromeErr != nil {
			// Chrome not installed — fall back to plain Chromium.
			slog.Warn("fetch/camoufox: Chrome channel not found, falling back to Chromium", "err", chromeErr)
			chromeLaunchOpts.Channel = nil
			browser, chromeErr = pw.Chromium.Launch(chromeLaunchOpts)
			if chromeErr != nil {
				_ = pw.Stop()
				return nil, fmt.Errorf("fetch/camoufox: Chromium launch failed: %w", chromeErr)
			}
		}
		f.pw = pw
		f.browser = browser
		slog.Info("fetch/camoufox: Chrome/Chromium browser ready",
			"headless", f.headless,
			"timeout", f.timeout,
		)
		return f, nil
	}

	// Mode 3: Firefox/Camoufox with persistent profile directory.
	if f.userDataDir != "" {
		persistOpts := playwright.BrowserTypeLaunchPersistentContextOptions{
			Headless: playwright.Bool(f.headless != "false"),
		}
		if execPath != "" {
			persistOpts.ExecutablePath = playwright.String(execPath)
		}
		if f.proxyURL != "" {
			persistOpts.Proxy = parsePlaywrightProxy(f.proxyURL)
		}
		bctx, persistErr := pw.Firefox.LaunchPersistentContext(f.userDataDir, persistOpts)
		if persistErr != nil {
			slog.Warn("fetch/camoufox: LaunchPersistentContext failed, falling back to regular launch",
				"err", persistErr)
			// Fall through to Mode 4 (normal Firefox launch).
		} else {
			f.pw = pw
			f.persistCtx = bctx
			f.browser = bctx.Browser()
			slog.Info("fetch/camoufox: Firefox/Camoufox with persistent context ready",
				"dir", f.userDataDir)
			return f, nil
		}
	}

	// Mode 4: standard Firefox/Camoufox launch (default path).
	browser, err := pw.Firefox.Launch(launchOpts)
	if err != nil {
		// If Camoufox binary failed, try plain Firefox as last resort.
		if execPath != "" {
			slog.Warn("fetch/camoufox: Camoufox binary launch failed, trying plain Firefox", "err", err)
			launchOpts.ExecutablePath = nil
			browser, err = pw.Firefox.Launch(launchOpts)
		}
		if err != nil {
			// Auto-install playwright Firefox if nothing works.
			slog.Info("fetch/camoufox: installing playwright Firefox...")
			if installErr := playwright.Install(&playwright.RunOptions{
				Browsers: []string{"firefox"},
			}); installErr != nil {
				_ = pw.Stop()
				return nil, fmt.Errorf("fetch/camoufox: all browser launch attempts failed: %w", err)
			}
			browser, err = pw.Firefox.Launch(launchOpts)
			if err != nil {
				_ = pw.Stop()
				return nil, fmt.Errorf("fetch/camoufox: launch failed after install: %w", err)
			}
		}
	}

	f.pw = pw
	f.browser = browser

	// Pre-warm: create and immediately close one context to initialize
	// browser internals. Subsequent context creation is ~50% faster.
	if warmCtx, warmErr := f.browser.NewContext(); warmErr == nil {
		warmCtx.Close()
	}

	browserType := "plain Firefox"
	if execPath != "" {
		browserType = "Camoufox"
	}
	slog.Info("fetch/camoufox: browser ready",
		"browser", browserType,
		"headless", f.headless,
		"block_images", f.blockImages,
		"timeout", f.timeout,
	)
	return f, nil
}

// findOrInstallCamoufox locates the Camoufox binary on the system.
// If not found, it attempts to auto-install via `python3 -m camoufox fetch`.
// Returns the executable path or an error.
func findOrInstallCamoufox() (string, error) {
	// Check known paths per OS.
	path := findCamoufoxBinary()
	if path != "" {
		slog.Debug("fetch/camoufox: binary found", "path", path)
		return path, nil
	}

	// Not found — try auto-install.
	slog.Info("fetch/camoufox: binary not found, auto-downloading...")

	// Try python3 first, then python.
	for _, py := range []string{"python3", "python"} {
		// First ensure the camoufox package is installed.
		installCmd := exec.Command(py, "-m", "pip", "install", "-q", "camoufox")
		installCmd.Stdout = os.Stderr
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			continue
		}

		// Download the browser binary.
		fetchCmd := exec.Command(py, "-m", "camoufox", "fetch")
		fetchCmd.Stdout = os.Stderr
		fetchCmd.Stderr = os.Stderr
		if err := fetchCmd.Run(); err != nil {
			slog.Warn("fetch/camoufox: download failed", "python", py, "err", err)
			continue
		}

		// Re-check after install.
		path = findCamoufoxBinary()
		if path != "" {
			slog.Info("fetch/camoufox: auto-install successful", "path", path)
			return path, nil
		}
	}

	return "", fmt.Errorf("camoufox binary not found and auto-install failed (is Python installed?)")
}

// findCamoufoxBinary checks known locations for the Camoufox executable.
func findCamoufoxBinary() string {
	home, _ := os.UserHomeDir()

	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		// macOS: installed via pip
		cacheDir := filepath.Join(home, "Library", "Caches", "camoufox")
		candidates = []string{
			filepath.Join(cacheDir, "Camoufox.app", "Contents", "MacOS", "camoufox"),
		}
	case "linux":
		candidates = []string{
			filepath.Join(home, ".cache", "camoufox", "camoufox"),
			filepath.Join(home, ".local", "share", "camoufox", "camoufox"),
			"/usr/local/bin/camoufox",
		}
	case "windows":
		appData := os.Getenv("LOCALAPPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Local")
		}
		candidates = []string{
			filepath.Join(appData, "camoufox", "camoufox.exe"),
		}
	}

	// PATH check is LAST — the `camoufox` in PATH is usually the Python CLI
	// wrapper, not the actual browser binary. Prefer the cache directory.
	if path, err := exec.LookPath("camoufox"); err == nil {
		candidates = append(candidates, path)
	}

	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// nopechaCacheDir returns the directory where NopeCHA extension is cached.
func nopechaCacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "foxhound", "extensions", "nopecha")
}

// nopeCHAGitHubReleasesAPI is the GitHub API endpoint for NopeCHA releases.
const nopeCHAGitHubReleasesAPI = "https://api.github.com/repos/NopeCHALLC/nopecha-extension/releases/latest"

// ensureNopeCHA returns the path to a cached NopeCHA extension directory,
// downloading it from GitHub releases if not already present.
func ensureNopeCHA() (string, error) {
	cacheDir := nopechaCacheDir()
	manifestPath := filepath.Join(cacheDir, "manifest.json")

	// Already cached — return immediately.
	if _, err := os.Stat(manifestPath); err == nil {
		slog.Debug("fetch/nopecha: using cached extension", "dir", cacheDir)
		return cacheDir, nil
	}

	slog.Info("fetch/nopecha: downloading NopeCHA extension from GitHub...")

	// Query GitHub API for latest release to find firefox_automation.zip asset.
	downloadURL, err := findNopeCHADownloadURL()
	if err != nil {
		return "", fmt.Errorf("fetch/nopecha: find download URL: %w", err)
	}

	// Download the zip to a temp file.
	tmpFile, err := os.CreateTemp("", "nopecha-*.zip")
	if err != nil {
		return "", fmt.Errorf("fetch/nopecha: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Use curl for the download (follows redirects, handles GitHub).
	dlCmd := exec.Command("curl", "-fsSL", "-o", tmpPath, downloadURL)
	dlCmd.Stderr = os.Stderr
	if err := dlCmd.Run(); err != nil {
		return "", fmt.Errorf("fetch/nopecha: download failed: %w", err)
	}

	// Create cache directory and extract.
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("fetch/nopecha: create cache dir: %w", err)
	}

	unzipCmd := exec.Command("unzip", "-o", tmpPath, "-d", cacheDir)
	unzipCmd.Stderr = os.Stderr
	if err := unzipCmd.Run(); err != nil {
		os.RemoveAll(cacheDir)
		return "", fmt.Errorf("fetch/nopecha: extract failed: %w", err)
	}

	// Verify manifest.json exists after extraction.
	if _, err := os.Stat(manifestPath); err != nil {
		// Some zips have a nested directory — check one level deep.
		entries, _ := os.ReadDir(cacheDir)
		for _, e := range entries {
			if e.IsDir() {
				nested := filepath.Join(cacheDir, e.Name(), "manifest.json")
				if _, nerr := os.Stat(nested); nerr == nil {
					// Move contents up one level.
					nestedDir := filepath.Join(cacheDir, e.Name())
					innerEntries, _ := os.ReadDir(nestedDir)
					for _, ie := range innerEntries {
						os.Rename(
							filepath.Join(nestedDir, ie.Name()),
							filepath.Join(cacheDir, ie.Name()),
						)
					}
					os.Remove(nestedDir)
					break
				}
			}
		}
	}

	// Final check.
	if _, err := os.Stat(manifestPath); err != nil {
		return "", fmt.Errorf("fetch/nopecha: manifest.json not found after extraction")
	}

	slog.Info("fetch/nopecha: extension downloaded and cached", "dir", cacheDir)
	return cacheDir, nil
}

// findNopeCHADownloadURL queries GitHub releases API and returns the
// firefox.zip asset download URL. We use the regular extension build (not
// firefox_automation.zip) because the automation build requires an API key
// for cloud-based solving. The regular build works like a normal browser
// extension — no API key needed.
func findNopeCHADownloadURL() (string, error) {
	// Use curl to fetch the releases API (avoids importing net/http for this one call).
	out, err := exec.Command("curl", "-fsSL", nopeCHAGitHubReleasesAPI).Output()
	if err != nil {
		return "", fmt.Errorf("GitHub API request failed: %w", err)
	}

	var release struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(out, &release); err != nil {
		return "", fmt.Errorf("parse GitHub API response: %w", err)
	}

	// Use firefox.zip (regular extension, no API key needed).
	// Do NOT use firefox_automation.zip — that build requires a paid API key.
	for _, a := range release.Assets {
		if a.Name == "firefox.zip" {
			return a.BrowserDownloadURL, nil
		}
	}

	return "", fmt.Errorf("no firefox extension asset found in latest NopeCHA release")
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
//
// Priority order:
//  1. persistCtx — set when LaunchPersistentContext succeeded (userDataDir).
//     The persistent context IS the only context; never closed by callers.
//  2. persistSession=true — a single BrowserContext shared across requests.
//     Initialised on first call, never closed by callers.
//  3. Default — a fresh isolated BrowserContext per request; caller must close.
//
// Returns (ctx, shouldClose, err).
func (f *CamoufoxFetcher) getOrCreateContext() (playwright.BrowserContext, bool, error) {

	// When a persistent context was obtained from LaunchPersistentContext,
	// use it directly — it already embeds the profile directory state.
	if f.persistCtx != nil {
		return f.persistCtx, false, nil // caller must NOT close
	}

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
	scroller := behavior.NewScroll(behavior.DefaultScrollConfig())
	dist, pause, _ := scroller.ScrollGesture(behavior.ScrollReading)
	if err := page.Mouse().Wheel(0, float64(dist)); err != nil {
		slog.Debug("fetch/camoufox: simulateHuman: scroll down failed", "err", err)
	}
	time.Sleep(pause)

	// 4. Occasionally scroll back up — natural reading / re-reading behaviour.
	if rand.Float64() < 0.3 {
		upDist, upPause, _ := scroller.ScrollGesture(behavior.ScrollReading)
		// Use half the distance for a partial re-read.
		if err := page.Mouse().Wheel(0, -float64(upDist/2)); err != nil {
			slog.Debug("fetch/camoufox: simulateHuman: scroll up failed", "err", err)
		}
		time.Sleep(upPause)
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

// executeStep performs a single Trail step on the loaded page. It handles
// Click, Wait, and Scroll actions. Extract steps are skipped (handled later
// by the Walker's Processor).
// executeStep runs a single JobStep on the page. For Evaluate steps, the
// return value of the JS expression is returned as the first value.
// For all other steps the first return value is nil.
func (f *CamoufoxFetcher) executeStep(page playwright.Page, step foxhound.JobStep) (any, error) {
	switch step.Action {
	case foxhound.JobStepClick:
		el, err := page.QuerySelector(step.Selector)
		if err != nil {
			return nil, fmt.Errorf("query selector %q: %w", step.Selector, err)
		}
		if el == nil {
			return nil, fmt.Errorf("selector %q not found", step.Selector)
		}
		if err := el.Click(); err != nil {
			return nil, fmt.Errorf("click %q: %w", step.Selector, err)
		}
		// Brief pause after click to let any JS handlers fire.
		time.Sleep(time.Duration(200+rand.IntN(300)) * time.Millisecond)

	case foxhound.JobStepWait:
		timeout := step.Duration
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		_, err := page.WaitForSelector(step.Selector, playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(float64(timeout.Milliseconds())),
		})
		if err != nil {
			return nil, fmt.Errorf("wait for %q (timeout %v): %w", step.Selector, timeout, err)
		}

	case foxhound.JobStepScroll:
		scroller := behavior.NewScroll(behavior.DefaultScrollConfig())
		axis := behavior.ScrollAxis(step.ScrollAxis)
		extent := step.ScrollExtent
		if extent <= 0 {
			extent = 3000
		}
		mode := behavior.ScrollMode(step.ScrollMode)
		actions := scroller.ScrollSequenceAxis(extent, mode, axis)
		for _, a := range actions {
			var dx, dy float64
			if a.Axis == behavior.ScrollHorizontal {
				dx = float64(a.Distance)
				if a.Up {
					dx = -dx
				}
			} else {
				dy = float64(a.Distance)
				if a.Up {
					dy = -dy
				}
			}
			if err := page.Mouse().Wheel(dx, dy); err != nil {
				slog.Debug("fetch/camoufox: scroll step failed", "err", err)
			}
			time.Sleep(a.Pause)
		}

	case foxhound.JobStepInfiniteScroll:
		maxScrolls := step.MaxScrolls
		if maxScrolls <= 0 {
			maxScrolls = 50
		}

		// Custom scroll container: if Selector is set, scroll inside that
		// element (e.g. Google Maps panel) instead of document.body.
		container := step.Selector // "" = whole page (default)

		slog.Info("fetch/camoufox: infinite scroll started",
			"max", maxScrolls, "container", container,
			"stop_selector", step.StopSelector, "stop_count", step.StopCount)

		scroller := behavior.NewScroll(behavior.DefaultScrollConfig())

		// Build JS snippets for scroll height depending on container.
		getHeightJS := "() => document.body.scrollHeight"
		scrollToBottomJS := "() => window.scrollTo(0, document.body.scrollHeight)"
		if container != "" {
			getHeightJS = fmt.Sprintf("() => { const el = document.querySelector(%q); return el ? el.scrollHeight : 0 }", container)
			scrollToBottomJS = fmt.Sprintf("() => { const el = document.querySelector(%q); if (el) el.scrollTop = el.scrollHeight }", container)
		}

		stopCount := step.StopCount
		if stopCount <= 0 {
			stopCount = 1
		}

		scrollWait := step.ScrollWait
		if scrollWait <= 0 {
			scrollWait = 2 * time.Second
		}

		// Track element count across iterations for StopSelector progress.
		prevCount := 0
		if step.StopSelector != "" {
			initResult, _ := page.Evaluate(fmt.Sprintf(
				"() => document.querySelectorAll(%q).length", step.StopSelector))
			if cnt, ok := initResult.(float64); ok {
				prevCount = int(cnt)
			}
		}

		for i := 0; i < maxScrolls; i++ {
			// Check StopSelector target count before scrolling.
			if step.StopSelector != "" {
				countResult, _ := page.Evaluate(fmt.Sprintf(
					"() => document.querySelectorAll(%q).length", step.StopSelector))
				if cnt, ok := countResult.(float64); ok && int(cnt) >= stopCount {
					slog.Info("fetch/camoufox: infinite scroll stopped — target reached",
						"selector", step.StopSelector, "count", int(cnt), "target", stopCount)
					break
				}
			}

			// Get current scroll height.
			prevHeight, _ := page.Evaluate(getHeightJS)
			prevH, _ := prevHeight.(float64)

			// Scroll with human-like gesture.
			dist, pause, _ := scroller.ScrollGesture(behavior.ScrollScan)
			if container != "" {
				// Scroll inside the container element.
				page.Evaluate(fmt.Sprintf(
					"(d) => { const el = document.querySelector(%q); if (el) el.scrollTop += d }", container),
					dist)
			} else {
				_ = page.Mouse().Wheel(0, float64(dist))
			}
			time.Sleep(pause)

			// Also jump to bottom for pages that need it.
			page.Evaluate(scrollToBottomJS)
			// Wait for new content to load.
			time.Sleep(scrollWait + time.Duration(rand.IntN(500))*time.Millisecond)

			// When StopSelector is set, use element count as progress indicator
			// instead of scrollHeight — more reliable for lazy-loaded content.
			if step.StopSelector != "" {
				countResult, _ := page.Evaluate(fmt.Sprintf(
					"() => document.querySelectorAll(%q).length", step.StopSelector))
				if cnt, ok := countResult.(float64); ok && int(cnt) >= stopCount {
					slog.Info("fetch/camoufox: infinite scroll stopped — target reached",
						"selector", step.StopSelector, "count", int(cnt), "target", stopCount)
					break
				}
				// If count changed from last iteration, content is still loading.
				newCount, _ := countResult.(float64)
				if int(newCount) > prevCount {
					prevCount = int(newCount)
					continue // skip scrollHeight check — content IS loading
				}
			}

			// Check if new content appeared.
			newHeight, _ := page.Evaluate(getHeightJS)
			newH, _ := newHeight.(float64)

			if newH <= prevH {
				slog.Info("fetch/camoufox: infinite scroll complete — no new content",
					"iterations", i+1)
				break
			}
		}

	case foxhound.JobStepLoadMore:
		maxClicks := step.MaxClicks
		if maxClicks <= 0 {
			maxClicks = 20
		}
		sel := step.Selector
		if sel == "" {
			// Common "load more" / "show more" selectors.
			sel = "button:has-text('Load more'), button:has-text('Show more'), " +
				"button:has-text('Muat lagi'), a:has-text('Load more'), " +
				"[class*='load-more'], [class*='show-more'], [class*='loadMore']"
		}
		slog.Info("fetch/camoufox: load more started", "selector", sel, "max", maxClicks)

		for i := 0; i < maxClicks; i++ {
			el, err := page.QuerySelector(sel)
			if err != nil || el == nil {
				slog.Info("fetch/camoufox: load more button gone", "clicks", i)
				break
			}
			visible, _ := el.IsVisible()
			if !visible {
				slog.Info("fetch/camoufox: load more button hidden", "clicks", i)
				break
			}
			// Human-like pause before click.
			time.Sleep(time.Duration(500+rand.IntN(1000)) * time.Millisecond)
			if err := el.Click(); err != nil {
				slog.Debug("fetch/camoufox: load more click failed", "err", err)
				break
			}
			// Wait for new content to load.
			time.Sleep(time.Duration(1500+rand.IntN(2000)) * time.Millisecond)
		}

	case foxhound.JobStepPaginate:
		maxPages := step.MaxPages
		if maxPages <= 0 {
			maxPages = 10
		}
		sel := step.Selector
		if sel == "" {
			// Common pagination "next" selectors.
			sel = "a[rel='next'], a:has-text('Next'), a:has-text('Selanjutnya'), " +
				"li.next a, a.next, [aria-label='Next'], [aria-label='Next page'], " +
				"a:has-text('›'), a:has-text('»')"
		}
		slog.Info("fetch/camoufox: pagination started", "selector", sel, "max", maxPages)

		// Capture initial page content before navigating away.
		var pages []string
		if content, contentErr := page.Content(); contentErr == nil {
			pages = append(pages, content)
		}

		for i := 0; i < maxPages; i++ {
			el, err := page.QuerySelector(sel)
			if err != nil || el == nil {
				slog.Info("fetch/camoufox: no more pagination links", "pages", i)
				break
			}
			visible, _ := el.IsVisible()
			if !visible {
				slog.Info("fetch/camoufox: pagination link hidden", "pages", i)
				break
			}
			// Human-like pause before navigating.
			time.Sleep(time.Duration(1000+rand.IntN(2000)) * time.Millisecond)
			if err := el.Click(); err != nil {
				slog.Debug("fetch/camoufox: pagination click failed", "err", err)
				break
			}
			// Wait for next page to load.
			page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
				State: playwright.LoadStateNetworkidle,
			})
			time.Sleep(time.Duration(1000+rand.IntN(1500)) * time.Millisecond)

			// Capture this page's content.
			if content, contentErr := page.Content(); contentErr == nil {
				pages = append(pages, content)
			}
		}

		// Return accumulated pages — caller stores in Response.StepResults.
		if len(pages) > 0 {
			return pages, nil
		}

	case foxhound.JobStepFill:
		if step.Selector == "" {
			return nil, fmt.Errorf("fill step has empty selector")
		}
		el, err := page.QuerySelector(step.Selector)
		if err != nil {
			return nil, fmt.Errorf("fill: query %q: %w", step.Selector, err)
		}
		if el == nil {
			return nil, fmt.Errorf("fill: selector %q not found", step.Selector)
		}
		// Clear existing value.
		if err := el.Fill(""); err != nil {
			slog.Debug("fetch/camoufox: fill clear failed", "err", err)
		}
		// Type with human-like keystrokes using behavior.Keyboard.
		kb := behavior.NewKeyboard(behavior.DefaultKeyboardConfig())
		actions := kb.TypeString(step.Value)
		for _, a := range actions {
			if a.IsBackspace {
				if err := el.Press("Backspace"); err != nil {
					slog.Debug("fetch/camoufox: fill backspace failed", "err", err)
				}
			} else {
				if err := el.Type(string(a.Char)); err != nil {
					slog.Debug("fetch/camoufox: fill type failed", "err", err)
				}
			}
			time.Sleep(a.Delay)
		}
		time.Sleep(time.Duration(200+rand.IntN(300)) * time.Millisecond)

	case foxhound.JobStepEvaluate:
		if step.Script == "" {
			return nil, fmt.Errorf("evaluate step has empty script")
		}
		result, err := page.Evaluate(step.Script)
		if err != nil {
			return nil, fmt.Errorf("evaluate script: %w", err)
		}
		return result, nil

	case foxhound.JobStepExtract:
		// Handled by Walker after fetch — no-op here.

	default:
		slog.Debug("fetch/camoufox: unknown step action", "action", step.Action)
	}
	return nil, nil
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

			slog.Info("fetch/camoufox: reCAPTCHA checkbox clicked but not solved, waiting for solver extension...")
			// Wait for solver extension (NopeCHA) to handle image challenge.
			for wait := 0; wait < 30; wait++ {
				time.Sleep(1 * time.Second)
				checked2, _ := cb.GetAttribute("aria-checked")
				if checked2 == "true" {
					slog.Info("fetch/camoufox: reCAPTCHA SOLVED by extension!")
					f.submitAfterCaptcha(page)
					time.Sleep(2 * time.Second)
					page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
						State: playwright.LoadStateNetworkidle,
					})
					return
				}
			}
			slog.Warn("fetch/camoufox: reCAPTCHA image challenge not solved in time")
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

// handleHCaptcha detects and attempts to solve hCaptcha checkbox challenges.
// hCaptcha renders inside an iframe similar to reCAPTCHA — we locate the
// iframe, find the checkbox, move the mouse naturally, and click.
func (f *CamoufoxFetcher) handleHCaptcha(page playwright.Page) {
	// Check if page contains hCaptcha indicators.
	hasHCaptcha := false
	indicators := []string{
		"iframe[src*='hcaptcha.com']",
		"iframe[data-hcaptcha-widget-id]",
		".h-captcha",
		"div[class*='h-captcha']",
	}
	for _, sel := range indicators {
		el, err := page.QuerySelector(sel)
		if err == nil && el != nil {
			hasHCaptcha = true
			break
		}
	}
	if !hasHCaptcha {
		return
	}

	slog.Info("fetch/camoufox: hCaptcha detected, attempting to solve...")
	time.Sleep(time.Duration(1500+rand.IntN(2000)) * time.Millisecond)

	// hCaptcha checkbox lives inside an iframe.
	iframeSelectors := []string{
		"iframe[src*='hcaptcha.com/captcha/checkbox']",
		"iframe[src*='assets.hcaptcha.com']",
		"iframe[src*='hcaptcha.com']",
		"iframe[data-hcaptcha-widget-id]",
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

		checkboxSelectors := []string{
			"#checkbox",
			"div[id='checkbox']",
			"div[class*='check']",
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

			// Human-like mouse movement toward the checkbox.
			box, err := cb.BoundingBox()
			if err == nil && box != nil {
				targetX := box.X + box.Width*0.3 + float64(rand.IntN(int(box.Width*0.4)))
				targetY := box.Y + box.Height*0.3 + float64(rand.IntN(int(box.Height*0.4)))
				currentX := float64(200 + rand.IntN(400))
				currentY := float64(200 + rand.IntN(200))
				steps := 8 + rand.IntN(8)
				for step := 1; step <= steps; step++ {
					t := float64(step) / float64(steps)
					t = t * t * (3 - 2*t)
					mx := currentX + (targetX-currentX)*t + float64(rand.IntN(3)-1)
					my := currentY + (targetY-currentY)*t + float64(rand.IntN(3)-1)
					page.Mouse().Move(mx, my)
					time.Sleep(time.Duration(20+rand.IntN(40)) * time.Millisecond)
				}
				time.Sleep(time.Duration(200+rand.IntN(300)) * time.Millisecond)
			}

			if err := cb.Click(); err != nil {
				slog.Debug("fetch/camoufox: hCaptcha checkbox click failed", "err", err)
				continue
			}

			slog.Info("fetch/camoufox: hCaptcha checkbox clicked, waiting for verification...")
			time.Sleep(time.Duration(3000+rand.IntN(3000)) * time.Millisecond)

			// Check if solved.
			checked, _ := cb.GetAttribute("aria-checked")
			if checked == "true" {
				slog.Info("fetch/camoufox: hCaptcha SOLVED via checkbox click!")
				f.submitAfterCaptcha(page)
				time.Sleep(time.Duration(2000+rand.IntN(2000)) * time.Millisecond)
				page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
					State: playwright.LoadStateNetworkidle,
				})
				return
			}

			slog.Info("fetch/camoufox: hCaptcha checkbox clicked but not solved, waiting for solver extension...")
			for wait := 0; wait < 30; wait++ {
				time.Sleep(1 * time.Second)
				checked2, _ := cb.GetAttribute("aria-checked")
				if checked2 == "true" {
					slog.Info("fetch/camoufox: hCaptcha SOLVED by extension!")
					f.submitAfterCaptcha(page)
					time.Sleep(2 * time.Second)
					page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
						State: playwright.LoadStateNetworkidle,
					})
					return
				}
			}
			slog.Warn("fetch/camoufox: hCaptcha image challenge not solved in time")
			return
		}
	}

	slog.Warn("fetch/camoufox: hCaptcha found but could not locate clickable checkbox")
}

// waitForExtensionSolve detects captcha type on the page and waits for the
// solver extension (NopeCHA) to solve it. Used when extension is loaded —
// no manual clicking needed, the extension handles everything.
func (f *CamoufoxFetcher) waitForExtensionSolve(page playwright.Page) {
	// Give page JS + extension time to init and start solving.
	time.Sleep(5 * time.Second)

	content, _ := page.Content()
	lower := strings.ToLower(content)

	// Detect what captcha type is on the page.
	var captchaType string
	switch {
	case strings.Contains(lower, "challenges.cloudflare.com/turnstile") || strings.Contains(lower, "cf-turnstile"):
		captchaType = "turnstile"
	case strings.Contains(lower, "hcaptcha.com") || strings.Contains(lower, "h-captcha"):
		captchaType = "hcaptcha"
	case strings.Contains(lower, "google.com/recaptcha") || strings.Contains(lower, "g-recaptcha"):
		captchaType = "recaptcha"
	case strings.Contains(lower, "geetest.com") || strings.Contains(lower, "gt_captcha"):
		captchaType = "geetest"
	}

	if captchaType == "" {
		return
	}

	slog.Info("fetch/camoufox: captcha detected, waiting for extension to solve",
		"type", captchaType)

	// Poll for solve completion — check every second for up to 45s.
	for i := 0; i < 45; i++ {
		time.Sleep(1 * time.Second)

		solved := false
		switch captchaType {
		case "turnstile":
			// Turnstile solved when hidden input has a token value.
			val, _ := page.Evaluate(`() => {
				const inp = document.querySelector('input[name="cf-turnstile-response"]');
				return inp && inp.value && inp.value.length > 10;
			}`)
			if b, ok := val.(bool); ok && b {
				solved = true
			}
		case "recaptcha":
			// reCAPTCHA solved when textarea#g-recaptcha-response has value.
			val, _ := page.Evaluate(`() => {
				const ta = document.querySelector('textarea[name="g-recaptcha-response"]');
				return ta && ta.value && ta.value.length > 10;
			}`)
			if b, ok := val.(bool); ok && b {
				solved = true
			}
		case "hcaptcha":
			// hCaptcha solved when textarea[name="h-captcha-response"] has value.
			val, _ := page.Evaluate(`() => {
				const ta = document.querySelector('textarea[name="h-captcha-response"]');
				return ta && ta.value && ta.value.length > 10;
			}`)
			if b, ok := val.(bool); ok && b {
				solved = true
			}
		case "geetest":
			// GeeTest solved when validate field is populated.
			val, _ := page.Evaluate(`() => {
				const inp = document.querySelector('input[name="geetest_validate"], .geetest_success');
				return inp != null;
			}`)
			if b, ok := val.(bool); ok && b {
				solved = true
			}
		}

		if solved {
			slog.Info("fetch/camoufox: extension solved captcha!",
				"type", captchaType, "seconds", i+1)
			f.submitAfterCaptcha(page)
			time.Sleep(1 * time.Second)
			return
		}
	}
	slog.Warn("fetch/camoufox: extension did not solve captcha in time",
		"type", captchaType)
}

// detectCloudflare inspects the fully-loaded page content and returns the type
// of Cloudflare challenge present, or "" if none is detected.
//
// Recognised challenge types:
//   - "js_challenge"  — "Checking your browser" / "Just a moment" interstitial
//   - "turnstile"     — Cloudflare Turnstile widget (visible or managed/invisible)
//   - "under_attack"  — Under Attack Mode (extended JS challenge)
func (f *CamoufoxFetcher) detectCloudflare(page playwright.Page) string {
	content, _ := page.Content()
	lower := strings.ToLower(content)

	// JS Challenge — "Checking your browser" / "Just a moment" interstitial
	if strings.Contains(lower, "checking your browser") ||
		strings.Contains(lower, "just a moment") ||
		strings.Contains(lower, "cf-browser-verification") ||
		strings.Contains(lower, "challenge-platform") {
		return "js_challenge"
	}

	// Turnstile widget — but only if it hasn't been solved yet.
	// When solved, the hidden input cf-turnstile-response gets a token value.
	if strings.Contains(lower, "challenges.cloudflare.com/turnstile") ||
		strings.Contains(lower, "cf-turnstile") {
		// Check if any cf-turnstile-response input already has a token value.
		solved, _ := page.Evaluate(`() => {
			const inputs = document.querySelectorAll('input[name="cf-turnstile-response"]');
			for (const input of inputs) {
				if (input.value && input.value.length > 10) return true;
			}
			return false;
		}`)
		if val, ok := solved.(bool); ok && val {
			return "" // already solved
		}
		return "turnstile"
	}

	// Under Attack Mode
	if strings.Contains(lower, "cf-chl-bypass") ||
		(strings.Contains(lower, "ray id") && strings.Contains(lower, "cloudflare")) {
		return "under_attack"
	}

	return ""
}

// handleCloudflare attempts to resolve a Cloudflare challenge on the given page.
// It returns true when the challenge was resolved and the page content has
// changed, and false when no challenge was detected or the challenge timed out.
//
// Three challenge types are handled:
//   - js_challenge: wait up to 12 s for the automatic JS solve + redirect.
//   - turnstile: locate the Turnstile iframe and click the checkbox; if not
//     found, wait 5 s for managed auto-resolution.
//   - under_attack: wait up to 15 s for the extended JS challenge to complete.
func (f *CamoufoxFetcher) handleCloudflare(page playwright.Page) bool {
	cfType := f.detectCloudflare(page)
	if cfType == "" {
		return false
	}

	slog.Info("fetch/camoufox: Cloudflare challenge detected", "type", cfType)

	switch cfType {
	case "js_challenge":
		// Cloudflare JS challenge auto-resolves after ~5 s when the browser
		// passes Cloudflare's JS fingerprinting checks.  Poll until the
		// challenge page disappears or 12 s elapses.
		slog.Info("fetch/camoufox: waiting for Cloudflare JS challenge to resolve...")
		for i := 0; i < 12; i++ {
			time.Sleep(1 * time.Second)
			if f.detectCloudflare(page) == "" {
				slog.Info("fetch/camoufox: Cloudflare JS challenge resolved!")
				page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
					State: playwright.LoadStateNetworkidle,
				})
				return true
			}
		}
		slog.Warn("fetch/camoufox: Cloudflare JS challenge did not resolve in time")

	case "turnstile":
		slog.Info("fetch/camoufox: attempting Turnstile challenge...")

		// Turnstile can render as:
		//  (a) an iframe with src*='challenges.cloudflare.com' (Cloudflare-hosted interstitial)
		//  (b) a div container with JS-injected widget (embedded via api.js — no iframe)
		// We try both strategies: iframe first, then container div.

		// Strategy 1: Turnstile iframe (Cloudflare interstitial pages).
		iframeSelectors := []string{
			"iframe[src*='challenges.cloudflare.com/turnstile']",
			"iframe[src*='challenges.cloudflare.com/cdn-cgi']",
			"iframe[src*='challenges.cloudflare.com']",
			"div.cf-turnstile iframe",
		}

		var widget playwright.ElementHandle
		for _, sel := range iframeSelectors {
			el, err := page.QuerySelector(sel)
			if err == nil && el != nil {
				if box, _ := el.BoundingBox(); box != nil && box.Width >= 30 && box.Height >= 30 {
					widget = el
					slog.Info("fetch/camoufox: Turnstile iframe found", "selector", sel)
					break
				}
			}
		}

		// Strategy 2: find widget by hidden input name="cf-turnstile-response".
		// The hidden input is always present — its parent div IS the widget container.
		if widget == nil {
			inputs, err := page.QuerySelectorAll("input[name='cf-turnstile-response']")
			if err == nil {
				for _, input := range inputs {
					// The widget container is the closest ancestor with visible dimensions.
					parent, _ := page.EvaluateHandle(
						"el => { let p = el.parentElement; while (p && (p.offsetWidth < 200 || p.offsetHeight < 30)) p = p.parentElement; return p; }",
						input)
					if parent == nil {
						continue
					}
					el, ok := parent.(playwright.ElementHandle)
					if !ok {
						continue
					}
					box, _ := el.BoundingBox()
					// Turnstile widget is ~300x65. Skip anything too wide (parent wrapper).
					if box != nil && box.Width >= 200 && box.Width <= 500 && box.Height >= 30 {
						widget = el
						slog.Info("fetch/camoufox: Turnstile widget found via hidden input",
							"w", box.Width, "h", box.Height)
						break
					}
				}
			}
		}

		// Strategy 3: Turnstile container div by class (fallback).
		if widget == nil {
			containerSelectors := []string{
				"div.cf-turnstile",
				"div[class*='cf-turnstile']",
			}
			for _, sel := range containerSelectors {
				els, err := page.QuerySelectorAll(sel)
				if err != nil || len(els) == 0 {
					continue
				}
				for _, el := range els {
					box, _ := el.BoundingBox()
					if box != nil && box.Width >= 200 && box.Width <= 500 && box.Height >= 30 {
						widget = el
						slog.Info("fetch/camoufox: Turnstile container found",
							"selector", sel, "w", box.Width, "h", box.Height)
						break
					}
				}
				if widget != nil {
					break
				}
			}
		}

		clicked := false
		if widget != nil {
			// Wait for widget to fully render.
			time.Sleep(time.Duration(1500+rand.IntN(1500)) * time.Millisecond)

			box, err := widget.BoundingBox()
			if err == nil && box != nil {
				slog.Info("fetch/camoufox: clicking Turnstile checkbox",
					"x", box.X, "y", box.Y, "w", box.Width, "h", box.Height)

				// Checkbox is in the left-center area of the widget (~28px from left).
				targetX := box.X + 26 + float64(rand.IntN(10))
				targetY := box.Y + box.Height/2 + float64(rand.IntN(6)-3)

				// Human-like mouse movement.
				startX := float64(200 + rand.IntN(300))
				startY := float64(150 + rand.IntN(200))
				steps := 6 + rand.IntN(6)
				for step := 1; step <= steps; step++ {
					t := float64(step) / float64(steps)
					t = t * t * (3 - 2*t) // ease-in-out
					mx := startX + (targetX-startX)*t + float64(rand.IntN(3)-1)
					my := startY + (targetY-startY)*t + float64(rand.IntN(3)-1)
					_ = page.Mouse().Move(mx, my)
					time.Sleep(time.Duration(15+rand.IntN(30)) * time.Millisecond)
				}

				time.Sleep(time.Duration(200+rand.IntN(400)) * time.Millisecond)
				_ = page.Mouse().Click(targetX, targetY)
				clicked = true
				slog.Info("fetch/camoufox: clicked Turnstile", "x", targetX, "y", targetY)
			}
		} else {
			slog.Warn("fetch/camoufox: no Turnstile widget found in DOM")
		}

		// Poll for resolution.
		waitSecs := 20
		if !clicked {
			slog.Info("fetch/camoufox: waiting for Turnstile auto-resolution...")
			waitSecs = 10
		}
		for i := 0; i < waitSecs; i++ {
			time.Sleep(1 * time.Second)
			if f.detectCloudflare(page) == "" {
				slog.Info("fetch/camoufox: Turnstile challenge resolved!")
				page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
					State: playwright.LoadStateNetworkidle,
				})
				return true
			}
			// Retry click every 5s if first click didn't resolve.
			if clicked && i > 0 && i%5 == 0 && widget != nil {
				if box, err := widget.BoundingBox(); err == nil && box != nil {
					tx := box.X + 26 + float64(rand.IntN(10))
					ty := box.Y + box.Height/2 + float64(rand.IntN(6)-3)
					_ = page.Mouse().Click(tx, ty)
					slog.Info("fetch/camoufox: retried Turnstile click")
				}
			}
		}
		slog.Warn("fetch/camoufox: Turnstile challenge did not resolve in time")

	case "under_attack":
		// Extended JS challenge — may require up to 10–15 s.
		slog.Info("fetch/camoufox: Cloudflare Under Attack mode, waiting...")
		for i := 0; i < 15; i++ {
			time.Sleep(1 * time.Second)
			if f.detectCloudflare(page) == "" {
				slog.Info("fetch/camoufox: Under Attack mode resolved!")
				page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
					State: playwright.LoadStateNetworkidle,
				})
				return true
			}
		}
		slog.Warn("fetch/camoufox: Cloudflare Under Attack mode did not resolve in time")
	}

	return false
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

// hasWaitStep returns true if the job has at least one Wait step. When a job
// includes explicit Wait steps the caller already handles element readiness, so
// the initial page.Goto can use the faster DOMContentLoaded event instead of
// waiting for full network idle — saving 1-3 seconds per navigation.
func hasWaitStep(job *foxhound.Job) bool {
	for _, s := range job.Steps {
		if s.Action == foxhound.JobStepWait {
			return true
		}
	}
	return false
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

	// Inject init script before any page JS executes. This runs on every
	// page navigation, making it the right place for navigator overrides or
	// global stubs that must survive across client-side route changes.
	if f.initScript != "" {
		if scriptErr := page.AddInitScript(playwright.Script{
			Content: playwright.String(f.initScript),
		}); scriptErr != nil {
			slog.Warn("fetch/camoufox: AddInitScript failed (non-fatal)",
				"url", job.URL,
				"err", scriptErr,
			)
		}
	}

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

	// Set up XHR/fetch response capture if patterns are configured.
	var capturedXHR []map[string]any
	if len(f.capturePatterns) > 0 {
		page.On("response", func(response playwright.Response) {
			url := response.URL()
			for _, p := range f.capturePatterns {
				if p.MatchString(url) {
					body, _ := response.Body()
					headers, _ := response.AllHeaders()
					capturedXHR = append(capturedXHR, map[string]any{
						"request_url":    url,
						"request_method": response.Request().Method(),
						"status":         response.Status(),
						"headers":        headers,
						"body":           string(body),
					})
					break
				}
			}
		})
	}

	timeoutMs := float64(f.timeout.Milliseconds())

	slog.Debug("fetch/camoufox: navigating",
		"url", job.URL,
		"job_id", job.ID,
		"timeout_ms", timeoutMs,
	)

	// Use "load" instead of "networkidle" when an extension is loaded —
	// solver extensions (NopeCHA) make ongoing API requests that prevent
	// the network from ever becoming idle.
	waitUntil := playwright.WaitUntilStateNetworkidle
	if f.hasExtension {
		waitUntil = playwright.WaitUntilStateLoad
	}
	// When job has explicit Wait steps, use faster domcontentloaded —
	// the Wait step handles element readiness, so networkidle is redundant.
	if hasWaitStep(job) {
		waitUntil = playwright.WaitUntilStateDomcontentloaded
	}

	start := time.Now()
	navResp, err := page.Goto(job.URL, playwright.PageGotoOptions{
		WaitUntil: waitUntil,
		Timeout:   playwright.Float(timeoutMs),
	})
	duration := time.Since(start)

	if err != nil {
		return nil, fmt.Errorf("fetch/camoufox: navigating to %s: %w", job.URL, err)
	}

	// Handle Cloudflare challenges (JS check, Turnstile, Under Attack Mode).
	if f.handleCloudflare(page) {
		slog.Debug("fetch/camoufox: page updated after Cloudflare challenge")
	}

	// Human behaviour simulation — makes page interaction look natural to
	// anti-bot behavioural analysis (Layer 4 defence).
	f.simulateHumanBehavior(page)

	// Dismiss cookie consent banners if present.
	f.handleCookieConsent(page)

	if f.hasExtension {
		// Extension (NopeCHA) handles clicking + image solving autonomously.
		// Just wait for it to finish.
		f.waitForExtensionSolve(page)
	} else {
		// Manual checkbox click when no extension is loaded.
		f.handleRecaptcha(page)
		f.handleHCaptcha(page)
	}

	// Execute Trail-attached steps. Click and Wait are "hard" steps — their
	// failure means the page content will be wrong, so we abort the fetch.
	// Scroll and Evaluate are best-effort (warn and continue).
	var stepResults map[string]any
	for i, step := range job.Steps {
		stepResult, stepErr := f.executeStep(page, step)
		if stepErr != nil {
			// Hard steps: Click, Wait — abort on failure.
			if !step.Optional && (step.Action == foxhound.JobStepClick || step.Action == foxhound.JobStepWait) {
				return nil, fmt.Errorf("fetch/camoufox: step %d (%d) failed for %s: %w",
					i, step.Action, job.URL, stepErr)
			}
			slog.Warn("fetch/camoufox: step execution failed (non-fatal)",
				"step_index", i,
				"action", step.Action,
				"selector", step.Selector,
				"err", stepErr,
			)
		}
		// Capture Evaluate step return values into Response.StepResults.
		if stepResult != nil {
			if stepResults == nil {
				stepResults = make(map[string]any)
			}
			stepResults[fmt.Sprintf("step_%d", i)] = stepResult
		}
	}

	// Extract the fully-rendered HTML. page.Content() returns the live DOM
	// after all JS has executed, which is the primary reason to use a browser.
	content, err := page.Content()
	if err != nil {
		return nil, fmt.Errorf("fetch/camoufox: extracting content from %s: %w", job.URL, err)
	}

	// If paginate steps accumulated pages, join them all for Response.Body so
	// processors can extract data from every page at once. The delimiter
	// <!-- foxhound:page-break --> allows processors to split pages if needed.
	for _, v := range stepResults {
		if pages, ok := v.([]string); ok && len(pages) > 1 {
			content = strings.Join(pages, "\n<!-- foxhound:page-break -->\n")
			break
		}
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
		StatusCode:  statusCode,
		Headers:     make(map[string][]string),
		Body:        []byte(content),
		URL:         finalURL,
		FetchMode:   foxhound.FetchBrowser,
		Duration:    duration,
		Job:         job,
		StepResults: stepResults,
		CapturedXHR: capturedXHR,
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

	// Close the persistent session context first — it references the old
	// browser and will be invalid after the browser is torn down.
	f.sessionMu.Lock()
	if f.sessionCtx != nil {
		if closeErr := f.sessionCtx.Close(); closeErr != nil {
			if !strings.Contains(closeErr.Error(), "Target closed") &&
				!strings.Contains(closeErr.Error(), "Browser has been closed") {
				slog.Warn("fetch/camoufox: error closing session context during restart", "err", closeErr)
			}
		}
		f.sessionCtx = nil
	}
	f.sessionMu.Unlock()

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

	// Launch a fresh browser with the same configuration — replicate the
	// full launch options from NewCamoufox so Camoufox binary, proxy, and
	// extension settings are preserved across restarts.
	headlessBool := f.headless != "false"
	launchOpts := playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(headlessBool),
	}

	// Restore Camoufox binary path.
	execPath, _ := findOrInstallCamoufox()
	if execPath != "" {
		launchOpts.ExecutablePath = playwright.String(execPath)
	}

	// Restore proxy settings.
	if f.proxyURL != "" {
		launchOpts.Proxy = parsePlaywrightProxy(f.proxyURL)
	}

	// Restore extension (CAMOU_CONFIG) if originally loaded.
	if f.hasExtension && f.extensionPath != "" && execPath != "" {
		absExt, _ := filepath.Abs(f.extensionPath)
		camoConfig := map[string]any{"addons": []string{absExt}}
		configJSON, _ := json.Marshal(camoConfig)
		if launchOpts.Env == nil {
			launchOpts.Env = map[string]string{}
		}
		launchOpts.Env["CAMOU_CONFIG_1"] = string(configJSON)
	}

	browser, err := f.pw.Firefox.Launch(launchOpts)
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
// LaunchPersistentContext context (if any), browser, then the playwright
// process. Errors from each step are logged but do not prevent the subsequent
// steps from running.
func (f *CamoufoxFetcher) Close() error {
	var firstErr error

	// Close the persistent session context so its resources are freed
	// before the browser itself is torn down.
	f.sessionMu.Lock()
	if f.sessionCtx != nil {
		if closeErr := f.sessionCtx.Close(); closeErr != nil {
			if !strings.Contains(closeErr.Error(), "Target closed") &&
				!strings.Contains(closeErr.Error(), "Browser has been closed") {
				slog.Warn("fetch/camoufox: error closing persistent session context", "err", closeErr)
				if firstErr == nil {
					firstErr = fmt.Errorf("fetch/camoufox: closing session context: %w", closeErr)
				}
			}
		}
		f.sessionCtx = nil
	}
	f.sessionMu.Unlock()

	// Close the LaunchPersistentContext context (userDataDir mode).
	// This also implicitly closes the underlying browser process.
	if f.persistCtx != nil {
		if closeErr := f.persistCtx.Close(); closeErr != nil {
			if !strings.Contains(closeErr.Error(), "Target closed") &&
				!strings.Contains(closeErr.Error(), "Browser has been closed") {
				slog.Warn("fetch/camoufox: error closing persistent profile context", "err", closeErr)
				if firstErr == nil {
					firstErr = fmt.Errorf("fetch/camoufox: closing persistent profile context: %w", closeErr)
				}
			}
		}
		f.persistCtx = nil
		f.browser = nil // browser was bound to the persistent context
	}

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
