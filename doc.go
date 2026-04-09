// Package foxhound is a high-performance Go web scraping framework with native
// anti-detection built on Camoufox, a Firefox fork designed to evade bot fingerprinting.
//
// Foxhound is a scraping framework for Go — it handles the full lifecycle of web
// data extraction: fetching pages (with or without a real browser), navigating
// JavaScript-heavy sites, solving CAPTCHAs, rotating identities and proxies,
// extracting structured data, and exporting results. Think of it as Scrapy for Go,
// but with first-class browser automation and anti-detection built in from day one.
//
// # Why Foxhound
//
// Modern websites deploy increasingly sophisticated bot detection: TLS fingerprinting,
// JavaScript challenges (Cloudflare, DataDome, PerimeterX), canvas/WebGL fingerprint
// checks, and behavioral analysis. Traditional HTTP-only scrapers fail silently against
// these defenses. Headless Chrome is widely fingerprinted. Foxhound solves this by
// combining two fetching strategies behind a single API:
//
//   - A TLS-impersonating HTTP client for static pages (~5-50ms per request)
//   - A Camoufox browser (Firefox fork) via playwright-go for JS-heavy and
//     protected pages (~500ms-5s per request)
//
// The smart router starts with the fast static client and automatically escalates to
// the full browser when it detects blocks (403, 429, 503, CAPTCHA pages). This means
// you get HTTP-client speed on easy targets and browser-level evasion on hard ones,
// without changing your code.
//
// # Architecture Overview
//
// Foxhound is organized around five core concepts:
//
// Hunt is the top-level campaign orchestrator. It owns the queue, spawns Walker
// goroutines, collects stats, and coordinates shutdown. You configure a Hunt with
// seed URLs, a Processor (your extraction logic), middleware, pipelines, and writers.
//
// Trail is a fluent navigation path builder. It chains browser actions — Navigate,
// Click, Fill, Wait, Scroll, InfiniteScroll, Evaluate (custom JS), and CaptureXHR —
// into a reusable sequence that gets compiled into Jobs. Trails describe what a human
// would do on the page.
//
// Walker is a goroutine that acts as a virtual user. Each Walker pops Jobs from the
// queue, fetches pages through the middleware chain, runs your Processor, writes
// extracted Items through the pipeline, and enqueues discovered follow-up Jobs. A Hunt
// runs multiple Walkers concurrently.
//
// Job is the unit of work: a URL plus fetch mode, priority, browser steps, metadata,
// and optional session routing. Jobs flow through the queue and are consumed by Walkers.
//
// Session is a stateful client that wraps a fetcher, cookie jar, identity profile,
// and proxy into a reusable unit. Use it standalone for ad-hoc scraping, or register
// multiple Sessions with a Hunt via Hunt.AddSession to route different Jobs through
// different identities and proxies.
//
// # Dual-Mode Fetching
//
// Every request flows through a middleware chain before reaching the fetcher:
//
//	Job → middleware (rate limit → dedup → autothrottle → cookies → referer → retry)
//	  → Smart Fetcher (static or browser) → Browser Steps → Parser → Processor
//	  → Result{Items, Jobs} → Pipeline (validate → clean → dedup → transform)
//	  → Writers (CSV/JSON/SQLite/Webhook) + Queue (new jobs)
//
// The static fetcher (fetch.NewStealth) uses Go's HTTP client with precise header
// ordering and TLS fingerprints matched to the identity profile. The browser fetcher
// (fetch.NewCamoufox) launches a real Camoufox browser instance via the Juggler
// protocol (Firefox's native remote protocol, less targeted by anti-bot than CDP).
// The smart fetcher (fetch.NewSmart) wraps both and auto-escalates based on response
// signals and Bayesian domain risk scoring.
//
// # Identity System
//
// Every request uses a complete, internally consistent identity profile: user agent,
// TLS fingerprint, header order, OS, hardware specs, screen dimensions, locale,
// timezone, and geolocation all match. Randomness without consistency is the number
// one cause of bot detection — a Windows UA with a macOS font list, or a US locale
// with a Tokyo timezone, triggers instant blocks.
//
// Foxhound ships 60 embedded device profiles. The identity package generates
// profiles with functional options:
//
//	id := identity.Generate(
//	    identity.WithBrowser(identity.BrowserFirefox),
//	    identity.WithOS(identity.OSWindows),
//	)
//
// When using Camoufox, the identity is serialized to a JSON config that sets
// navigator properties, WebGL vendor/renderer, canvas noise, OS-specific fonts,
// screen dimensions, and timezone at the C++ level inside the browser — not via
// JavaScript injection that anti-bot scripts can detect.
//
// # Human Behavior Simulation
//
// Foxhound models human behavior using statistical distributions observed from real
// user sessions:
//
//   - Timing uses Weibull and Gamma distributions (right-skewed, matching human
//     reaction times), not uniform random
//   - Mouse movements follow Bezier curves with natural acceleration/deceleration
//   - Scroll patterns simulate reading speed with variable pause durations
//   - Keyboard input uses per-key timing with realistic inter-keystroke intervals
//   - Session fatigue: warmup slowdown at start, cruise speed mid-session, gradual
//     fatigue buildup — with per-call noise to prevent smooth-curve detection
//   - Per-session jitter: all behavior parameters are perturbed ±15% to prevent
//     anti-bot ML from clustering sessions into discrete archetypes
//
// Three built-in profiles ("careful", "moderate", "aggressive") control the
// overall pacing. Configure via BehaviorConfig or Hunt options.
//
// # NopeCHA CAPTCHA Solving
//
// The NopeCHA browser extension is automatically downloaded from GitHub and loaded
// into Camoufox on first launch. It solves reCAPTCHA, hCAPTCHA, and Cloudflare
// Turnstile challenges without API keys. The extension is cached at
// ~/.cache/foxhound/extensions/nopecha/ and updated automatically.
//
// The design philosophy: the goal is to never trigger a CAPTCHA. If one appears,
// earlier layers (identity, timing, proxy rotation) failed. NopeCHA is the safety
// net, not the primary strategy.
//
// Disable with extension_path: "none" in config or WithExtensionPath("none").
//
// # Middleware Chain
//
// Foxhound provides 13 middleware layers that wrap the fetcher:
//
//   - Rate limiting (token bucket per domain)
//   - Request deduplication (URL + method fingerprint)
//   - Autothrottle (adaptive delay based on response times)
//   - Cookie persistence (file-backed or in-memory jar)
//   - Referer chain (natural browsing simulation)
//   - Blocked response detection (403/429/503/CAPTCHA triggers)
//   - Redirect following (with loop detection)
//   - Depth limiting (max crawl depth from seed)
//   - Retry with exponential backoff
//   - Delta-fetch (skip unchanged pages via ETag/Last-Modified)
//   - Circuit breaker (3-state FSM: closed → open → half-open)
//   - Metrics collection (Prometheus counters)
//   - Robots.txt compliance
//
// Middleware is composable: each layer wraps a Fetcher and returns a Fetcher,
// so you can stack them in any order or add custom middleware.
//
// # Adaptive Selectors
//
// Websites frequently change their DOM structure — class names rotate, IDs are
// randomized, layouts shift. Foxhound's adaptive selector system survives these
// rewrites by building element signatures (tag, position, text patterns, ancestor
// structure) alongside CSS selectors. When a selector stops matching, the system
// falls back to similarity matching against saved signatures.
//
// Enable with Hunt.WithAdaptive and use via Response.Adaptive, Response.CSSAdaptive,
// Response.CSSAdaptiveAll, or Trail.Adaptive. Signatures can be stored in JSON
// files or SQLite.
//
// # Example: Hunt Campaign
//
// A Hunt is the standard way to scrape at scale. Define a Processor, configure
// middleware and writers, add seed URLs, and run:
//
//	hunt := engine.NewHunt("bookstore",
//	    engine.WithDomain("books.toscrape.com"),
//	    engine.WithWalkers(4),
//	    engine.WithProcessor(foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
//	        result := &foxhound.Result{}
//	        titles := resp.CSS("h3 a").Texts()
//	        prices := resp.CSS(".price_color").Texts()
//	        for i, title := range titles {
//	            item := foxhound.NewItem()
//	            item.Set("title", title)
//	            if i < len(prices) {
//	                item.Set("price", prices[i])
//	            }
//	            result.Items = append(result.Items, item)
//	        }
//	        // Follow pagination links
//	        result.Jobs = resp.Follow("li.next a[href]")
//	        return result, nil
//	    })),
//	)
//	hunt.AddSeed("https://books.toscrape.com/")
//	huntResult, err := hunt.Run(ctx)
//
// # Example: Trail Navigation
//
// Trails describe multi-step browser interactions for JS-heavy pages. This example
// searches Google Maps and scrolls through results:
//
//	trail := engine.NewTrail("maps-search").
//	    Navigate("https://www.google.com/maps").
//	    Fill("input#searchboxinput", "cafe in canggu").
//	    Click("button#searchbox-searchbutton").
//	    WaitOptional("div[role='feed']", 10*time.Second).
//	    InfiniteScrollInUntil("div[role='feed']", "div.Nv2PK", 20, 100).
//	    Evaluate("() => document.querySelectorAll('.Nv2PK').length")
//
//	jobs := trail.ToJobs()
//
// # Example: Session (Ad-Hoc Scraping)
//
// Session is the lightweight alternative to Hunt for quick, stateful fetches.
// Cookies persist across calls, and the identity stays consistent:
//
//	sess := foxhound.NewSession(
//	    foxhound.WithSessionFetcher(fetch.NewStealth()),
//	    foxhound.WithSessionIdentity(identity.Generate()),
//	    foxhound.WithSessionProxy("http://user:pass@proxy.example:8080"),
//	)
//	defer sess.Close()
//
//	resp, err := sess.Get(ctx, "https://example.com/login")
//	// cookies from login response are automatically persisted
//	resp2, err := sess.Get(ctx, "https://example.com/dashboard")
//
// # Example: CSS and XPath Selectors
//
// Response provides built-in CSS and XPath querying without importing the parse
// package:
//
//	// Single element
//	title := resp.CSS("h1.title").Text()
//	price := resp.CSS("span.price").Text()
//	image := resp.CSS("img.product").Attr("src")
//
//	// Multiple elements
//	allTitles := resp.CSS("h3 a").Texts()
//	allLinks  := resp.CSS("a.product[href]").Attrs("href")
//	count     := resp.CSS("div.result").Len()
//
//	// XPath (subset converted to CSS internally)
//	author := resp.XPath("//span[@class='author']")
//
// # Example: Follow Links
//
// Response.Follow extracts links from the page and generates follow-up Jobs:
//
//	// Follow all product links, route to a different handler
//	jobs := resp.Follow("a.product-link[href]",
//	    foxhound.WithFollowCallback("parseProduct"),
//	    foxhound.WithFollowReferer(true),
//	)
//
//	// Follow a single known URL
//	nextPage := resp.FollowURL("/api/products?page=2")
//
//	// Follow all anchor links on the page
//	allJobs := resp.FollowAll()
//
// # Example: XHR/Fetch Capture
//
// Capture background API calls that JavaScript makes after page load. This is
// essential for SPAs where data loads via XHR/fetch, not in the initial HTML:
//
//	trail := engine.NewTrail("api-capture").
//	    Navigate("https://example.com/app").
//	    CaptureXHR("*/api/v2/products*").
//	    Click("button.load-data").
//	    Wait("div.results", 5*time.Second)
//
// The captured responses are available in Response.CapturedXHR as a slice of maps
// with keys: request_url, request_method, status, headers, body.
//
// # Example: Cloudflare Solve
//
// For sites behind Cloudflare's JavaScript challenge, Foxhound can detect and wait
// for the challenge to complete:
//
//	fetcher := fetch.NewCamoufox(
//	    fetch.WithSolveCloudflare(30 * time.Second),
//	)
//	// resp.CloudflareSolved is true when the challenge was detected and solved.
//	// Verification checks: cf_clearance cookie, absence of Turnstile DOM markers,
//	// and a non-empty cf-turnstile-response token.
//
// # Example: Multi-Session Campaigns
//
// Route different jobs through different identities and proxies within a single Hunt:
//
//	indexSession := foxhound.NewSession(
//	    foxhound.WithSessionFetcher(fetch.NewStealth()),
//	    foxhound.WithSessionProxy("http://proxy-a:8080"),
//	)
//	detailSession := foxhound.NewSession(
//	    foxhound.WithSessionFetcher(fetch.NewCamoufox()),
//	    foxhound.WithSessionProxy("http://proxy-b:8080"),
//	)
//
//	hunt := engine.NewHunt("multi-session", /* ... */)
//	hunt.AddSession("index", indexSession)
//	hunt.AddSession("detail", detailSession)
//
//	// Jobs with SessionID "index" use indexSession's fetcher and proxy;
//	// jobs with SessionID "detail" use detailSession's browser.
//
// # Example: Development Mode
//
// Cache responses on disk for zero-network iteration during development:
//
//	hunt := engine.NewHunt("dev",
//	    engine.WithDevelopmentMode("./dev-cache"),
//	    // ... other options
//	)
//	// First run: fetches from network, saves responses to ./dev-cache/
//	// Subsequent runs: replays cached responses instantly
//
// # Sub-Packages
//
// The foxhound module is organized into focused sub-packages:
//
//   - [engine] — Hunt, Trail, Walker, scheduler, retry logic, stats collection,
//     and ItemList for thread-safe item accumulation with CSV/JSON/JSONL export.
//
//   - [fetch] — Stealth HTTP client (TLS fingerprinting + header ordering),
//     Camoufox browser automation (Juggler protocol), Smart router (auto-escalation),
//     XHR capture, page pool management, domain risk scoring, and SOCKS5 auth relay.
//
//   - [identity] — Profile generation with 60 embedded device profiles. Produces
//     consistent identity bundles (UA, TLS, headers, OS, hardware, screen, locale,
//     geo) and Camoufox fingerprint configs.
//
//   - [behavior] — Human behavior simulation: timing (Weibull/Gamma distributions),
//     mouse (Bezier curves), scroll patterns, keyboard input, navigation profiles,
//     and session fatigue modeling.
//
//   - [middleware] — 13 composable middleware layers: rate limiting, dedup, retry,
//     autothrottle, cookies, referer, redirect, depth, delta-fetch, circuit breaker,
//     metrics, blocked detection, and robots.txt.
//
//   - [parse] — Content extraction: CSS (goquery), JSON (dot-path), XPath (subset),
//     regex, structured schema, Markdown/text conversion, metadata (JSON-LD, OpenGraph,
//     NextData, NuxtData), contact deobfuscation, sitemap/feed parsing, adaptive
//     selectors, HTML table extraction, JS preload detection, directory listings,
//     pagination detection, and auto-detection with Readability-style scoring.
//
//   - [pipeline] — Item processing stages: validation, cleaning, deduplication,
//     field transformation (regex, rename, type coercion), and chain composition.
//
//   - [pipeline/export] — Output writers: JSON, JSONL, CSV, XML, SQLite, PostgreSQL,
//     Markdown, Text, and Webhook.
//
//   - [proxy] — Proxy pool management with geo-aware selection, health checking,
//     cooldown tracking, and provider adapters (BrightData, Oxylabs, Smartproxy).
//
//   - [queue] — Job queue implementations: in-memory (heap-based priority queue),
//     Redis (sorted sets), and SQLite (persistent).
//
//   - [cache] — Response caching: in-memory (LRU + TTL), file-based (SHA256 keys),
//     Redis, and SQLite.
//
//   - [captcha] — CAPTCHA detection (Cloudflare, reCAPTCHA, hCAPTCHA, GeeTest) and
//     solving via NopeCHA, CapSolver, 2Captcha, and Turnstile.
//
//   - [monitor] — Observability: atomic stat counters, Prometheus metrics (isolated
//     registry), and webhook-based alerting rules.
//
//   - [cmd/foxhound] — CLI tool: init, run, check, proxy-test, shell, browser-shell,
//     resume, curl2fox, and preview commands.
package foxhound
