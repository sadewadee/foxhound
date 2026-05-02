# Dev Squad Gotchas

## 2026-04-07 coordinator -- Firefox version mismatch: profiles said 148.0 but Camoufox is 135.0
- Root cause: All firefox_*.json profile files had `browser_ver: "148.0"` but installed Camoufox binary is based on Firefox 135.0.1 (verified via application.ini). This caused UA mismatch — browser reported Firefox 135 at C++ level but CAMOU_CONFIG injected Firefox 148 UA, creating a detectable inconsistency.
- Fix: Updated all firefox_*.json profiles, fallback profile, and stealth fetcher defaults from 148.0 to 135.0. Created firefox_135.0.json TLS profile. Updated all tests.
- Prevention: Always check `application.ini` in Camoufox binary to verify Firefox version before updating profile data. Add a startup check that validates profile version matches binary version.

## 2026-04-07 coordinator -- CAMOU_CONFIG over-specified: manually set WebGL/fonts/canvas that BrowserForge handles
- Root cause: Previous fix added `addWebGLConfig()`, `addFontConfig()`, and `canvas:aaOffset/aaCapOffset` to `BuildCamoufoxConfig()`. BrowserForge auto-populates these with realistic statistical distributions from its built-in database. Our static values were fingerprintable as a cluster.
- Fix: Removed addWebGLConfig, addFontConfig, font lists (windowsFonts/macosFonts/linuxFonts), and canvas noise from BuildCamoufoxConfig. BrowserForge now handles all of these automatically.
- Prevention: Read Camoufox docs before overriding properties. Rule of thumb: only set what we want to CONTROL (identity consistency). Let BrowserForge handle everything else.

## 2026-04-07 coordinator -- Missing navigator.language/languages and window.devicePixelRatio in CAMOU_CONFIG
- Root cause: BuildCamoufoxConfig set locale:language but not navigator.language/navigator.languages (official Camoufox property names). Also missing window.devicePixelRatio. Also missing OS-specific taskbar height for screen.availHeight.
- Fix: Added navigator.language, navigator.languages, window.devicePixelRatio. Replaced hardcoded -40 with screenAvailHeight() helper (Windows -40, macOS -25, Linux -28). Added extractLang/extractRegion helpers.
- Prevention: Cross-reference official Camoufox property name list when implementing CAMOU_CONFIG.

## 2026-04-07 coordinator -- Camoufox CAMOU_CONFIG format was completely wrong
- Root cause: `buildCamoufoxEnv()` created individual env vars like `CAMOU_CONFIG_SCREEN_W=1920` but Camoufox expects a JSON blob in `CAMOU_CONFIG_1`, `CAMOU_CONFIG_2`, etc. (chunked at 2000 bytes). Additionally the CamoufoxEnv map was never injected into browser launch options — only addon config used CAMOU_CONFIG_1.
- Fix: Rewrote to produce proper JSON with dot-path keys (`screen.width`, `navigator.userAgent`, etc.), added WebGL, font, and canvas config, merged fingerprint+addon config into single JSON blob, and removed Playwright context overrides that conflicted with Camoufox C++ level spoofing.
- Prevention: Read actual Camoufox documentation/source before implementing config format. The Python camoufox package shows the correct format.

## 2026-04-07 coordinator -- Playwright context overrides conflicted with Camoufox
- Root cause: `buildContextOptions()` set UserAgent, Locale, TimezoneId via Playwright context API even when Camoufox was active. This creates a detectable mismatch between JS-injected values and C++ level values.
- Fix: Added `hasCamoufox` flag — when true, skip UA/Locale/TimezoneId in context options. Keep Viewport (window size != screen size) and Geolocation permissions.
- Prevention: When using a browser fork that patches at C++ level, never also override the same values at the automation layer.

## 2026-04-07 coordinator -- examples/social is stale (pre-existing, not caused by our changes)
- Root cause: `examples/social/main.go` references old API signatures (`fetch.NewStealth` returning 2 values, `middleware.DefaultBlockDetectorConfig`, `export.NewJSONLinesWriter`, etc.)
- Fix: Not fixed — this is a pre-existing issue outside the scope of the Camoufox fingerprint work
- Prevention: Run `go build -tags playwright ./...` periodically to catch stale examples

## 2026-04-07 coordinator -- Google Search 429 was proxy IP reputation, not fingerprint
- Root cause: Proxy exit IP `154.6.48.10` is flagged by Google for `/search` specifically. Google homepage (200) and Maps work fine through the same proxy, but `/search` always returns 429. Direct (no proxy) returns 200 immediately.
- Fix: Need a proxy with clean Google Search reputation. The fingerprint fixes (below) are still valuable for general anti-detection.
- Prevention: Always test with direct connection first to isolate proxy vs fingerprint issues. Check proxy exit IP, not entry IP.

## 2026-04-07 coordinator -- Five fingerprint issues found in stealth fetcher
- Root cause: Multiple deviations from real Firefox fingerprint:
  1. `Windows NT 11.0` in UA — Windows 11 uses `NT 10.0` (Microsoft kept it)
  2. `Connection: keep-alive` header — illegal over HTTP/2, bot signal
  3. `Accept` missing `image/png,image/svg+xml` — Firefox includes these
  4. `Accept-Encoding` missing `zstd` — Firefox 138+ supports zstd
  5. `Accept-Language` quality factors wrong — used `q=0.9,0.8,...` instead of Firefox's `q=0.5` (for 2 langs)
  6. Missing `TE: trailers` header — Firefox sends this
  7. `Cache-Control: max-age=0` not needed for initial navigation — Firefox doesn't send this on first load
- Fix: All six issues fixed in both stealth_tls.go and stealth_default.go, plus identity profile data
- Prevention: Compare headers against real Firefox DevTools network tab before shipping

## 2026-04-06 coordinator -- Agent dispatch failed (Skill tool does not resolve subagent types)
- Root cause: Attempted to dispatch `dev-squad:architect`, `dev-squad:reviewer`, `dev-squad:backend` via Skill tool but these are Agent tool identifiers, not Skills
- Fix: Performed the audit directly as coordinator instead of dispatching
- Prevention: In environments without the Agent tool, coordinator must perform the work directly or use a different dispatch mechanism

## 2026-04-06 coordinator -- Logic audit: initially misdiagnosed P0-1 as deadlock
- Root cause: Failed to trace `defer p.mu.Unlock()` through the full fallback path in `GetForGeo`, initially concluded it was a deadlock when it was actually a fragile-but-functional unlock/relock pattern
- Fix: Re-read the exact code line by line and traced lock state through all paths before finalizing the report
- Prevention: Always trace defer statements to their execution point (function return), not where they appear in source order

## 2026-04-06 coordinator -- Captcha soft-block fix was too aggressive
- Root cause: Removed "blocked" and "forbidden" keywords entirely from soft-block detection, but existing tests expected them to still trigger when no normal page structure is present
- Fix: Kept all keywords but added structural check (no <nav, <footer, <main) as additional signal
- Prevention: Always run tests after each fix, not just at the end

## 2026-04-06 coordinator -- Performance audit: body-copy pattern is pervasive
- Root cause: Multiple packages (captcha/detect, middleware/blocked, parse/metadata) each independently call `string(resp.Body)` and `strings.ToLower()` creating 200KB+ of garbage per request
- Fix: Documented as H1+H2+M7-M9 in performance audit; need unified body scanning or cached lowercase body
- Prevention: When adding response body inspection, check if another layer already does it; consider adding a cached `LowerBody()` method on Response

## 2026-04-06 coordinator -- Performance fixes: DomainStats fields changed from values to atomics
- Root cause: Changing DomainStats fields from plain int64 to atomic.Int64 and AvgLatency/AvgProcessLatency from fields to methods broke tests that accessed them as fields
- Fix: Updated engine_test.go to call AvgProcessLatency() as a method
- Prevention: When changing struct fields to methods, always grep for all usage sites including test files before considering the change complete

## 2026-04-06 coordinator -- Performance fixes: circuitbreaker outcome.at removal
- Root cause: Removed `at time.Time` from outcome struct but forgot to update the struct literal in record()
- Fix: Removed `at: time.Now()` from the struct literal
- Prevention: When removing struct fields, always search for all initialization sites

## 2026-04-06 coordinator -- Xvfb "virtual" mode was a no-op in Go code
- Root cause: `headless: "virtual"` was documented as using Xvfb but Go code treated it identically to `headless: "true"` (native headless). Only the Docker entrypoint managed Xvfb, with no crash recovery, health monitoring, or cleanup.
- Fix: Created `fetch/display.go` — a proper Xvfb lifecycle manager with dynamic display allocation, crash monitoring/restart, stale lock cleanup, and /dev/shm validation. Wired into CamoufoxFetcher.
- Prevention: When adding a feature mode (like "virtual"), implement the actual behavior in Go code rather than relying on external shell scripts

## 2026-04-07 coordinator -- Browser timeout config not wired through in run.go
- Root cause: `cmd/foxhound/run.go` constructed CamoufoxFetcher without passing `WithBrowserTimeout(cfg.Fetch.Browser.Timeout.Duration)`, so user config was silently ignored
- Fix: Added `fetch.WithBrowserTimeout(cfg.Fetch.Browser.Timeout.Duration)` to camoufox options
- Prevention: When adding config fields, always grep for where the config is consumed and verify it's actually passed through

## 2026-04-07 coordinator -- Navigation used networkidle wait event causing slow/timeout on Google SERP
- Root cause: Default `WaitUntilStateNetworkidle` waits for all network activity to stop, which can take 10+ seconds on ad-heavy pages like Google SERP. With extension loaded, `WaitUntilStateLoad` was used which is also slow. Both eat into the navigation timeout budget.
- Fix: Changed default to `WaitUntilStateDomcontentloaded` (sufficient for content scraping). Added retry-with-escalation: if navigation times out, retry with 2x timeout and `domcontentloaded`
- Prevention: For scraping, `domcontentloaded` is almost always sufficient — the DOM is ready even if third-party scripts haven't finished

## 2026-05-02 coordinator -- v0.0.19 single-shot test methodology missed sustained-request pin failures
- Root cause: v0.0.19 end-to-end verification used single-shot requests (one request per target). azuretls DefaultPinManager only fails on the SECOND+ request to the same host within a session — the first request opens an extra handshake to capture the SPKI pin and always succeeds. Multi-edge CDN targets (Bing, Google, Cloudflare) rotate certificates across edges, so the second request often lands on a different edge with a different SPKI, triggering "pin verification failed for <host>".
- Fix: Set `sess.InsecureSkipVerify = true` by default in `NewStealth` (disables PinManager); add `WithStrictTLSVerify()` opt-in. See v0.0.20.
- Prevention: When testing TLS scraping fixes against multi-edge targets (Bing/Google/CF), make at least 2 sequential requests to the same host within the same session before declaring the fix correct. Single-shot testing is insufficient to surface PinManager-class failures. Source: v0.0.20 retrospective.

## 2026-04-29 backend -- azuretls ApplyHTTP2 bypasses browser-aware HTTP/2 defaults (issue #41)
- Root cause: `Session.ApplyHTTP2(fp)` calls `getDefaultHTTP2Transport()` which is NOT browser-aware (unlike `initHTTP2(browser)`). `applyPseudoHeaders` ends with `tr.HeaderPriority = defaultHeaderPriorities("")` — empty browser string. Once `HTTP2Transport != nil`, the lazy `initTransport(browser)` SKIPS `initHTTP2(browser)`, so `Priorities`, `SettingsOrder`, `ConnectionFlow`, and `HeaderPriority` fall to generic defaults. Pairing `WithJA3(firefox)` + `WithHTTP2Fingerprint(firefox)` produces JA3-says-Firefox / HTTP2-is-half-generic mismatch that Akamai-class validators detect and terminate with `tls: illegal parameter` (CDN obfuscation — alert misleading).
- Fix: `WithIdentity` only sets `session.Browser` (e.g. "firefox") and lets azuretls's built-in `GetLastFirefoxVersion` produce the ClientHello at request time — empirically the built-in spec works on Bing/DDG (verified through proxy) while our captured JA3 from `presets.FirefoxLatest()` was rejected with `tls: illegal parameter`. Captured strings drift faster than this repo updates; built-in is more current. The HTTP/2 layer is left to azuretls's browser-aware `initHTTP2(browser)`. Removed v0.0.18 multi-browser bundles (`ChromeLatest`/`SafariLatest`/`All`/`JA3Pool`) — they violated the project's "Camoufox only" stance and let users compose Firefox headers + Chrome TLS by accident. Manual `WithJA3 + WithHTTP2Fingerprint` pairing kept for power users with a `slog.Warn` at NewStealth that links the issue.
- Earlier rejected approach: tried auto-applying `presets.FirefoxLatest().JA3` inside `WithIdentity`. Failed end-to-end smoketest — bare azuretls Firefox passed Bing/DDG, but applying our preset string broke them. Lesson: don't assume curated capture > library default; the library default is updated more often than this repo.
- Prevention: When wrapping a third-party library that exposes setter methods for related state (TLS spec, HTTP/2 fingerprint), do not surface them as independent options if the library does not compose them safely. Equally important: when CLAUDE.md says "X only" and the code ships "X, Y, Z", question the code, not the principle. Don't preserve API surface that contradicts the project's stated design just because it was already there.
