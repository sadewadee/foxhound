# Changelog

All notable changes to foxhound are documented in this file.

## [v0.0.18] — 2026-04-27

### Added — TLS fingerprint customisation + build-mode safety (issues #39, #40)

- **`StealthFetcher.IsImpersonating()`** (`fetch/stealth_tls.go`, `fetch/stealth_default.go`): runtime accessor reporting whether the active build performs real JA3/JA4 TLS fingerprint impersonation. Returns `true` only when built with `-tags tls`. Lets consumers fail-fast at startup when the wrong build is shipped to production.
- **Startup log line in `NewStealth`**: emits `slog.Info` (tls build) or `slog.Warn`/`slog.Error` (default build) declaring which TLS implementation is active and which fingerprint customisations were requested. Operators see the mode in container logs without reading source.
- **`fetch.WithJA3(ja3)`** (`fetch/stealth_tls.go`): pin a specific JA3 ClientHello fingerprint via `azuretls.Session.ApplyJa3`. Apply order is deferred to the end of `NewStealth` so identity-driven browser selection is finalised before ApplyJa3 reads it. Invalid JA3 strings are logged and the session falls back to its default preset rather than panicking.
- **`fetch.WithJA3Pool(pool)`** (`fetch/stealth_tls.go`): pick a JA3 at random from the supplied pool. Pair with periodic fetcher recycling to rotate fingerprints across session lifetimes — turns a 1-fingerprint × N-requests cluster into N/recycle-window distinct fingerprints.
- **`fetch.WithHTTP2Fingerprint(fp)`** (`fetch/stealth_tls.go`): configure the Akamai-style HTTP/2 fingerprint via `azuretls.Session.ApplyHTTP2`. Format `<SETTINGS>|<WINDOW_UPDATE>|<PRIORITY>|<PSEUDO_HEADER>`.
- **`fetch.WithHTTP3Fingerprint(fp)`** (`fetch/stealth_tls.go`): configure QUIC/HTTP3 fingerprint via `azuretls.Session.ApplyHTTP3` for advanced consumers wrapping the underlying session directly.
- **`StealthFetcher.Session()`** (`fetch/stealth_tls.go`): accessor exposing the underlying `*azuretls.Session` for advanced configuration the option API does not yet cover (e.g. `GetClientHelloSpec` rotation, per-request `ForceHTTP3`).
- **`fetch/presets`** subpackage: curated `(JA3, HTTP/2)` bundles for Firefox 135, Chrome 131, and Safari 17 along with `Bundle`, `All()`, and `JA3Pool([]Bundle)` helpers. Backed by tests that validate both the structural shape and that azuretls accepts every preset under `-tags tls`.
- **No-op stubs in default build** (`fetch/stealth_default.go`): `WithJA3`, `WithJA3Pool`, `WithHTTP2Fingerprint`, `WithHTTP3Fingerprint` compile-compatible no-ops so consumer code builds without `-tags tls`. Calling any of them flips an internal flag that escalates the startup log from warn to error, surfacing the mismatch loudly.
- **Documentation updates**: README adds a "TLS Fingerprint Customisation" section with worked examples, a smoke-test `nm` recipe, and a clear warning that the default build's TLS layer is detectable. The `stealth_default.go` package doc was rewritten as a blunt fallback warning rather than the prior "Phase 1 foundation" copy that read like WIP.
- **Tests**: `stealth_default_test.go` pins `IsImpersonating()==false` on the default build; the existing `stealth_tls_test.go` gains `TestTLSStealthFetcher_IsImpersonating`, `TestTLSStealthFetcher_AppliesPresetBundle` (runs every curated bundle through real ApplyJa3 + ApplyHTTP2), `TestTLSStealthFetcher_InvalidJA3FallsBack` (graceful invalid-input handling), and `TestTLSStealthFetcher_JA3PoolPicks`. `stealth_fingerprint_test.go` covers build-tag-agnostic safety (empty pool, option composition, `IsImpersonating()` callable).

### Internal
- `StealthFetcher.pendingJA3 / pendingHTTP2 / pendingHTTP3` (`fetch/stealth_tls.go`): captured by the With* options and applied after every option ran so option order is irrelevant.
- `StealthFetcher.ja3Requested / http2Requested / http3Requested` (`fetch/stealth_default.go`): tracks customisation requests in the no-op build for the escalated log line.

### Why
Issue #39 documents a real production footgun: `serp-scraper` shipped to production for months believing TLS impersonation was active because the binary built and ran without `-tags tls`. Issue #40 documents the next layer: even with `-tags tls`, the API only exposed `WithIdentity`/`WithTimeout`/`WithProxy`, so 20 fetchers × 300 requests = 6000 requests with **2 unique JA3 hashes**. Combined, these changes make the build mode legible at runtime and unlock per-fetcher fingerprint rotation for high-volume scraping.

## [v0.0.17] — 2026-04-06

### Added — Session, Dev-Mode, Interception, Verified Solve

- **`foxhound.Session`** (`session.go`): new stateful, single-call client that owns a fetcher, an `http.CookieJar`, an optional identity profile, and an optional proxy URL, and reuses them across calls so cookies and identity persist for the lifetime of the session. Options: `WithSessionFetcher`, `WithSessionIdentity`, `WithSessionProxy`, `WithSessionCookieJar`. Methods: `Get`, `Fetch`, `CookiesFor`, `Identity`, `ProxyURL`, `Name`/`SetName`, `Fetcher`/`SetFetcher`, `Close`. Safe for concurrent use.
- **Multi-session routing** (`engine/hunt.go`, `engine/walker.go`, `foxhound.go`): `Hunt.AddSession(name, SessionConfig)` registers a named session; `Hunt.Session(name)` looks one up. `Job.SessionID` now routes individual jobs through the named session's fetcher instead of the Hunt's default fetcher — letting one Hunt drive multiple identities/proxies/cookie jars in a single campaign (e.g. index pages with one identity, detail pages with another).
- **`Hunt.WithDevelopmentMode(dir)`** (`engine/hunt.go`, `engine/walker.go`): first run hits the site and caches every `*foxhound.Response` to a SHA256-keyed file cache under `dir`; subsequent runs replay from disk without hitting the network. Massive iteration-speed boost when developing processors and pipelines. Cache writes are advisory — errors are logged but never abort the fetch.
- **`Trail.CaptureXHR(urlPattern)`** (`engine/trail.go`, `fetch/capture.go`): new builder method that registers a URL regexp pattern at Trail scope. At `ToJobs` time the patterns are attached to every produced job's `Meta` under `_foxhound_capture_xhr` and `FetchMode` is forced to `FetchBrowser`. The Camoufox fetcher compiles the per-job patterns and merges them with any fetcher-global `WithCaptureXHR` patterns so XHR/fetch response bodies matching either source are captured and delivered via `Response.Captures`.
- **`Hunt.WithBlockedDomains(...)` and `Hunt.WithDisableResources(...)`** (`engine/hunt.go`): Hunt-level builders that push intercept config to a `*fetch.CamoufoxFetcher` at Run time. Blocks requests by fully-qualified domain (e.g. `"ads.example.com"`) or by browser resource type (`"image"`, `"media"`, `"font"`, `"stylesheet"`). No-op with a warning when the underlying fetcher is not Camoufox.
- **`fetch.WithSolveCloudflare(timeout)` + `Response.CloudflareSolved`** (`fetch/camoufox_playwright.go`, `foxhound.go`): opt-in verified Cloudflare solve. After the normal CF challenge loop the fetcher polls three independent signals — the `cf_clearance` cookie, Turnstile DOM cleanliness, and `isCaptchaSolved("turnstile")` token presence — and only reports success when all three agree. The result is exposed to user code via `Response.CloudflareSolved`.
- **`fetch.WithInterceptConfig(ic)`** (`fetch/camoufox.go`, `fetch/camoufox_playwright.go`): functional option carrying the `InterceptConfig` struct used by `WithBlockedDomains` / `WithDisableResources`. When active, a playwright route handler aborts matching requests before they leave the browser.
- **`session_test.go`, `engine/p1_features_test.go`, `engine/trail_capturexhr_test.go`**: new tests for session cookie persistence, session routing through Hunt, dev-mode cache replay (second run makes zero fetcher calls), Trail XHR capture meta attachment + browser-mode forcing, and blocked-domains no-op safety on non-Camoufox fetchers.

### Internal
- `Job.SessionID string` (`foxhound.go`): new field for per-job session routing.
- `Response.CloudflareSolved bool` (`foxhound.go`): new field exposing verified solve state.
- `Walker.processJob` (`engine/walker.go`): restructured to check dev-mode cache before fetch (with `goto afterFetch` label on hit), route to per-job session fetcher when `job.SessionID` is set, and write the response to the dev-mode cache on success.
- `capturePatternsFromJob(job)` helper (`fetch/capture.go`) compiles `[]string` patterns attached via Trail meta into `*regexp.Regexp` for the navigate-time merge with fetcher-global capture patterns.

### Why
These are the long-standing "ergonomic gap" features identified in the capability audit: developers already had fetchers, processors, and pipelines, but nothing sat at the middle-ground between a one-shot static fetch and a full Hunt campaign. `Session` fills that gap, `AddSession` unlocks multi-identity campaigns without rewriting the walker, dev-mode drops iteration time from network-bound to disk-bound, `CaptureXHR` brings XHR interception to the fluent Trail API, `WithBlockedDomains`/`WithDisableResources` cut page-load cost on ad- and tracker-heavy sites, and verified Cloudflare solve closes the race where a challenge "disappears" visually but the clearance cookie hasn't materialised yet.

## [v0.0.16] — 2026-04-07

### Added — Anti-fragility / Adaptive Selectors (high-level API)
- **`Hunt.WithAdaptive(savePath)`** (`engine/hunt.go`): enable adaptive selector mode for a Hunt. Walker attaches the shared `*parse.AdaptiveExtractor` to every Response so user processors can call `resp.Adaptive(name)` / `resp.CSSAdaptive(selector, name)` without manual wiring. Pass empty string for in-memory only; pass a path to persist learned signatures as JSON across runs.
- **`Trail.Adaptive(name, selector)`** (`engine/trail.go`): builder method that records adaptive selector registration intent on the Trail. The walker applies the registration against the Hunt's shared extractor when the resulting Job is fetched, learning the element signature from the live page body.
- **`Response.Adaptive(name)`** (`foxhound.go`): retrieves the text of a registered adaptive selector. Falls back to similarity matching against the saved signature when the primary CSS selector finds nothing on the current page.
- **`Response.CSSAdaptive(selector, name)` / `CSSAdaptiveAll(selector, name)`** (`foxhound.go`): inline-register-and-extract shortcut. Registers the selector against the Hunt-scoped extractor, learns the signature from the current body, and returns a `*Selection` that supports `.Text()`, `.Texts()`, `.Attr()`, `.Attrs()`.
- **`Document.Adaptive() / SetAdaptive(ae)`** (`parse/goquery.go`): accessors for attaching an extractor to a Document for processors that build their own Document.
- **`parse.NewAdaptiveExtractorWithOptions(opts...)`** with `WithJSONStorage(path)` and `WithSQLiteStorage(path)` options (`parse/adaptive_factory.go`): single functional-options factory unifying the JSON-file and SQLite-backed persistence variants. The legacy `parse.NewAdaptiveExtractor(savePath)` constructor is retained for backward compatibility and is equivalent to `WithJSONStorage(savePath)`.
- **`examples/adaptive/main.go`**: runnable demonstration of an adaptive selector surviving a CSS class rename.

### Internal
- `Response` gained an unexported `adaptiveExtractor any` field with `SetAdaptiveExtractor` / `AdaptiveExtractor` accessors. Untyped to avoid an import cycle with `parse`.
- `foxhound.RegisterAdaptiveHooks` mirrors `RegisterHTMLSelectors`: the `parse` package registers its `*AdaptiveExtractor` implementation at `init()` so `Response.Adaptive` and `Response.CSSAdaptive` resolve without importing `parse` from the root package.

### Why
The audit at `docs/audit/2026-04-07-capability-audit.md` identified that the full adaptive selector machinery (`parse/adaptive.go`, `parse/adaptive_sqlite.go`, `parse/similarity.go`) was implemented and tested but completely invisible from `Trail`, `Response`, and `Hunt` — every consumer would have had to construct an extractor by hand. v0.0.16 is pure plumbing on top of the existing algorithms, exposing the framework's biggest hidden strength as a first-class API.

## [v0.0.15] — 2026-04-07

### Fixed — Captcha Solving
- **NopeCHA "solved then fallback" bug** (`fetch/camoufox_playwright.go`): after extension successfully solved a reCAPTCHA, the post-solve check used `detectCaptchaType()` which only checks for captcha markup presence — markup remains in DOM after a successful solve, only the response token is filled. Now correctly uses `isCaptchaSolved()` to verify the token is present before falling back to manual handling.
- **Captcha solve timeout too short** (23s for all types): replaced fixed 20-iteration loop with per-type budgets — turnstile 20s, hcaptcha 45s, geetest 45s, **recaptcha 90s** (Enterprise can take 30-60s).
- **reCAPTCHA detection too narrow**: `isCaptchaSolved` only checked `textarea[name="g-recaptcha-response"]`. Now checks all `g-recaptcha-response*` textareas, the `grecaptcha.getResponse()` API, the `grecaptcha.enterprise.getResponse()` namespace, and hidden inputs used by Enterprise widgets.

### Verified
- NopeCHA addon successfully solves reCAPTCHA v2 on `nopecha.com/demo/recaptcha` in ~40 seconds via Camoufox
- reCAPTCHA Enterprise (used by Salesforce LWR sites like Yoga Alliance) is **NOT solvable** by the free NopeCHA addon — requires paid API key (NopeCHA Token API, CapSolver, or 2Captcha)

## [v0.0.14] — 2026-04-07

### Fixed — Security (24 fixes)
- **CSS selector injection** (`engine/trail.go`): user-supplied selector interpolated into JS without escaping — arbitrary JS execution in browser context
- **Proxy GetForGeo double-panic** (`proxy/pool.go`): fragile mutex unlock/relock dance — crash on fallback path panic
- **Unbounded io.ReadAll** (`fetch/stealth_default.go`): wrapped with `io.LimitReader(body, 10MB)` to prevent OOM
- **Proxy credentials logged** in full URL strings across pool.go and health.go
- **Webhook writer SSRF**: accepted arbitrary URLs without validation
- **Cookie file world-readable**: written with `0o644` instead of `0o600`
- **Dedup seen-set unbounded growth**: memory leak on long crawls
- **Fetcher resource leak**: `Hunt.Run` never called `Close()` on middleware-wrapped fetcher

### Fixed — Logic Errors (19 fixes)
- **Circuit breaker permanently tripped** (`middleware/circuitbreaker.go`): sliding window not reset on half-open→closed transition
- **FetchAuto (value 0) unusable** (`engine/hunt.go`): zero value treated as "unset" and overridden to FetchBrowser — added `PoolFetchModeSet` flag
- **Jitter inverts Min/Max** (`behavior/profiles.go`): independent jittering could make Min > Max — now swaps if inverted
- **Blocked responses counted as successful** (`engine/stats.go`): blocked/CAPTCHA responses inflated SuccessCount
- **Walker double-counting** (`engine/walker.go`): `RecordRequest` called twice for CAPTCHA responses
- **Login-wall false positives** (`middleware/blocked.go`): matched ANY page containing "login" text
- **Soft-block false positives** (`captcha/detect.go`): "blocked" keyword on legitimate small pages
- **Queue dedup key wrong** (`queue/reliable.go`): used `job.URL` instead of `job.ID` — silently dropped jobs with same URL
- **Hunt drain/settle race** (`engine/hunt.go`): `activeWalkers.Add(1)` moved before `processJob`
- **Nil resp panic** (`middleware/retry.go`): accessed `resp.StatusCode` before nil check
- **Thundering herd** (`middleware/robotstxt.go`): added singleflight per domain
- **GaussianClamped unbounded** (`behavior/distributions.go`): fallback clamped to `[-bound, +bound]`
- **Table thead colspan ignored** (`parse/table.go`): header extraction now respects `colspan`
- **Domain delay lock window** (`middleware/domaindelay.go`): reserve time slot before releasing lock
- **Context-unaware sleep** (`fetch/stealth_tls.go`): `time.Sleep` → `select` with `ctx.Done()`

### Fixed — Performance (37 fixes)
- **Body lowercase 2x per request** (`captcha/detect.go`, `middleware/blocked.go`): scan first 10KB only — saves ~300KB garbage/request
- **Timer leak 6000+/min** (`engine/hunt.go`): `time.After()` in polling loop → reusable `time.NewTicker`
- **Stats write-lock contention** (`engine/stats.go`): `sync.RWMutex` → `sync.Map` + `atomic.Int64` for lock-free reads/writes
- **Linear scans in hot paths**: `isMetadataHost` → `map[string]struct{}`; `findEntry` → `indexByURL` map; `GetForGeo` normalize at insertion
- **Find("*") DOM scan** (`parse/finder.go`): `FindByAttr`/`FindByAttrContains` → CSS attribute selectors `[attr='val']`
- **Multiple HTML re-parses** (`parse/metadata.go`): added `FromDoc()` variants sharing single `goquery.Document`
- **fmt.Sprintf in hot paths**: replaced with `strconv.Itoa`/string concat across identity, monitor, pipeline
- **Regex compiled per-call** → package-level `var`
- **Autothrottle alloc** (`middleware/autothrottle.go`): pre-allocated scratch buffer instead of `make([]float64)` per request
- **Queue length without mutex** (`queue/memory.go`): atomic length counter
- **Domain scorer lock** (`fetch/domain_score.go`): `sync.Mutex` → `sync.RWMutex` for concurrent reads
- **SOCKS5 dialer recreated per conn** (`fetch/socks5_bridge.go`): cached on struct
- **Dedup canonicalURL fast path** (`middleware/dedup.go`): skip sort+rebuild when no query string

### Fixed — Xvfb Display Server (7 fixes)
- **`"virtual"` headless mode was a no-op**: Go now manages Xvfb lifecycle via new `fetch/display.go`
- **Dynamic display allocation** `:99`-`:199` with stale lock cleanup
- **Health monitoring**: background goroutine checks every 5s, auto-restart with exponential backoff
- **Docker Xvfb fire-and-forget**: removed from entrypoint, Go manages directly
- **`/dev/shm` validation**: warns if shared memory < 128MB
- **`shm_size: "256m"`** added to docker-compose.yml

### Fixed — Browser Navigation Timeout (3 fixes)
- **Config timeout not wired** (`cmd/foxhound/run.go`): `WithBrowserTimeout()` now passed to CamoufoxFetcher
- **Wait event too slow**: `networkidle` → `domcontentloaded` (2-10s faster on ad-heavy pages)
- **No retry on timeout**: added retry with 2x timeout escalation (max 120s)

### Fixed — Fingerprint (6 fixes)
- **Illegal HTTP/2 header**: removed `Connection: keep-alive`
- **Accept header**: added `image/png,image/svg+xml` matching Firefox
- **Accept-Encoding**: added `zstd` (Firefox 138+)
- **TE header**: added `TE: trailers`
- **Accept-Language q-factor**: fixed to Firefox pattern (`q=0.5`)
- **Windows NT version**: `"11.0"` → `"10.0"` (Win11 uses NT 10.0 in UA)

### Fixed — Camoufox Integration (6 critical fixes)
- **CAMOU_CONFIG format wrong**: individual env vars → proper JSON blob with dot-path keys
- **Config never passed to browser**: `profile.CamoufoxEnv` was generated but never injected into `launchOpts.Env`
- **Playwright context conflicts**: removed UA/Locale/TimezoneId overrides when Camoufox active (C++ level handles these)
- **Removed manual WebGL/fonts/canvas**: let BrowserForge auto-populate with realistic statistical distributions
- **Firefox version mismatched**: aligned all profiles to installed Camoufox binary (148.0 → 135.0)
- **Cookie jar missing** (`fetch/stealth_default.go`): added `cookiejar` for session persistence across requests

### Added
- `fetch/display.go` — Xvfb lifecycle manager with crash recovery
- `fetch/display_stub.go` — no-op stub for non-playwright builds
- `identity/data/tls/firefox_135.0.json` — TLS profile for Firefox 135
- `tests/integration/scrape_test.go` — 11 integration tests against real sites (Bing, DDG, Kompas, Google Maps, httpbin)
- `Job.NavigationTimeout` field for per-job timeout override

### Changed
- 54 files changed, 3084 insertions, 622 deletions
- All 1200+ unit tests pass
- Integration tested: Bing (10 results), DuckDuckGo (9 results), Kompas (416 URLs), Google Maps (20-34 businesses), httpbin (proxy/identity verified)

### Dependencies
- `go-jose/v3` upgraded v3.0.4 → v3.0.5 (fixes JWE decryption panic CVE)

## [v0.0.12] — 2026-04-04

### Added — SOCKS5 Auth Proxy Bridge
- **Transparent SOCKS5 auth bridge for browser path**: Firefox/Playwright does not support SOCKS5 proxies with authentication (Mozilla bug #122752, open since 2002). Foxhound now auto-detects `socks5://user:pass@host:port` and spawns a local unauthenticated SOCKS5 listener that relays traffic to the upstream proxy with credentials. Zero config — works from the URL protocol alone.
- **New file `fetch/socks5_bridge.go`**: server-side SOCKS5 handshake (RFC 1928, CONNECT only), bidirectional relay via `io.Copy`, proper goroutine lifecycle with `sync.WaitGroup`
- **Bridge survives browser restarts**: independent of browser lifecycle, persists across `restart()` cycles
- **4 new tests**: `TestNeedsSocks5Bridge` (9 cases), `TestSocks5BridgeStartClose`, `TestSocks5BridgeRelay` (end-to-end), `TestSocks5BridgeUnsupportedCmd`

### Added — NopeCHA Token API Solver
- **New captcha provider `"nopecha"`**: implements `Solver` interface using NopeCHA Token API (`POST /token/` → poll `GET /token/?id=`). Supports Turnstile (1 credit), hCaptcha (5 credits), reCAPTCHA v2 (20 credits).
- **Conditional addon loading**: when `captcha.provider: "nopecha"` with a valid API key, the NopeCHA browser addon is NOT loaded — API and addon never run simultaneously. When API key is absent/invalid, addon loads as fallback (default behavior unchanged).
- **New file `captcha/nopecha.go`**: `NopeCHA` struct with `Solve()` and `Balance()` methods
- **New option `WithSkipExtension()`**: prevents NopeCHA addon auto-load when API solver is active
- **7 new tests**: Turnstile, reCAPTCHA, hCaptcha solve + payload validation + balance + error + context cancel
- **Config**: `captcha.provider: "nopecha"`, `captcha.api_key: "${NOPECHA_API_KEY}"`

### Changed
- **Version bumped to v0.0.12**
- **`golang.org/x/net`** promoted from indirect to direct dependency (for `proxy.SOCKS5()` in bridge)

## [v0.0.11] — 2026-04-04

### Fixed — Captcha/Cloudflare Handling
- **Turnstile misclassified as js_challenge**: `challenge-platform` appears in Turnstile pages — reordered `detectCloudflare` to check Turnstile first, removed ambiguous marker
- **under_attack false positive**: "ray id" + "cloudflare" matched all CF error footers — tightened to `cf-chl-bypass` only
- **5s+ wasted on unresolved CF pages**: `simulateHumanBehavior` + `handleCookieConsent` now skip when Cloudflare challenge is still active
- **Double Turnstile handling**: Cloudflare loop manual click conflicted with NopeCHA extension — defers Turnstile to extension when loaded
- **22+ page.Content() calls per Turnstile page**: poll loops in `handleCloudflare` replaced with lightweight `page.Evaluate` JS checks
- **Extension elapsed time log off by 3s**: now includes the 3s init sleep in reported seconds
- **Multi-widget Turnstile inconsistency**: `isCaptchaSolved` used `querySelector` (first only) while `detectCloudflare` used `querySelectorAll` — now consistent
- **submitAfterCaptcha broad selectors**: `button:has-text('Continue')` and `input[type='submit']` could click unrelated buttons — scoped to captcha/challenge forms
- **waitUntil override order**: `hasWaitStep` overrode `hasExtension`, causing `domcontentloaded` before extension content scripts loaded — extension now wins
- **No fallback from extension to manual**: if NopeCHA fails, now falls back to `handleRecaptcha`/`handleHCaptcha`
- **detectCloudflare swallowed page.Content() errors**: now logs at debug level

### Fixed — SmartFetcher
- **Cautious timeout poisoned browser escalation**: the shortened context was reused for `browser.Fetch`, causing immediate failure — now preserves parent context
- **Static fetch error was terminal**: timeout/DNS/connection errors now escalate to browser instead of returning error

### Fixed — DomainScorer
- **Negative clock drift amplified scores**: `decayFactor` with negative `time.Since` produced values > 1.0 — now clamps age to zero
- **Unnecessary allocation in getOrCreate**: `sync.Map.LoadOrStore` eagerly allocated `DomainScore` on every call — added fast-path `Load` check

### Fixed — PagePool
- **Close() sent nil to blocked acquirers**: caused panic — `Acquire` now returns error when channel closes
- **Release after Close leaked counters**: `created` not decremented, `usageCount` not cleaned — now properly tracked
- **WarmUp race with Acquire**: non-atomic `Load`+`Add` could exceed `maxSize` — now uses CAS like `Acquire`

### Fixed — Build
- **WithBehaviorProfile missing from non-playwright build**: stub `camoufox.go` lacked the option function and struct field — compile error when used without `-tags playwright`

## [v0.0.10] — 2026-04-04

### Fixed
- **NopeCHA extension solve skipped** (#36): removed incorrect `checkNopeCHAKey()` gate that treated the browser extension signing key in `manifest.json` as an API key. NopeCHA browser extension solves captchas without an API key — the gate caused `waitForExtensionSolve()` to exit immediately, making the extension a pure detection signal with zero benefit.
- **5-second unconditional sleep on every browser request**: `waitForExtensionSolve()` slept 5s before checking if a captcha even existed. Now detects captcha first, only sleeps when there's actually work for the extension.
- **Double `detectCloudflare()` call**: `handleCloudflare()` re-detected the challenge type that the caller already detected. Now accepts `cfType` as a parameter.
- **GeeTest false-positive solve detection**: only checked element existence (`inp != null`), not token value. Now checks `inp.value.length > 10` like all other captcha types.
- **Poll loop sleep-before-check**: solve polling slept 1s before first check, adding unnecessary delay when extension already solved during init wait. Now checks first, then sleeps.
- **30s impossible wait in handleRecaptcha/handleHCaptcha**: when no extension loaded (`hasExtension=false`), the code still waited 30s for an extension to solve image challenges. Removed the dead wait loops.

### Refactored
- Extracted `detectCaptchaType()` and `isCaptchaSolved()` helpers from `waitForExtensionSolve()` for reuse and clarity
- Errors from `page.Content()` and `page.Evaluate()` now logged at debug level instead of silently discarded

### Removed
- `checkNopeCHAKey()` function and `nopechaHasKey` field from `CamoufoxFetcher`

### Closed Issues
#36

## [v0.0.9] — 2026-04-02

### Added
- **Storage state export/import** (`fetch/camoufox_playwright.go`): `WithStorageState(path)` saves/loads browser session to JSON. Auto-saves on Close, auto-loads on startup.
- **Login trail helper** (`engine/trail.go`): `engine.Login()` convenience for building login flows
- **Reliable queue** (`queue/reliable.go`): `ReliableQueue` wrapper with Ack/Nack/DLQ semantics, stale job recovery, `RetryDLQ()`
- **Stats collector** (`monitor/collector.go`): `StatsCollector` bridges any `StatsSource` to sinks (Prometheus, logging, alerting)
- **LogSink** for structured periodic stats logging

### Fixed
- PersistentCookies: added Expires/MaxAge fields, clone Job before mutation to prevent data races
- Contact extraction (#33): email rejects .avif/.bmp/.tiff/.ico, no-reply addresses, RFC 2606 domains, infrastructure domains; phone rejects IP addresses, version numbers, CSS dimensions, descending sequences

### Closed Issues
#33

## [v0.0.8] — 2026-04-02

### Added
- **HTML table extraction** (`parse/table.go`): `ExtractTable`, `ExtractTables`, `Table.AsItems()` with colspan/rowspan grid-fill algorithm
- **JS preloaded data** (`parse/preload.go`): `ExtractWindowVar`, `ExtractPreloadedData`, `DetectFramework` with balanced-brace JSON extraction for Next.js, Nuxt, Redux, Apollo, Relay
- **Directory parser** (`parse/directory.go`): `ExtractListings` (JSON-LD → Microdata → DOM patterns), `NormalizeAddress`, `NormalizeRating`
- **Pagination accumulator** (`parse/paginator.go`): `DetectPagination` (multi-signal scoring), `AssemblePages`, `ExtractArticleFromPageBreaks`
- **Auto-detection engine** (`parse/autodetect.go`): `DetectContentType` (7-factor heuristic), `AutoExtract`, `ExtractArticle` (Readability-style DOM scoring)

## [v0.0.7] — 2026-04-02

### Added
- **Distribution library** (`behavior/distributions.go`): Weibull, Gamma (Marsaglia-Tsang), Gaussian (rejection sampling), LogNormal samplers — pure Go, zero dependencies
- **Bigram typing model** (`behavior/keyboard.go`): per-character speed varies by letter frequency, QWERTY hand/finger transitions, position fatigue. LogNormal variance (CV ~30%)
- **Session fatigue model** (`behavior/fatigue.go`): inverted-U speed curve with warmup decay + fatigue buildup + per-call Gaussian noise
- **Per-session profile jitter** (`behavior/profiles.go`): `profile.Jitter()` perturbs all parameters ±15% to prevent anti-bot clustering
- **Bayesian domain risk scoring** (`fetch/domain_score.go`): Beta posterior mean with asymmetric time decay. `SocialMediaScoreConfig()` preset with Beta(3,1) prior
- **SmartFetcher learning**: auto-escalates to browser for high-risk domains via `WithDomainScorer()`
- **Circuit breaker middleware** (`middleware/circuitbreaker.go`): 3-state FSM with exponential backoff ±50% jitter
- **AutoThrottle outlier dampening**: ring buffer + MAD clamp, configurable EMA alpha
- **Page reuse limit** (`fetch/pagepool.go`): `WithPageReuseLimit(n)` for per-page request counting with automatic pool recycling
- **NopeCHA key detection**: skips 20s extension wait when no API key configured
- **Network error retry** (`middleware/retry.go`): retries NS_ERROR_NET_RESET, timeout, EOF, connection reset
- **Social media scraping example** (`examples/social/main.go`)
- **Configurable cautious timeout**: `WithCautiousTimeout(d)` on SmartFetcher

### Changed
- Mouse jitter: uniform → Gaussian (rejection sampling, no boundary spikes)
- Mouse movement in browser: single teleport → full bezier path traversal with 5-15ms waypoints + idle micro-drift
- Click duration: uniform [50,150ms] → LogNormal (median 90ms)
- Rhythm burst/pause: uniform → Weibull distributions (right-skewed, mode-heavy)
- Scroll distances: uniform → Gamma distribution; pauses → Weibull
- DomainDelay jitter: uniform ±25% → log-normal (sigma=0.3, CV=0.31)
- Circuit breaker backoff jitter: ±10% → ±50%
- Keyboard same-finger penalty: 1.4× → 2.0× (matches research)
- Keyboard variance: LogNormal sigma 0.15 → 0.35 (CV 2.3% → ~30%)

### Fixed
- DomainDelay `Randomize` field stored but never applied in `delayFor()`
- CamoufoxFetcher ignored `BehaviorProfile` — always used `DefaultScrollConfig()`/`DefaultKeyboardConfig()`
- Circuit breaker half-open state allowed concurrent probes
- `/tmp/foxhound-addon-*` temp directories leaked on restart/close
- Page pool not drained on browser restart (stale pages from dead browser)
- `waitForExtensionSolve` waited 20s even when NopeCHA had no API key
- Flaky `TestHunt_OnError_CalledOnFetchFailure` timeout (5s → 10s)

### Closed Issues
#21, #22, #23, #24, #25, #26, #28, #30, #31, #32

## [v0.0.6] — 2026-03-21

### Added
- Cookie injection via `WithBrowserCookies()` and `Response.Cookies` export (#28)
- Sequential Cloudflare challenge retry (up to 3 cycles) (#30)
- PagePool integration into CamoufoxFetcher via `WithPoolSize()` (#25)
- Cloudflare bypass patterns documentation (`docs/cloudflare.md`) (#31)
- Paginated HTML accumulation in `Response.Body` (#24)

### Fixed
- Phone/email extraction false positives — raised minimum digit threshold, added domain/pattern filters (#21, #22)
- Extension timeout reduced from 45s to 15s (#30)

## [v0.0.5] — 2026-03-20

### Added
- Initial public release
- Dual-mode fetching: static (TLS-impersonating) + browser (Camoufox/Juggler)
- Smart router with auto-escalation on block detection
- Identity system: 60 embedded device profiles with UA/TLS/header/OS consistency
- NopeCHA CAPTCHA extension auto-download
- Hunt/Trail/Walker architecture for scraping campaigns
- 13 middleware layers (rate limit, dedup, autothrottle, cookies, retry, etc.)
- Parse subsystem: CSS, JSON, XPath, regex, structured, metadata, contact, sitemap, feed
- Pipeline: validate, clean, dedup, transform with CSV/JSON/JSONL/webhook export
- Queue: memory (heap), Redis (sorted set), SQLite (persistent)
- Cache: memory (LRU+TTL), file (SHA256), Redis, SQLite
- Monitoring: Prometheus metrics, webhook alerting
- CAPTCHA: detect + solve via CapSolver, 2Captcha, Turnstile
- CLI: init, run, check, proxy-test, shell, browser-shell, resume
- Docker support with compose scaling
