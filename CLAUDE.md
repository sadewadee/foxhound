# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Foxhound is a Go scraping framework with native Camoufox (Firefox fork) anti-detection. It uses dual-mode fetching: a TLS-impersonating HTTP client for static pages and Camoufox via playwright-go for JS-heavy/protected pages, with automatic escalation when blocks are detected.

**Status**: v0.0.19. 19 packages, 1200+ tests. NopeCHA CAPTCHA extension auto-downloads on first launch. Adaptive selectors are exposed via `Hunt.WithAdaptive`, `Trail.Adaptive`, `Response.Adaptive`, `Response.CSSAdaptive`, and `Response.CSSAdaptiveAll`. v0.0.17 adds the stateful `foxhound.Session` type (fetcher + cookie jar + identity + proxy), multi-session routing via `Hunt.AddSession` + `Job.SessionID`, `Hunt.WithDevelopmentMode(dir)` for on-disk response replay, `Trail.CaptureXHR(pattern)` fluent XHR capture, `Hunt.WithBlockedDomains` / `Hunt.WithDisableResources` for browser-layer request filtering, and verified Cloudflare solve via `fetch.WithSolveCloudflare(timeout)` exposed through `Response.CloudflareSolved`. v0.0.18 adds `StealthFetcher.IsImpersonating()` + startup log so consumers can fail-fast when built without `-tags tls`, and exposes `WithJA3` / `WithJA3Pool` / `WithHTTP2Fingerprint` / `WithHTTP3Fingerprint` for advanced fingerprint customisation. v0.0.19 fixes issue #41: `WithIdentity` alone is now sufficient for fingerprint consistency — it sets `session.Browser` so azuretls's built-in `GetLastFirefoxVersion` (or matching browser preset) produces a current ClientHello at request time, and leaves `HTTP2Transport` nil so the browser-aware `initHTTP2(browser)` provides matching HTTP/2. Verified end-to-end against Bing and DuckDuckGo through a datacenter proxy. Captured JA3 strings drift faster than this repo updates, so the curated `presets.FirefoxLatest()` is opt-in only via `WithJA3`. The v0.0.18 multi-browser bundles (`ChromeLatest`/`SafariLatest`/`All`/`JA3Pool`) are removed — they violated foxhound's "Camoufox/Firefox only" stance. Manual `WithJA3`+`WithHTTP2Fingerprint` pairing now logs a startup warning. Only issue #27 (SERP docs), #29 (leech/seed), #34 (proxy intelligence) remain open.

**Browser**: Camoufox only. No Chromium, Nightly, or other browsers.

## Build & Test

```bash
# Build everything
go build ./...

# Build with browser support (playwright)
go build -tags playwright ./...

# Run all tests
go test ./...

# Run tests with race detector
go test -race ./...

# Run single package tests
go test ./engine/...
go test ./fetch/...

# Build CLI binary
go build -o foxhound ./cmd/foxhound/

# CLI commands
foxhound run --config config.yaml
foxhound --headless false run --config config.yaml   # global headless flag
foxhound init myproject
foxhound check
foxhound proxy-test --config config.yaml
foxhound shell
foxhound browser-shell                               # interactive Camoufox REPL
foxhound browser-shell --headless true
foxhound resume --hunt-id <id> --queue redis://localhost:6379

# Docker
docker compose up
docker compose up --scale foxhound=4
docker compose --profile monitoring up
```

## Architecture

### Dual-Mode Fetcher (core differentiator)
- **Static path** (`FetchStatic`): Go HTTP client with header ordering from identity profile. ~5-50ms/req. (`fetch/stealth.go`)
- **Browser path** (`FetchBrowser`): Camoufox (Firefox fork) via playwright-go using Juggler protocol. ~500ms-5s/req. (`fetch/camoufox_playwright.go`)
- **Smart Router** (`FetchAuto`): starts static, auto-escalates to browser on block detection (403/429/503). (`fetch/smart.go`)

### NopeCHA Extension (auto-download)
NopeCHA CAPTCHA-solving extension is downloaded from GitHub (`NopeCHALLC/nopecha-extension`) and loaded into Camoufox by default. Cached at `~/.cache/foxhound/extensions/nopecha/`. Disable with `extension_path: "none"` in config or `WithExtensionPath("none")`.

### Identity System
Every request uses a complete, internally-consistent identity profile: UA + TLS fingerprint + header order + OS + hardware + screen + locale + geo must all match. 60 embedded device profiles via Go `embed` directive in `identity/data/`. Generate with functional options:
```go
id := identity.Generate(identity.WithBrowser(identity.BrowserFirefox), identity.WithOS(identity.OSWindows))
```

### Camoufox Fingerprint Config (CAMOU_CONFIG)
The identity system builds a JSON config for Camoufox via `Profile.BuildCamoufoxConfig()`. This JSON is chunked into `CAMOU_CONFIG_1`, `CAMOU_CONFIG_2`, ... env vars (max 2000 bytes each). The config includes:
- Screen dimensions (`screen.width`, `screen.height`, etc.)
- Navigator properties (`navigator.userAgent`, `navigator.platform`, `navigator.oscpu`, etc.)
- WebGL vendor/renderer strings matched to the declared GPU
- OS-specific font lists (Windows/macOS/Linux)
- Canvas noise (`canvas:aaOffset`)
- Timezone, locale, geolocation

When the Camoufox binary is active, `buildContextOptions()` does NOT set UserAgent, Locale, or TimezoneId via Playwright — Camoufox handles these at C++ level. The addon config (NopeCHA) is merged into the same JSON blob via `Profile.MergeCamoufoxConfig()`.

### Key Terminology
- **Hunt** (`engine/hunt.go`): scraping campaign orchestrator — seeds queue, launches walkers, collects stats
- **Trail** (`engine/trail.go`): fluent navigation path builder (Navigate → Click → Fill → Wait → Scroll → Evaluate → Extract)
- **Walker** (`engine/walker.go`): goroutine that pops jobs, fetches, processes, writes items, enqueues discovered jobs
- **Job** (`foxhound.go`): unit of work (URL + FetchMode + Priority + Steps + Meta)
- **ItemList** (`engine/collect.go`): thread-safe item collection with CSV/JSON/JSONL export

### Browser Steps (JobStep)
Steps are browser actions executed after page load, before content extraction:
| Step | Constant | Description |
|------|----------|-------------|
| Navigate | 0 | Page navigation |
| Click | 1 | Click element (hard failure unless Optional) |
| Wait | 2 | Wait for selector (hard failure unless Optional) |
| Extract | 3 | Handled by Processor after fetch |
| Scroll | 4 | Human-like scroll via behavior.Scroll |
| InfiniteScroll | 5 | Scroll until no new content; supports custom container (Selector) and StopSelector/StopCount |
| LoadMore | 6 | Click "load more" button repeatedly |
| Paginate | 7 | Follow pagination links, accumulate all pages HTML |
| Evaluate | 8 | Execute custom JS, return value in Response.StepResults |
| Fill | 9 | Type text with human-like keystrokes via behavior.Keyboard |

### Trail API Example
```go
trail := engine.NewTrail("maps-search").
    Navigate("https://www.google.com/maps").
    Fill("input#searchboxinput", "cafe in canggu").
    Click("button#searchbox-searchbutton").
    WaitOptional("div[role='feed']", 10*time.Second).
    ClickOptional("button.cookie-dismiss").
    InfiniteScrollInUntil("div[role='feed']", "div.Nv2PK", 20, 100).
    Evaluate("() => document.querySelectorAll('.Nv2PK').length")
```

### Request Data Flow
```
Job → middleware chain (rate limit → dedup → autothrottle → cookies → referer → retry)
  → Smart Fetcher (static or browser) → Steps → Parser → User Process()
  → Result{Items, Jobs} → Pipeline chain (validate → clean → dedup → transform)
  → Writers (CSV/JSON/webhook) + Queue (new jobs)
```

### Package Map
```
foxhound/
  foxhound.go     — core types: Job, Response, Item, Result, Fetcher, Queue, Pipeline, Writer, Middleware
  config.go       — YAML config parser with env var expansion and defaults
  engine/         — hunt, walker, trail, scheduler, retry, stats, collect (ItemList/HuntMetrics/HuntResult)
  identity/       — profile generation, embedded device/TLS/header databases (60 profiles)
  fetch/          — stealth (HTTP+headers), camoufox (browser), smart (auto-router), capture (XHR), pagepool,
                    domain_score.go (Bayesian domain risk scoring), socks5_bridge.go (transparent SOCKS5 auth relay)
  proxy/          — pool (+ GetForGeo), health, cooldown, static provider
  proxy/providers — brightdata, oxylabs, smartproxy adapters
  behavior/       — timing (log-normal), mouse (bezier), scroll, keyboard, navigation, profiles,
                    distributions.go (Weibull/Gamma/Gaussian samplers), fatigue.go (session warmup/fatigue model)
  middleware/     — ratelimit, dedup, depth, retry, autothrottle, cookies, cookies_persist, referer, redirect, deltafetch, metrics,
                    circuitbreaker.go (3-state FSM circuit breaker)
  parse/          — goquery (CSS), json (dot-path), xpath (subset→CSS), regex, structured (schema),
                    content (markdown/text), metadata (JSON-LD/OG/NextData/Nuxt), contact (email/phone),
                    sitemap, feed (RSS/Atom), finder,
                    adaptive (signature-based selectors that survive DOM rewrites — exposed via
                    Hunt.WithAdaptive, Trail.Adaptive, Response.Adaptive/CSSAdaptive/CSSAdaptiveAll;
                    NewAdaptiveExtractorWithOptions + WithJSONStorage/WithSQLiteStorage factory),
                    adaptive_sqlite,
                    table (HTML table→grid with colspan/rowspan), preload (JS window vars, framework detection),
                    directory (listing extraction: JSON-LD/Microdata/DOM), paginator (detection + assembly),
                    autodetect (content type heuristic, readability-style article extraction)
  pipeline/       — validate, clean, dedup, transform, field_transform (regex/rename/coerce), chain
  pipeline/export — json/jsonl, csv, webhook writers
  queue/          — memory (heap), redis (sorted set), sqlite (persistent)
  cache/          — memory (LRU+TTL), file (SHA256), redis, sqlite
  monitor/        — stats (atomic counters), prometheus (isolated registry), alerting (webhook rules)
  captcha/        — detect (cloudflare/recaptcha/hcaptcha/geetest), capsolver, twocaptcha, nopecha (Token API), turnstile
  cmd/foxhound/   — CLI: init, run, check, proxy-test, shell, browser-shell, resume, curl2fox, preview
  examples/       — ecommerce (books.toscrape.com), travel, realtime price monitor
```

## Key Dependencies

- `goquery` — HTML/CSS selector parsing
- `playwright-go` — Camoufox browser automation (build tag: playwright)
- `go-redis/v9` — Redis client (queue, cache)
- `modernc.org/sqlite` — pure-Go SQLite (queue, cache)
- `prometheus/client_golang` — metrics
- `golang.org/x/time/rate` — rate limiting
- `gopkg.in/yaml.v3` — config parsing

## Anti-Detection Design Principles

These must be maintained in all implementation work:

1. **Consistency over randomness**: all identity attributes (UA, TLS, headers, OS, hardware, screen, locale, geo) must be internally consistent.
2. **Human timing uses log-normal distribution** (`behavior/timing.go`), not uniform random.
3. **Camoufox only** — Juggler protocol is less targeted by anti-bot than CDP. No Chromium/Nightly.
4. **NopeCHA auto-loaded** — CAPTCHA solving is built-in, no API key needed.
5. **Goal is to never trigger CAPTCHA**. If CAPTCHA appears, earlier layers failed.
6. **Proxy geo must match identity locale/timezone**.
7. **Human timing uses Weibull/Gamma distributions** — not uniform random. Burst delays, scroll distances, and pauses are drawn from right-skewed distributions matching observed human behavior.
8. **Per-session parameter jitter** — `profile.Jitter()` perturbs all behavior parameters ±15% to prevent anti-bot ML from clustering sessions into discrete archetypes.
9. **Session fatigue model** — warmup slowdown at session start, cruise speed mid-session, gradual fatigue buildup. Per-call noise prevents smooth-curve detection.
10. **Adaptive domain learning** — Bayesian risk scoring learns which domains block static fetches and auto-escalates to browser. Social media preset escalates after 1 block.

## Config

Example config at `config/config.yaml`. All config structs in `config.go`. Supports env var expansion via `os.ExpandEnv`. Defaults applied for all missing values.

Key config additions (v0.0.4+):
```yaml
fetch:
  browser:
    extension_path: ""       # default: auto-download NopeCHA. Set "none" to disable.
    page_reuse_limit: 200    # destroy pooled page after N requests (0 = unlimited)

behavior:
  profile: "careful"          # careful | moderate | aggressive
  jitter: true                # apply ±15% per-session parameter jitter

middleware:
  circuit_breaker:
    enabled: true
    failure_threshold: 0.5
    base_timeout: 30s
    max_timeout: 10m
```

Global CLI flags:
```
--headless MODE   "true" | "false" | "virtual" (overrides config)
-v / -vv          verbose logging
```
