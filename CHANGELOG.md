# Changelog

All notable changes to foxhound are documented in this file.

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
