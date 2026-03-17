# FOXHOUND — Ecosystem & Containerization
### Addendum to Architecture Document
### Version 0.1 Draft — March 2026

---

## 1. Design Philosophy: Batteries-Included

Scrapy ecosystem: 150+ plugins yang user harus cari, evaluate, install, configure,
dan harap compatible satu sama lain. Ini fragile:

```
scrapy-fake-useragent + scrapy-rotating-proxies + scrapy-playwright 
+ scrapy-deltafetch + scrapy-redis + scrapy-impersonate
= 6 packages, 6 maintainers, 6 update cycles, unknown compatibility
```

Foxhound philosophy: semua yang 80% user butuhkan sudah BUILT-IN sebagai
sub-packages. Zero external plugins untuk workflow standar. User cukup:

```go
import "github.com/foxhound-scraper/foxhound"
```

Satu import. Semua modul diakses via konfigurasi, bukan dependency tambahan.

---

## 2. Module Map: Foxhound vs Scrapy Ecosystem

```
┌─────────────────────────────────────────────────────────────────┐
│                     foxhound (root package)                     │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ foxhound/identity — Anti-Detection Stack                │    │
│  │                                                         │    │
│  │  identity/fingerprint   500+ real device profiles       │    │
│  │  identity/useragent     UA database synced with         │    │
│  │                         fingerprint (bukan random!)     │    │
│  │  identity/tls           JA3/JA4 profiles per browser    │    │
│  │  identity/headers       Header order + values per       │    │
│  │                         browser version                 │    │
│  │  identity/geo           GeoIP → timezone, locale,       │    │
│  │                         language auto-matching          │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ foxhound/proxy — Proxy Management                       │    │
│  │                                                         │    │
│  │  proxy/pool             Multi-provider pool manager     │    │
│  │  proxy/rotator          Strategy: per-session,          │    │
│  │                         per-domain, per-request         │    │
│  │  proxy/health           Live health check + scoring     │    │
│  │  proxy/cooldown         Cooldown tracking per proxy     │    │
│  │  proxy/providers        Built-in adapters:              │    │
│  │    /static              File/env list (user:pass@host)  │    │
│  │    /brightdata          Bright Data API                 │    │
│  │    /oxylabs             Oxylabs API                     │    │
│  │    /smartproxy          Smartproxy API                  │    │
│  │    /custom              User-defined provider           │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ foxhound/fetch — Fetcher Layer                          │    │
│  │                                                         │    │
│  │  fetch/stealth          TLS-impersonating HTTP client   │    │
│  │                         (wraps surf/azuretls)           │    │
│  │  fetch/camoufox         Native Camoufox via playwright  │    │
│  │  fetch/smart            Auto-route: static → browser    │    │
│  │                         with block detection & fallback │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ foxhound/behavior — Human Simulation                    │    │
│  │                                                         │    │
│  │  behavior/timing        Log-normal delays, session      │    │
│  │                         rhythm, circadian patterns      │    │
│  │  behavior/mouse         Bezier curves, micro-jitter,    │    │
│  │                         overshoot, idle drift           │    │
│  │  behavior/scroll        Read scroll, quick scan,        │    │
│  │                         deceleration near target        │    │
│  │  behavior/keyboard      Typing speed variation,         │    │
│  │                         typo simulation (optional)      │    │
│  │  behavior/navigation    Entry paths, back button,       │    │
│  │                         "useless" page visits           │    │
│  │  behavior/profiles      Presets: Careful, Moderate,     │    │
│  │                         Aggressive, Custom              │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ foxhound/engine — Core Scraping Engine                  │    │
│  │                                                         │    │
│  │  engine/hunt            Campaign orchestrator           │    │
│  │  engine/trail           Navigation path + steps         │    │
│  │  engine/walker          Virtual user (own session,      │    │
│  │                         identity, behavior)             │    │
│  │  engine/scheduler       Job dispatch + priority         │    │
│  │  engine/retry           Retry policies + backoff        │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ foxhound/middleware — Request/Response Processing        │    │
│  │                                                         │    │
│  │  middleware/ratelimit   Per-domain adaptive throttle     │    │
│  │  middleware/dedup       URL dedup (fingerprint-based)    │    │
│  │  middleware/autothrottle Monitor response time, auto-    │    │
│  │                         adjust speed (Scrapy-style)     │    │
│  │  middleware/robotstxt   robots.txt compliance (optional) │    │
│  │  middleware/redirect    Follow/limit redirects          │    │
│  │  middleware/cookies     Cookie jar per session          │    │
│  │  middleware/referer     Auto-set realistic referer      │    │
│  │  middleware/metrics     Prometheus metrics export       │    │
│  │  middleware/logging     Structured logging (slog)       │    │
│  │  middleware/retry       Retry with strategy selection   │    │
│  │  middleware/depth       Max crawl depth limiter         │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ foxhound/parse — HTML Parsing                           │    │
│  │                                                         │    │
│  │  parse/goquery          CSS selector parsing            │    │
│  │  parse/xpath            XPath parsing (via xmlquery)    │    │
│  │  parse/json             JSON response parsing           │    │
│  │  parse/regex            Regex extraction helpers        │    │
│  │  parse/structured       Schema-based extraction         │    │
│  │                         (define struct, auto-extract)   │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ foxhound/pipeline — Data Processing & Export            │    │
│  │                                                         │    │
│  │  pipeline/validate      Schema validation (drop bad)    │    │
│  │  pipeline/clean         Text cleaning, price parsing,   │    │
│  │                         date normalization              │    │
│  │  pipeline/dedup         Item-level dedup (by key field) │    │
│  │  pipeline/transform     Custom transform functions      │    │
│  │                                                         │    │
│  │  pipeline/export                                        │    │
│  │    /csv                 CSV export                      │    │
│  │    /json                JSON / JSON Lines export        │    │
│  │    /parquet             Parquet (analytics-ready)       │    │
│  │    /postgres            PostgreSQL direct insert        │    │
│  │    /sqlite              SQLite (single-file DB)         │    │
│  │    /s3                  AWS S3 / R2 / MinIO upload      │    │
│  │    /webhook             HTTP POST per-item or batch     │    │
│  │    /custom              User-defined writer interface   │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ foxhound/queue — Job Queue Backends                     │    │
│  │                                                         │    │
│  │  queue/memory           In-memory (single process)      │    │
│  │  queue/redis            Redis (distributed, resumable)  │    │
│  │  queue/postgres         PostgreSQL (persistent, ACID)   │    │
│  │  queue/sqlite           SQLite (single-file, resumable) │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ foxhound/cache — Response Caching                       │    │
│  │                                                         │    │
│  │  cache/memory           In-memory LRU                   │    │
│  │  cache/file             Disk-based (hash filenames)     │    │
│  │  cache/redis            Redis (shared across workers)   │    │
│  │  cache/sqlite           SQLite (single-file)            │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ foxhound/monitor — Observability                        │    │
│  │                                                         │    │
│  │  monitor/stats          Built-in stats collector        │    │
│  │                         (requests, items, errors, etc)  │    │
│  │  monitor/prometheus     Prometheus metrics endpoint     │    │
│  │  monitor/alerting       Slack/Discord/Webhook alerts    │    │
│  │                         (error rate threshold, etc)     │    │
│  │  monitor/dashboard      Built-in web UI (optional)      │    │
│  │                         status, live stats, trail viz   │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ foxhound/captcha — CAPTCHA Handling (opt-in)            │    │
│  │                                                         │    │
│  │  captcha/detect         Auto-detect CAPTCHA page        │    │
│  │  captcha/capsolver      CapSolver API integration       │    │
│  │  captcha/twocaptcha     2captcha API integration        │    │
│  │  captcha/turnstile      Cloudflare Turnstile handler    │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ foxhound/cmd — CLI Tools                                │    │
│  │                                                         │    │
│  │  foxhound init          Scaffold new project            │    │
│  │  foxhound run           Run a hunt                      │    │
│  │  foxhound shell         Interactive scraping shell      │    │
│  │  foxhound check         Test fingerprint + TLS          │    │
│  │  foxhound proxy-test    Test proxy pool health          │    │
│  │  foxhound resume        Resume interrupted hunt         │    │
│  │  foxhound docker        Generate Dockerfile             │    │
│  └─────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
```

---

## 3. Module Detail: identity

### Mengapa BUKAN "fake-useragent"

Scrapy `fake-useragent` ambil random UA dari database. Masalahnya:

```
Request 1: User-Agent: Chrome/120 Windows
  → TLS fingerprint: Go net/http (bukan Chrome) → MISMATCH → blocked

Request 2: User-Agent: Safari/17 macOS  
  → TLS fingerprint: Go net/http (bukan Safari) → MISMATCH → blocked
```

Random UA tanpa matching TLS + header order + fingerprint = WORSE than no rotation.
Anti-bot cross-check: kalau UA bilang Chrome tapi TLS bilang bukan Chrome, instant block.

Foxhound `identity` module generate COMPLETE identity yang consistent:

```go
// identity.Profile — semua attribute consistent satu sama lain
type Profile struct {
    // Browser
    UA          string   // "Mozilla/5.0 ... Firefox/148.0"
    BrowserName string   // "firefox"  
    BrowserVer  string   // "148.0"
    
    // TLS (JA3/JA4 fingerprint yang MATCH browser ini)
    TLSProfile  string   // "firefox_148" → lookup exact JA3/JA4/HTTP2
    
    // Header ordering (setiap browser punya order berbeda)
    HeaderOrder []string // ["Host","User-Agent","Accept","Accept-Language",...]
    
    // OS
    OS          string   // "windows"
    OSVersion   string   // "10.0"
    Platform    string   // "Win32"
    
    // Hardware (consistent dengan OS)
    Cores       int      // 8
    Memory      float64  // 8 (GB)
    GPU         string   // "Intel(R) UHD Graphics 630"
    
    // Screen (common resolution for this OS)
    ScreenW     int      // 1920
    ScreenH     int      // 1080
    ColorDepth  int      // 24
    PixelRatio  float64  // 1.0 (Mac = 2.0)
    
    // Locale (match proxy geo)
    Languages   []string // ["en-US", "en"]
    Timezone    string   // "America/New_York"
    Locale      string   // "en-US"
    
    // Geo (derived from proxy IP)
    Lat         float64  // 40.7128
    Lng         float64  // -74.0060
    
    // Camoufox config (auto-generated)
    CamoufoxEnv map[string]string // CAMOU_CONFIG_* vars
}
```

### Usage

```go
// Generate identity yang match proxy
proxy := proxy.Get() // returns proxy with known geo

id := identity.Generate(
    identity.WithProxy(proxy),     // auto-match geo → locale → tz
    identity.WithBrowser("firefox"), // atau "chrome" untuk TLS client
    identity.WithOS("windows"),    // atau identity.RandomOS()
)

// Semua attribute guaranteed consistent:
// - UA match browser + OS
// - TLS profile match browser version
// - Header order match browser
// - GPU match OS
// - Screen resolution common untuk OS
// - Timezone match proxy geo
// - Languages match locale

// Untuk TLS client (static fetch):
resp, _ := fetch.Stealth(ctx, url, fetch.WithIdentity(id))

// Untuk Camoufox (browser fetch):
browser, _ := fetch.Camoufox(ctx, fetch.WithIdentity(id))
```

### Database

```
identity/data/
  profiles/
    firefox_148_windows.json    // 50+ screen/hw combinations
    firefox_148_macos.json
    firefox_148_linux.json
    chrome_131_windows.json     // untuk TLS client impersonation
    chrome_131_macos.json
    chrome_131_linux.json
  tls/
    firefox_148.json            // JA3, JA4, HTTP/2 settings
    chrome_131.json
  headers/
    firefox_148.json            // header name ordering
    chrome_131.json
  geo/
    maxmind_lite.mmdb           // GeoIP database (free tier)
```

Database di-embed via Go `embed` directive — nggak perlu download terpisah.
Update database = update Foxhound version.

---

## 4. Module Detail: proxy

### Pool Manager

```go
pool := proxy.NewPool(
    // Multiple providers sekaligus
    proxy.Static([]string{
        "http://user:pass@residential1.com:8080",
        "socks5://user:pass@residential2.com:1080",
    }),
    proxy.BrightData("api_key", proxy.Residential, proxy.Country("US")),
    proxy.Oxylabs("api_key", proxy.ISP),
)

// Configuration
pool.SetRotation(proxy.PerSession)     // rotate per Walker session
pool.SetCooldown(30 * time.Minute)     // proxy rest setelah dipakai
pool.SetMaxRequests(100)               // max requests per proxy per session
pool.SetHealthCheck(60 * time.Second)  // check interval
pool.SetGeoPreference("US", "GB")     // prefer proxies dari negara ini
```

### Rotation Strategies

```go
const (
    PerRequest  RotationStrategy = iota // new proxy setiap request (mahal, suspicious)
    PerSession                          // new proxy per Walker session (recommended)
    PerDomain                           // satu proxy per target domain (sticky)
    OnBlock                             // rotate hanya kalau kena block
    Manual                              // user control
)
```

### Health Check

```go
// Proxy health = composite score
type ProxyHealth struct {
    Alive       bool          // responds to test request?
    Latency     time.Duration // avg response time
    SuccessRate float64       // % requests non-blocked (last 100)
    BlockRate   float64       // % requests blocked
    BanCount    int           // times banned from target domain
    CooldownUntil time.Time   // jangan pakai sampai waktu ini
    Score       float64       // weighted composite: 0.0 (dead) → 1.0 (perfect)
}

// Pool auto-rank proxy by score, prioritize highest score
// Low score → moved to cooldown → retry later
// Zero score (dead) → removed from pool
```

---

## 5. Module Detail: middleware

### AutoThrottle (ported dari Scrapy concept)

Scrapy punya auto-throttle yang brilliant. Foxhound implement versi Go-nya
yang juga integrate dengan behavior simulation:

```go
// AutoThrottle monitor response time dan adjust delay
type AutoThrottle struct {
    TargetConcurrency float64       // target concurrent requests per domain
    InitialDelay      time.Duration // starting delay (default 1s)
    MinDelay          time.Duration // minimum delay (default 0.5s)  
    MaxDelay          time.Duration // maximum delay (default 60s)
    
    // Per-domain state
    domains map[string]*domainState
}

type domainState struct {
    avgLatency    time.Duration // exponential moving average
    currentDelay  time.Duration // current inter-request delay
    lastRequest   time.Time
    responseCount int64
    errorCount    int64
}

// Logic:
// - Server responds fast (200ms) → decrease delay toward MinDelay
// - Server responds slow (5s) → increase delay toward MaxDelay  
// - Server returns 429/503 → spike delay to MaxDelay
// - Delay calculated: avgLatency / TargetConcurrency
//
// Ini simultaneously:
// 1. Polite (nggak overwhelm server)
// 2. Stealthy (timing nggak uniform — adapts to server load)
// 3. Efficient (fast when server allows, slow when needed)
```

### Dedup

```go
// DedupMiddleware — multiple strategies
type DedupMiddleware struct {
    Strategy DedupStrategy
    store    DedupStore
}

const (
    URLDedup          DedupStrategy = iota // exact URL match
    URLCanonicalDedup                      // normalize URL (remove tracking params, sort query)
    ContentHashDedup                       // hash response body (different URL same content)
)

// DedupStore backends
type DedupStore interface {
    Seen(key string) bool
    Mark(key string) error
    Count() int64
}

// Implementations:
// dedup.MemoryStore   — in-memory map (fast, lost on restart)
// dedup.RedisStore    — Redis SET (shared across workers, persistent)
// dedup.SQLiteStore   — SQLite (single-file, persistent)
// dedup.BloomStore    — Bloom filter (memory-efficient, ~1% false positive)
```

### DeltaFetch (equivalent scrapy-deltafetch)

```go
// DeltaFetch — skip URLs yang sudah di-scrape di previous run
// Persist across runs, nggak scrape page yang nggak berubah

type DeltaFetch struct {
    store    DeltaStore  // SQLite/Redis
    strategy DeltaStrategy
}

const (
    DeltaSkipSeen    DeltaStrategy = iota // skip kalau pernah fetch
    DeltaSkipSameETag                      // skip kalau ETag sama
    DeltaSkipSameHash                      // skip kalau content hash sama
    DeltaSkipRecent                        // skip kalau di-fetch < N jam lalu
)
```

---

## 6. Module Detail: pipeline

Foxhound pipeline = chain of processors, mirip Scrapy tapi type-safe:

```go
// Pipeline interface
type Pipeline interface {
    Process(ctx context.Context, item *Item) (*Item, error)
    // Return nil item = drop (equivalent Scrapy DropItem)
    // Return error = log error, continue
}

// Built-in pipelines
type ValidatePipeline struct {
    RequiredFields []string
}

type CleanPipeline struct {
    TrimWhitespace bool
    NormalizePrice  bool   // "$1,234.56" → 1234.56
    NormalizeDate   bool   // "March 18, 2026" → "2026-03-18"
    StripHTML       bool
}

type DedupPipeline struct {
    KeyField string     // field yang dijadikan unique key
    Store    DedupStore
}

// Chain configuration
hunt := &foxhound.Hunt{
    Pipeline: foxhound.Chain(
        &pipeline.Validate{Required: []string{"name", "price"}},
        &pipeline.Clean{TrimWhitespace: true, NormalizePrice: true},
        &pipeline.Dedup{KeyField: "sku"},
        // Export ke multiple destinations simultaneously
        &pipeline.Export{
            Writers: []pipeline.Writer{
                export.JSON("output/products.jsonl", export.JSONLines),
                export.CSV("output/products.csv"),
                export.Postgres(dbURL, "products"),
                export.Webhook("https://api.myapp.com/ingest", export.BatchSize(50)),
            },
        },
    ),
}
```

### Export Formats

```go
// JSON / JSON Lines
export.JSON("file.json")           // single JSON array
export.JSON("file.jsonl", JSONLines) // one JSON per line (streaming)

// CSV
export.CSV("file.csv")
export.CSV("file.csv", WithHeaders("Name", "Price", "URL"))

// Parquet (analytics-ready, columnar)
export.Parquet("file.parquet")

// PostgreSQL
export.Postgres(connString, "table_name",
    WithUpsert("sku"),           // upsert on conflict
    WithBatchSize(100),          // batch insert
)

// SQLite
export.SQLite("data.db", "products")

// S3 / R2 / MinIO
export.S3("bucket/path/", 
    WithRegion("us-east-1"),
    WithFormat(JSONLines),
    WithPartition(Daily),        // daily file partitioning
)

// Webhook
export.Webhook("https://api.example.com/data",
    WithBatchSize(50),           // send 50 items per POST
    WithRetry(3),
    WithAuth("Bearer", token),
)
```

---

## 7. Module Detail: cmd (CLI)

```bash
# Scaffold new project
$ foxhound init myproject
Created myproject/
├── main.go          # entry point
├── hunts/
│   └── example.go   # example hunt definition
├── targets/
│   └── product.go   # example extract function
├── config.yaml      # foxhound configuration
├── Dockerfile       # production-ready container
├── docker-compose.yml
├── .env.example     # proxy credentials, etc
└── go.mod

# Run a hunt
$ foxhound run --config config.yaml --hunt shop-products

# Interactive shell (test selectors, identities, proxies)
$ foxhound shell
foxhound> identity.generate(os="windows", browser="firefox")
  UA: Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:148.0)...
  TLS: firefox_148 (JA4: t13d1517h2_...)
  Screen: 1920x1080
  Timezone: America/New_York
  
foxhound> fetch("https://example.com")
  Status: 200
  Body: 14,523 bytes
  Fetcher: stealth (TLS client)
  Duration: 45ms

foxhound> fetch("https://protected-site.com")
  Status: 200
  Body: 89,234 bytes
  Fetcher: camoufox (auto-escalated from stealth)
  Duration: 2,341ms

# Test fingerprint + TLS against detection sites
$ foxhound check
  ✓ TLS Fingerprint: matches Firefox 148 (JA4: t13d1517h2_...)
  ✓ HTTP/2 Fingerprint: matches Firefox 148
  ✓ Header Order: correct for Firefox
  ✓ Browser Fingerprint (Camoufox): no leaks detected
  ✓ WebDriver: hidden
  ✓ CreepJS: no inconsistencies
  ✗ Proxy: 2/10 proxies unhealthy (rotated out)

# Test proxy pool
$ foxhound proxy-test --config config.yaml
  Testing 50 proxies...
  ✓ 43 healthy (avg latency: 320ms)
  ✗ 5 slow (>2s latency)
  ✗ 2 dead (connection refused)
  Score: 86/100

# Resume interrupted hunt
$ foxhound resume --hunt-id abc123 --queue redis://localhost:6379

# Generate optimized Dockerfile
$ foxhound docker --target production
  Generated Dockerfile (multi-stage, 487MB final image)
```

---

## 8. Containerization: First-Class Docker Support

### 8.1 Dockerfile (auto-generated, production-optimized)

```dockerfile
# ============================================
# Foxhound Production Dockerfile
# Multi-stage build, minimal final image
# ============================================

# Stage 1: Build Go binary
FROM golang:1.23-bookworm AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /foxhound ./cmd/main.go

# Stage 2: Install Camoufox + Playwright driver
FROM ubuntu:24.04 AS browser

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    python3 \
    python3-pip \
    xvfb \
    fonts-liberation \
    fonts-noto-cjk \
    libasound2t64 \
    libatk1.0-0t64 \
    libcairo2 \
    libcups2t64 \
    libdbus-glib-1-2 \
    libgdk-pixbuf-2.0-0 \
    libgtk-3-0t64 \
    libnspr4 \
    libnss3 \
    libpango-1.0-0 \
    libx11-xcb1 \
    libxcomposite1 \
    libxdamage1 \
    libxrandr2 \
    && rm -rf /var/lib/apt/lists/*

# Install Camoufox binary
RUN pip3 install --break-system-packages camoufox \
    && python3 -m camoufox fetch

# Install Playwright driver (for playwright-go)
COPY --from=builder /build/go.mod /tmp/go.mod
RUN PWGO_VER=$(grep -oE "playwright-go v\S+" /tmp/go.mod | sed 's/playwright-go //g') \
    && apt-get update && apt-get install -y golang \
    && go install github.com/playwright-community/playwright-go/cmd/playwright@${PWGO_VER} \
    && playwright install --with-deps firefox \
    && apt-get purge -y golang \
    && rm -rf /var/lib/apt/lists/*

# Stage 3: Final production image
FROM ubuntu:24.04

# Runtime dependencies only (no build tools)
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    xvfb \
    fonts-liberation \
    fonts-noto-cjk \
    libasound2t64 \
    libatk1.0-0t64 \
    libcairo2 \
    libcups2t64 \
    libdbus-glib-1-2 \
    libgdk-pixbuf-2.0-0 \
    libgtk-3-0t64 \
    libnspr4 \
    libnss3 \
    libpango-1.0-0 \
    libx11-xcb1 \
    libxcomposite1 \
    libxdamage1 \
    libxrandr2 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -r foxhound && useradd -r -g foxhound foxhound

# Copy binaries
COPY --from=builder /foxhound /usr/local/bin/foxhound
COPY --from=browser /root/.cache/camoufox /home/foxhound/.cache/camoufox
COPY --from=browser /root/.cache/ms-playwright /home/foxhound/.cache/ms-playwright

# Set permissions
RUN chown -R foxhound:foxhound /home/foxhound

# SHM size penting untuk browser stability
VOLUME /dev/shm

# Non-root user
USER foxhound
WORKDIR /home/foxhound

# Virtual display untuk headless Camoufox
ENV DISPLAY=:99
ENV FOXHOUND_DATA_DIR=/data
ENV PLAYWRIGHT_BROWSERS_PATH=/home/foxhound/.cache/ms-playwright

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
    CMD foxhound health || exit 1

# Entrypoint: start Xvfb + foxhound
ENTRYPOINT ["sh", "-c", "Xvfb :99 -screen 0 1920x1080x24 -nolisten tcp & foxhound run"]
```

### 8.2 Docker Compose (full stack)

```yaml
version: "3.8"

services:
  # ═══════════════════════════════════════════
  # Foxhound worker (scalable)
  # ═══════════════════════════════════════════
  foxhound:
    build: .
    environment:
      - FOXHOUND_CONFIG=/config/config.yaml
      - FOXHOUND_QUEUE=redis://redis:6379/0
      - FOXHOUND_CACHE=redis://redis:6379/1
      - FOXHOUND_EXPORT_DB=postgres://fox:hunt@postgres:5432/foxhound
      - FOXHOUND_METRICS=:9090
      - FOXHOUND_WALKERS=3
      - FOXHOUND_LOG_LEVEL=info
    volumes:
      - ./config:/config:ro
      - ./output:/data/output
      - /dev/shm:/dev/shm  # shared memory untuk browser
    depends_on:
      redis:
        condition: service_healthy
      postgres:
        condition: service_healthy
    deploy:
      resources:
        limits:
          memory: 4G
          cpus: "2.0"
        reservations:
          memory: 2G
          cpus: "1.0"
    # Scale horizontally: docker compose up --scale foxhound=3
    restart: unless-stopped

  # ═══════════════════════════════════════════
  # Redis (queue + cache + dedup)
  # ═══════════════════════════════════════════
  redis:
    image: redis:7-alpine
    command: redis-server --maxmemory 512mb --maxmemory-policy allkeys-lru
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 3s
      retries: 3
    deploy:
      resources:
        limits:
          memory: 512M

  # ═══════════════════════════════════════════
  # PostgreSQL (results + hunt state)
  # ═══════════════════════════════════════════
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: fox
      POSTGRES_PASSWORD: hunt
      POSTGRES_DB: foxhound
    ports:
      - "5432:5432"
    volumes:
      - pg-data:/var/lib/postgresql/data
      - ./config/init.sql:/docker-entrypoint-initdb.d/init.sql:ro
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U fox -d foxhound"]
      interval: 10s
      timeout: 3s
      retries: 3
    deploy:
      resources:
        limits:
          memory: 512M

  # ═══════════════════════════════════════════
  # Prometheus (metrics collection)
  # ═══════════════════════════════════════════
  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9090:9090"
    volumes:
      - ./config/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prom-data:/prometheus
    profiles:
      - monitoring  # optional: docker compose --profile monitoring up

  # ═══════════════════════════════════════════
  # Grafana (dashboard)
  # ═══════════════════════════════════════════
  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=foxhound
    volumes:
      - grafana-data:/var/lib/grafana
      - ./config/grafana/dashboards:/etc/grafana/provisioning/dashboards:ro
      - ./config/grafana/datasources:/etc/grafana/provisioning/datasources:ro
    profiles:
      - monitoring

volumes:
  redis-data:
  pg-data:
  prom-data:
  grafana-data:
```

### 8.3 config.yaml (single file, semua config)

```yaml
# foxhound.yaml — complete configuration

hunt:
  domain: "shop.com"
  walkers: 3
  
identity:
  browser: "firefox"
  os: ["windows", "macos"]     # random pick per session
  fingerprint_db: "embedded"    # atau path ke custom DB
  
proxy:
  providers:
    - type: static
      list:
        - "http://user:pass@proxy1.com:8080"
        - "socks5://user:pass@proxy2.com:1080"
    # - type: brightdata
    #   api_key: "${BRIGHTDATA_API_KEY}"
    #   product: residential
    #   country: US
  rotation: per_session
  cooldown: 30m
  max_requests_per_proxy: 100
  health_check_interval: 60s

behavior:
  profile: moderate              # careful | moderate | aggressive | custom
  # Custom overrides:
  # session_duration: { min: 15m, max: 45m }
  # pages_per_session: { min: 10, max: 40 }
  # timing: { mu: 1.0, sigma: 0.8 }

fetch:
  static:
    timeout: 30s
    max_idle_conns: 100
    tls_impersonate: true        # match browser TLS fingerprint
  browser:
    timeout: 60s
    block_images: true           # save bandwidth + proxy cost
    block_webrtc: true
    headless: virtual            # Xvfb virtual display (recommended)
    instances: 3                 # pre-spawn N Camoufox instances

middleware:
  autothrottle:
    enabled: true
    target_concurrency: 2.0
    initial_delay: 1s
    min_delay: 0.5s
    max_delay: 30s
  dedup:
    strategy: url_canonical
    store: redis
  deltafetch:
    enabled: true
    strategy: skip_recent
    ttl: 24h                     # re-scrape after 24 hours
  robots_txt:
    enabled: false               # respect robots.txt? (default false)
  depth_limit:
    max: 10

pipeline:
  - validate:
      required: ["name", "price"]
  - clean:
      trim_whitespace: true
      normalize_price: true
  - dedup:
      key: "sku"
  - export:
      - type: jsonl
        path: "/data/output/products.jsonl"
      - type: postgres
        table: "products"
        upsert_key: "sku"
        batch_size: 100
      # - type: s3
      #   bucket: "my-data-bucket"
      #   path: "scrapes/products/"
      #   partition: daily
      # - type: webhook
      #   url: "https://api.myapp.com/ingest"
      #   batch_size: 50

queue:
  backend: redis                 # memory | redis | postgres | sqlite
  # resume: true                 # enable resume on restart

cache:
  backend: redis                 # memory | file | redis | sqlite
  ttl: 1h

monitor:
  metrics:
    enabled: true
    port: 9090
  alerting:
    # - type: slack
    #   webhook: "${SLACK_WEBHOOK}"
    #   on: [error_rate > 20%, hunt_complete, hunt_failed]
  dashboard:
    enabled: false
    port: 8080

logging:
  level: info                    # debug | info | warn | error
  format: json                   # json | text
  output: stderr
```

### 8.4 Scaling Patterns

```yaml
# ═══════ Pattern 1: Single Node (dev/small) ═══════
# docker compose up
# 1 worker, 3 walkers, in-memory queue
# RAM: ~2GB, CPU: 2 cores
# Throughput: 5-20 pages/min (protected sites)

# ═══════ Pattern 2: Multi-Worker (production) ═══════
# docker compose up --scale foxhound=4
# 4 workers × 3 walkers = 12 virtual users
# Shared Redis queue, all workers consume from same queue
# RAM: ~8GB total, CPU: 8 cores
# Throughput: 20-80 pages/min (protected sites)

# ═══════ Pattern 3: Kubernetes (scale-out) ═══════
# Deploy foxhound as Deployment, Redis + PG as StatefulSet
# HPA: scale based on queue depth metric
# Each pod = 1 foxhound worker with N walkers
# External proxy provider (Bright Data/Oxylabs API)
```

### 8.5 Lightweight Mode (no browser, no Docker)

Nggak semua use case butuh Camoufox. Untuk static sites:

```bash
# Single binary, no browser dependency, no Docker needed
$ FOXHOUND_MODE=static foxhound run --config config.yaml

# Atau di Go code:
engine := foxhound.New(
    foxhound.StaticOnly(),  // no browser, no Camoufox
    // TLS impersonation still active (surf/azuretls)
    // Semua middleware still active
    // Hanya browser fetcher yang disabled
)
```

Image size: ~30MB Go binary + ~10MB embedded data. Bisa deploy dimana aja
tanpa Docker.

---

## 9. Scrapy Ecosystem Mapping

Setiap Scrapy plugin yang populer, dan equivalent Foxhound built-in:

```
Scrapy Plugin                    │ Foxhound Built-in
─────────────────────────────────┼─────────────────────────────────
scrapy-fake-useragent            │ identity/useragent (+ TLS match)
scrapy-rotating-proxies          │ proxy/rotator + proxy/health
scrapy-crawlera (Zyte proxy)     │ proxy/providers/brightdata + oxylabs
scrapy-playwright                │ fetch/camoufox (native)
scrapy-splash                    │ fetch/camoufox (superset)
scrapy-impersonate               │ fetch/stealth (TLS impersonation)
scrapy-deltafetch                │ middleware/deltafetch
scrapy-crawl-once                │ middleware/dedup
scrapy-redis                     │ queue/redis
scrapy-s3pipeline                │ pipeline/export/s3
scrapy-jsonschema                │ pipeline/validate
scrapy-magicfields               │ pipeline/transform
scrapy-pagestorage               │ cache/file
scrapy-statsd                    │ monitor/prometheus
spidermon                        │ monitor/alerting
scrapy-autoextract               │ (future: parse/ai)
scrapy-feedexporter-sftp         │ pipeline/export/custom (interface)
scrapy-random-useragent          │ identity/useragent
scrapy-proxies                   │ proxy/pool
scrapy-querycleaner              │ middleware/dedup (canonical URL)
scrapy-delayed-requests          │ behavior/timing
scrapy-html-storage              │ cache/file
scrapy-settings-log              │ monitor/stats

NO Scrapy equivalent:
  ❌ identity/fingerprint         (complete device profile, consistent)
  ❌ identity/tls                 (JA3/JA4 profiles per browser)
  ❌ identity/geo                 (GeoIP auto-matching)
  ❌ identity/headers             (browser-specific header ordering)
  ❌ behavior/mouse               (human mouse simulation)
  ❌ behavior/scroll              (human scroll simulation)
  ❌ behavior/keyboard            (human typing simulation)
  ❌ behavior/navigation          (realistic browsing patterns)
  ❌ engine/hunt                  (campaign orchestration)
  ❌ engine/trail                 (declarative navigation paths)
  ❌ engine/walker                (virtual user with own identity)
  ❌ middleware/autothrottle      (Scrapy has this, but foxhound
                                  version integrates with behavior)
  ❌ fetch/smart                  (auto-escalation static → browser)
```

Total: Foxhound built-in covers 22 Scrapy plugins + adds 13 features
yang nggak ada equivalent di Scrapy ecosystem sama sekali.

---

## 10. Updated Implementation Priorities

### Phase 1: Core + Identity + Container (Week 1-4)
```
Priority modules:
  ✓ foxhound/engine        (hunt, trail, walker, scheduler)
  ✓ foxhound/identity      (fingerprint, useragent, tls, headers, geo)
  ✓ foxhound/fetch         (stealth TLS client, camoufox launcher, smart router)
  ✓ foxhound/proxy         (pool, rotator, health, static provider)
  ✓ foxhound/parse         (goquery, json)
  ✓ foxhound/pipeline      (validate, export/json, export/csv)
  ✓ foxhound/queue         (memory)
  ✓ foxhound/cmd           (init, run)
  ✓ Dockerfile             (multi-stage, production-ready)
  ✓ docker-compose.yml     (foxhound + redis + postgres)
```

### Phase 2: Behavior + Middleware (Week 4-6)
```
  ✓ foxhound/behavior      (timing, mouse, scroll, navigation, profiles)
  ✓ foxhound/middleware     (autothrottle, dedup, ratelimit, cookies, referer)
  ✓ foxhound/cache          (memory, redis)
```

### Phase 3: Production Polish (Week 6-8)
```
  ✓ foxhound/pipeline      (clean, dedup, postgres, s3, webhook, parquet)
  ✓ foxhound/queue          (redis, sqlite)
  ✓ foxhound/middleware     (deltafetch, depth, robotstxt, redirect, metrics)
  ✓ foxhound/monitor        (stats, prometheus, alerting)
  ✓ foxhound/cmd            (shell, check, proxy-test, resume, docker)
  ✓ foxhound/proxy          (brightdata, oxylabs providers)
```

### Phase 4: Ecosystem (Week 8+)
```
  ✓ foxhound/captcha        (detect, capsolver, twocaptcha, turnstile)
  ✓ foxhound/parse          (xpath, regex, structured)
  ✓ foxhound/monitor        (dashboard web UI)
  ✓ Documentation site
  ✓ Example projects (e-commerce, travel, real estate)
  ✓ Kubernetes Helm chart
```
