<div align="center">
  <img src="assets/foxhound-banner.webp" alt="Foxhound - Go Scraping Framework" width="600" height="450"/>
</div>

<p align="center">
  <strong>Go Scraping Framework with Native Camoufox Anti-Detection</strong>
</p>

# Foxhound v0.0.17

High-performance Go scraping framework with native Camoufox anti-detection, dual-mode fetching, and 13-layer middleware.

## Highlights

- **Dual-mode fetching**: TLS-impersonating HTTP client (~5-50ms) + Camoufox browser (~500ms-5s), with automatic escalation on block detection
- **Consistent identity profiles**: UA + TLS fingerprint + header order + OS + hardware + screen + locale all match — randomness without consistency causes instant blocks
- **13-layer middleware chain**: concurrency, metrics, rate limit, robots.txt, delta-fetch, dedup, autothrottle, cookies, referer, blocked detector, redirect, depth limit, retry
- **Trail API**: fluent navigation builder with Fill, InfiniteScroll, Evaluate (custom JS), XHR/fetch capture, and optional steps
- **Structured data extraction**: JSON-LD, OpenGraph, NextData, NuxtData extractors + contact deobfuscation (CloudFlare cfemail)
- **NopeCHA auto-download**: CAPTCHA-solving extension fetched and configured automatically at runtime
- **9 export formats**: JSON, JSONL, CSV, Markdown, Text, XML, SQLite, PostgreSQL, Webhook
- **Parsing engine**: HTML table extraction (colspan/rowspan), JS preloaded data (Next.js/Nuxt/Redux), directory listings (JSON-LD/Microdata/DOM), pagination detection, and auto-detection with Readability-style article scoring
- **Adaptive parsing**: CSS pseudo-selectors (`::text`, `::attr`), similarity matching, auto-selector generation + sitemap/RSS/Atom parsing
- **Streaming API**: `Hunt.Stream(ctx)` for real-time item processing via Go channels
- **Checkpoint/resume**: auto-save hunt state every N items
- **Stateful Session**: `foxhound.NewSession(...)` wraps fetcher + cookie jar + identity + proxy for single-call ad-hoc scraping, with cookies persisted across calls
- **Multi-session campaigns**: `Hunt.AddSession(name, cfg)` + `Job.SessionID` route individual jobs through distinct fetchers / identities / proxies inside one Hunt
- **Development mode**: `Hunt.WithDevelopmentMode(dir)` caches responses on disk after the first run and replays them on subsequent runs for zero-network iteration
- **Verified Cloudflare solve**: `fetch.WithSolveCloudflare(timeout)` polls cookie + DOM + token signals before declaring success and exposes `Response.CloudflareSolved`
- **Domain & resource blocking**: `Hunt.WithBlockedDomains(...)` / `Hunt.WithDisableResources(...)` abort ad, tracker, image, and font requests at the browser layer
- **Trail XHR capture**: `Trail.CaptureXHR(pattern)` attaches URL regexps to every produced job so matching XHR/fetch response bodies land in `Response.Captures`
- **18 packages, 1200+ tests**

## Key Capabilities

| Area | What you get |
|------|-------------|
| **Performance** | CSS parsing in ~8ms for 5K elements. Multi-core goroutines with per-domain concurrency control |
| **Anti-detection** | Real Camoufox binary (C++ fingerprint spoofing), human behavior simulation (log-normal timing, Bezier mouse, scroll rhythm), NopeCHA auto-download |
| **Block avoidance** | 9 vendor patterns (Cloudflare, Akamai, DataDome, PerimeterX) with auto-retry + reCAPTCHA checkbox click + Turnstile handler |
| **Identity** | 60+ device profiles with consistent UA + TLS + headers + OS + GPU + screen + locale + geo matching |
| **Trail API** | Fill forms (`JobStepFill`), infinite scroll with container + stop condition, `Evaluate` custom JS, XHR/fetch capture, optional steps, persistent cookies |
| **Parsing** | CSS + XPath + regex + JSON + structured schema + adaptive selectors + similarity matching + pseudo-selectors + sitemap/RSS/Atom |
| **Structured data** | JSON-LD, OpenGraph, NextData, NuxtData extractors + CloudFlare cfemail deobfuscation |
| **Export** | 9 formats: JSON, JSONL, CSV, Markdown (table/list/cards), Text, XML, SQLite, PostgreSQL, Webhook + field-level pipeline transforms |
| **Proxy** | Pool rotation, health checking, cooldown, geo-targeted selection matching identity locale |
| **Queue** | Memory, Redis (distributed), SQLite (persistent) — checkpoint/resume across restarts |
| **Monitoring** | Prometheus metrics + webhook alerting with error/block rate thresholds |
| **Scaling** | `docker compose --scale foxhound=4` with shared Redis queue |

## Quick Start

```bash
git clone https://github.com/sadewadee/foxhound.git
cd foxhound
go build -tags playwright -o foxhound ./cmd/foxhound/
foxhound init myproject && cd myproject
go mod tidy
foxhound run --config config.yaml
```

### Google Maps — Scroll feed, collect businesses, extract contacts

```go
// Generate a consistent identity (UA + TLS + headers + OS all match)
id := identity.Generate(identity.WithBrowser(identity.BrowserFirefox))
profile := behavior.CarefulProfile().Jitter() // ±15% per-session parameter variance

browser, _ := fetch.NewCamoufox(
    fetch.WithBrowserIdentity(id),
    fetch.WithBehaviorProfile(profile),
    fetch.WithStorageState("session.json"), // persist session across runs
)
defer browser.Close()

// SmartFetcher with Bayesian domain learning — auto-escalates to browser when blocked
scorer := fetch.NewDomainScorer(fetch.SocialMediaScoreConfig())
smart := fetch.NewSmart(static, browser, fetch.WithDomainScorer(scorer))

// Trail: search → scroll feed → collect all business URLs
trail := engine.NewTrail("maps-search").
    Navigate("https://www.google.com/maps").
    Fill("input#searchboxinput", "restaurant in bali").
    Click("button#searchbox-searchbutton").
    WaitOptional("div[role='feed']", 10*time.Second).
    InfiniteScrollInUntil("div[role='feed']", "div.Nv2PK", 50, 200).
    Evaluate(`() => document.querySelectorAll('.Nv2PK').length`)

h := engine.NewHunt(engine.HuntConfig{
    Name:            "maps",
    Walkers:         3,
    Seeds:           trail.ToJobs(),
    Fetcher:         middleware.Chain(
        middleware.NewCircuitBreaker(middleware.DefaultCircuitBreakerConfig()),
        middleware.NewAutoThrottle(middleware.AutoThrottleConfig{
            TargetConcurrency: 1, MinDelay: 2 * time.Second, MaxDelay: 15 * time.Second,
        }),
    ).Wrap(smart),
    Queue:           queue.NewReliable(queue.NewMemory(1000), queue.DefaultReliableConfig()),
    BehaviorProfile: profile,
    Processor: foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
        // Auto-detect page type and extract accordingly
        result, _ := parse.AutoExtract(resp)
        if result.Type == parse.ContentListing {
            var items []*foxhound.Item
            for _, l := range result.Listings {
                items = append(items, l.AsItem())
            }
            return &foxhound.Result{Items: items}, nil
        }
        // Fallback: extract contacts from business website
        item := foxhound.NewItem()
        item.Set("url", resp.URL)
        item.Set("emails", parse.ExtractEmails(resp))
        item.Set("phones", parse.ExtractPhones(resp))
        return &foxhound.Result{Items: []*foxhound.Item{item}}, nil
    }),
    Writers: []foxhound.Writer{jsonlWriter},
})
h.Run(context.Background())
```

### Trail API — Login + Search + Infinite Scroll + JS Extract

```go
// Login trail (reusable across sessions with WithStorageState)
login := engine.Login("ig-login",
    "https://www.instagram.com/accounts/login/",
    "input[name='username']", "input[name='password']", "button[type='submit']",
    os.Getenv("IG_USER"), os.Getenv("IG_PASS"),
)

// Feed scraping trail
feed := engine.NewTrail("ig-feed").
    Navigate("https://www.instagram.com/explore/").
    WaitOptional("article", 10*time.Second).
    InfiniteScrollUntil("article", 100, 500).
    Evaluate(`() => {
        const posts = document.querySelectorAll('a[href*="/p/"]');
        return Array.from(posts).map(a => a.href);
    }`)
```

### Auto-Detection — Let foxhound figure out the page type

```go
result, _ := parse.AutoExtract(resp)
switch result.Type {
case parse.ContentArticle:
    fmt.Println(result.Article.Title, result.Article.WordCount, "words")
case parse.ContentListing:
    for _, listing := range result.Listings {
        fmt.Println(listing.Name, listing.Phone, listing.Rating)
    }
case parse.ContentProduct:
    fmt.Println("Product page detected")
}

// Extract preloaded JS data (Next.js, Nuxt, Redux, Apollo)
data, _ := parse.ExtractPreloadedData(resp)
fmt.Println("Framework:", data.Framework) // "nextjs", "nuxt", "react"...

// Detect pagination and follow next pages
links := parse.DetectPagination(resp) // multi-signal scoring (50pt threshold)
for _, link := range links {
    fmt.Println(link.Direction, link.URL, "score:", link.Score)
}
```

## Anti-fragility / Adaptive Selectors

Most scrapers break the moment a target site renames a CSS class. Foxhound's adaptive selectors learn an element signature (tag, classes, text prefix, parent, depth, position) on the first successful match, then fall back to similarity matching when the primary CSS selector stops working — so a class rename, a wrapper-div change, or a sibling reordering does not break extraction.

Enable adaptive mode on a Hunt with `WithAdaptive(savePath)` (pass an empty string for in-memory only, or a JSON path to persist learned signatures across runs), then use the adaptive helpers on `Response`:

```go
hunt := engine.NewHunt(engine.HuntConfig{
    Name:      "shop",
    Fetcher:   fetcher,
    Queue:     q,
    Processor: foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
        // Inline: register and extract in one call. The signature is
        // learned automatically and persisted by the Hunt.
        title := resp.CSSAdaptive("h1.product-title", "title").Text()
        price := resp.CSSAdaptive(".price", "price").Text()

        // On future runs, even if .product-title gets renamed to
        // .item-name, similarity matching will recover the element.
        // Use Adaptive(name) for selectors registered earlier (e.g.
        // via Trail.Adaptive or a previous CSSAdaptive call).
        _ = resp.Adaptive("title")

        item := foxhound.NewItem()
        item.Set("title", title)
        item.Set("price", price)
        return &foxhound.Result{Items: []*foxhound.Item{item}}, nil
    }),
}).WithAdaptive("./adaptive_signatures.json")
```

You can also declare adaptive selectors at the Trail level:

```go
trail := engine.NewTrail("books").
    Navigate("https://books.toscrape.com/").
    Adaptive("book_title", ".product_pod h3 a").
    Adaptive("book_price", ".product_pod .price_color")
```

See [`examples/adaptive/`](examples/adaptive/main.go) for a complete runnable example demonstrating an adaptive selector surviving a CSS class rename.

## Real Scraping Results

| Target | Mode | Items | Block Avoidance | Notes |
|--------|------|-------|-----------------|-------|
| Google Maps (10 queries) | Camoufox + proxy | **100 places** | 100% | 1,297 items/hour, 0 CAPTCHAs |
| Alibaba (yoga mat) | Camoufox + proxy | **10 products** | 100% | Prices + suppliers extracted |
| bot.sannysoft.com | Camoufox | 29/30 PASS | — | webdriver NOT detected |
| CreepJS | Camoufox | Trust: HIGH | — | Fingerprint consistent |

## Benchmarks

Measured on **hachibi** (AMD Ryzen 7 5700G, Docker container, 2 cores / 4GB RAM, Ubuntu 24.04).

### CSS Selection — 5,000 elements

| Library | Language | Time | vs Foxhound |
|---------|----------|------|-------------|
| **Foxhound CSS** | Go | **13.6ms** | **1.0x** |
| Raw goquery | Go | 13.0ms | 0.96x |
| stdlib html | Go | 17.7ms | 1.3x slower |
| Raw lxml | Python/C | 195.8ms | 14.4x slower |
| BeautifulSoup | Python | 245.6ms | 18.1x slower |

### Foxhound Internal Benchmarks (5,000 elements)

| Method | Time | Memory | Allocs | Notes |
|--------|------|--------|--------|-------|
| Foxhound CSS | 13.6ms | 6.5 MB | 100K | <1% overhead vs raw goquery |
| Foxhound Adaptive | 17.3ms | 6.2 MB | 95K | Zero overhead when selector works |
| Foxhound Schema | 31.3ms | 13.3 MB | 320K | 3 fields per item |
| Foxhound TextExtract | 22.5ms | 10.0 MB | 270K | 3 fields per item |
| FindByText | 24.6ms | 12.1 MB | 165K | Full DOM text search |
| Regex extract | 6.7ms | 1.1 MB | 15K | Pattern matching on body |
| Similarity score | **96ns** | **0 B** | **0** | Zero allocation |
| Item.ToJSON | 1.2µs | 432 B | 10 | — |
| Item.ToMarkdown | 716ns | 376 B | 8 | — |

### Scaling by Document Size

| Benchmark | 1K elements | 5K elements | 10K elements | Scaling |
|-----------|-------------|-------------|--------------|---------|
| Foxhound CSS | 2.3ms | 13.6ms | 29.6ms | ~linear |
| Regex extract | 1.5ms | 6.7ms | 15.7ms | ~linear |
| stdlib html | 3.1ms | 17.7ms | 31.4ms | ~linear |

```bash
# Run yourself
go test -bench=. -benchmem ./benchmarks/

# Run in Docker with resource limits
docker run --cpus=2 --memory=4g foxhound-benchmark:latest \
  go test -bench=. -benchmem ./benchmarks/
```

## Documentation

| File | Contents |
|------|----------|
| [docs/getting-started.md](docs/getting-started.md) | Install, first scrape, running modes |
| [docs/configuration.md](docs/configuration.md) | Full config.yaml reference |
| [docs/cli.md](docs/cli.md) | All CLI commands and flags |
| [docs/api.md](docs/api.md) | Go types, interfaces, Hunt/Stream API |
| [docs/anti-detection.md](docs/anti-detection.md) | Identity system, TLS, behavior simulation |
| [docs/parsing.md](docs/parsing.md) | Table, preload, directory, pagination, auto-detection parsers |
| [docs/middleware.md](docs/middleware.md) | All 13 middleware, chain order |
| [docs/pipeline.md](docs/pipeline.md) | Pipeline stages and all 9 export formats |
| [docs/proxy.md](docs/proxy.md) | Proxy pool, rotation, providers, geo matching |
| [docs/browser.md](docs/browser.md) | Camoufox setup, options, human simulation |
| [docs/examples.md](docs/examples.md) | E-commerce, Maps, adaptive parsing, streaming |
| [docs/deployment.md](docs/deployment.md) | Docker, scaling, environment variables |

## Export Formats

| Format | Constructor | Notes |
|--------|-------------|-------|
| JSON array | `export.NewJSON(path, export.JSONArray)` | Single file, full array |
| JSON Lines | `export.NewJSON(path, export.JSONLines)` | One object per line, streaming-friendly |
| CSV | `export.NewCSV(path, cols...)` | Fixed or auto-inferred columns |
| Markdown table | `export.NewMarkdown(path, export.MarkdownTable)` | GFM pipe table |
| Markdown list | `export.NewMarkdown(path, export.MarkdownList)` | Bullet list, first field bolded |
| Markdown cards | `export.NewMarkdown(path, export.MarkdownCards)` | H2 heading + bullet fields |
| Plain text lines | `export.NewText(path, export.TextLines)` | `key=value` per line |
| Plain text pretty | `export.NewText(path, export.TextPretty)` | Labelled blocks with separators |
| XML | `export.NewXML(path, root, item)` | Configurable root/item element names |
| SQLite | `export.NewSQLite(dbPath, table)` | Auto-creates and extends schema |
| PostgreSQL | `export.NewPostgres(dsn, table)` | Upsert support, batch inserts |
| Webhook | `export.NewWebhook(url)` | HTTP POST, optional batch size |

## Architecture

```
Job → rate limit → dedup → behavior timing → header enrichment
  → Smart Fetcher (static TLS or Camoufox browser)
    → Block detection (9 vendor patterns) → retry with backoff
  → Parser (CSS / XPath / JSON / Regex / Adaptive / Similarity)
  → User Process() → Result{Items, NextJobs}
  → Pipeline (validate, clean, dedup) → Writers (9 formats)
  → Queue (memory / Redis / SQLite)
```

## License

MIT
