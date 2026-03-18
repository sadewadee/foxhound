<div align="center">
  <img src="assets/foxhound-banner.webp" alt="Foxhound - Go Scraping Framework" width="600" height="450"/>
</div>

<p align="center">
  <strong>🦊 Go Scraping Framework with Native Camoufox Anti-Detection</strong>
</p>

## Overview

**Foxhound** is a high-performance Go scraping framework engineered to defeat modern anti-bot detection. It combines:

- **Dual-mode fetching**: TLS-impersonating HTTP client for static pages (~5-50ms) + Camoufox (Firefox fork) via playwright-go for JS-heavy/protected pages (~500ms-5s)
- **Smart escalation**: Automatically detects blocks (403/429/503) and escalates to browser
- **Complete identity profiles**: 60+ embedded device profiles with consistent UA + TLS + headers + OS + hardware + screen + locale + geo
- **Human behavior simulation**: Log-normal timing distribution, Bézier mouse curves, realistic scroll/keyboard patterns
- **Production-ready**: 19 packages, 408 tests, 18k+ LOC, Redis/SQLite support, Prometheus metrics

---

## Quick Start

### Installation

```bash
# Clone & build
git clone https://github.com/sadewadee/foxhound.git
cd foxhound
go build -o foxhound ./cmd/foxhound/

# or use Docker
docker compose up
```

### Basic Usage

```bash
# Initialize a new scraping project
foxhound init myproject
cd myproject

# Run with configuration
foxhound run --config config.yaml

# Test proxy setup
foxhound proxy-test --config config.yaml

# Interactive shell
foxhound shell

# Resume interrupted hunt
foxhound resume --hunt-id <id> --queue redis://localhost:6379
```

### Go API Example

```go
package main

import (
	"github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/engine"
	"github.com/foxhound-scraper/foxhound/parse"
)

func main() {
	// Create hunt
	hunt := engine.NewHunt("books.toscrape.com", 4)

	// Add initial jobs
	hunt.Enqueue(&foxhound.Job{
		URL:      "https://books.toscrape.com",
		Priority: 1,
	})

	// Define scraping logic
	hunt.OnFetch(func(resp *foxhound.Response) (*foxhound.Result, error) {
		doc := parse.Goquery(resp.Body)

		var items []foxhound.Item
		doc.Find(".product_pod").Each(func(i int, s *parse.Selection) {
			items = append(items, foxhound.Item{
				"title": s.Find("h3 a").AttrOr("title", ""),
				"price": s.Find(".price_color").Text(),
			})
		})

		return &foxhound.Result{Items: items}, nil
	})

	// Run scrape
	hunt.Run()
}
```

---

## Architecture

### Core Components

| Component | Purpose | Performance |
|-----------|---------|-------------|
| **Dual Fetcher** | Routes requests to static HTTP client or Camoufox | Auto-escalates on detection |
| **Identity System** | Manages consistent device profiles (UA, TLS, headers, fingerprint) | 60+ embedded profiles |
| **Middleware Chain** | Rate limiting, deduplication, autothrottle, cookies, retry, deltafetch | Ordered execution |
| **Behavior Engine** | Human-like timing, mouse movement, scroll, keyboard, navigation | Log-normal distribution |
| **Parser Library** | CSS selectors (goquery), XPath, JSON dot-path, regex, structured schemas | Flexible extraction |
| **Pipeline** | Validation, cleaning, deduplication, transformation, export (JSON/CSV/webhook) | Chainable stages |
| **Queue System** | Memory (heap), Redis (sorted set), SQLite (persistent) | Distributed-ready |
| **Monitoring** | Prometheus metrics, alerting via webhooks, live stats | Per-walker counters |
| **Captcha Handling** | Detection + solvers (Capsolver, 2Captcha, Turnstile) | Cloudflare/reCAPTCHA/hCaptcha/GeeTest |
| **Proxy Rotation** | Health checking, cooldown, provider adapters (Bright Data, Oxylabs, SmartProxy) | Geo-aware |

### Request Flow

```
Job → Middleware Chain (rate limit → dedup → autothrottle → cookies → referer → retry)
  → Smart Fetcher (FetchAuto: static → browser escalation)
    → Static: HTTP client with TLS impersonation (5-50ms)
    → Browser: Camoufox + Juggler protocol (500ms-5s)
  → Parser (goquery/XPath/JSON/regex/structured)
  → User Process() function
  → Result {Items, NextJobs}
  → Pipeline Chain (validate → clean → dedup → transform)
  → Writers (CSV/JSON/webhook)
  → Queue (new discovered jobs)
```

---

## Config Example

```yaml
# config/config.yaml
hunt:
  name: "books.toscrape"
  workers: 4
  timeout: 30s
  max_depth: 3

fetcher:
  mode: "auto"           # static, browser, or auto (default)
  timeout: 15s
  retries: 3

identity:
  browser: "firefox"     # chrome or firefox
  os: "linux"           # linux, windows, macos
  # or random: true     # randomize each walker

proxy:
  enabled: true
  provider: "brightdata"
  rotation: "per-request"
  cooldown: 5m

queue:
  backend: "memory"      # memory, redis, sqlite
  redis: "redis://localhost:6379"

cache:
  backend: "memory"
  ttl: 1h

middleware:
  ratelimit: 10/s
  dedup: true
  autothrottle: true
  cookies: true
  deltafetch: false

pipeline:
  validate: true
  clean: true
  dedup: true

export:
  format: "jsonl"
  path: "./output.jsonl"
  webhook: "https://api.example.com/items"
```

---

## Key Features

### 🔐 Anti-Detection Layers

1. **TLS Impersonation** (`fetch/stealth.go`)
   - JA3/JA4 fingerprint matching
   - Chrome/Firefox cipher suite reordering
   - GREASE value injection
   - HTTP/2 SETTINGS frame emulation

2. **Browser Fingerprinting** (`identity/`)
   - 60+ device profile combinations
   - Consistent screen resolution, hardware, OS, timezone
   - Device pixel ratio, GPU, WebGL vendor matching

3. **Behavioral** (`behavior/`)
   - Log-normal request timing (not uniform random)
   - Bézier curve mouse movement
   - Realistic scroll/keyboard patterns
   - Navigation delays between page loads

4. **Contextual** (`middleware/`)
   - Referer header consistency
   - Cookie jar management
   - User-Agent rotation with matching TLS
   - Proxy geo matching identity locale

### 📊 Production Ready

- **19 packages, 408 tests**: Comprehensive test coverage with race detector
- **Redis/SQLite backends**: Distributed queue and cache support
- **Prometheus metrics**: Per-walker counters, histograms, alerts
- **Docker Compose**: Single-worker or multi-worker (scale: 4) or full monitoring stack
- **CLI tools**: init, run, check, proxy-test, shell, resume commands
- **Configuration**: YAML + env var expansion + smart defaults

### 🚀 Performance

| Scenario | Latency | Notes |
|----------|---------|-------|
| Static page (HTTP client) | 5-50ms | TLS impersonation, no JS execution |
| JS-heavy page (Camoufox) | 500ms-5s | Full browser rendering |
| Rate-limited (10/s) | +100ms | Middleware overhead |
| With proxy rotation | +50-200ms | Network latency |
| With behavior timing | +500ms-5s | Log-normal delays |

---

## Build & Test

```bash
# Build everything
go build ./...

# Run all tests
go test ./...

# Run tests with race detector
go test -race ./...

# Run single package tests
go test ./engine/...
go test ./fetch/...

# Build CLI binary
go build -o foxhound ./cmd/foxhound/

# Docker
docker compose up
docker compose up --scale foxhound=4
docker compose --profile monitoring up   # includes Prometheus + Grafana
```

---

## Examples

### 🛍️ E-commerce Scraper (books.toscrape.com)

```go
examples/ecommerce/main.go
```

Scrapes book listings with:
- CSS selector parsing
- Pagination via job queue
- Deduplication middleware
- JSON export

### ✈️ Travel Price Monitor

```go
examples/travel/main.go
```

Real-time flight/hotel tracking with:
- Webhook exports
- Scheduling (recurring jobs)
- Delta-fetch (only changed items)

### 📈 Realtime Price Monitor

```go
examples/realtime/main.go
```

Continuous monitoring with:
- Redis queue for distributed workers
- Prometheus metrics
- Alerting on price changes

---

## Design Principles

1. **Consistency over randomness**: All identity attributes (UA, TLS, headers, OS, hardware, screen, locale, geo) must be internally consistent.
2. **Human timing uses log-normal distribution**, not uniform random. Real behavior is bursty/clumpy.
3. **Camoufox chosen over Chromium** because Juggler protocol is less targeted by anti-bot than CDP, and Firefox source is open for C++ patching.
4. **Goal is to never trigger CAPTCHA**. If CAPTCHA appears, earlier layers failed.
5. **Proxy geo must match identity locale/timezone**. New York proxy + Tokyo timezone = instant flag.

---

## CI/CD

GitHub Actions runs:
- **Test**: Go 1.24.6 with race detector + codecov upload
- **Lint**: golangci-lint on every push
- **Build**: CLI binary + Docker image → GitHub Container Registry

Workflows: `.github/workflows/test.yml` & `.github/workflows/docker.yml`

---

## Roadmap

- [x] Phase 1: Core engine, identity, stealth HTTP, smart router
- [x] Phase 2: Behavior engine, middleware chain, parsing, pipeline
- [x] Phase 3: Redis/SQLite backends, Prometheus metrics, captcha detection
- [x] Phase 4: CLI, examples, Docker, GitHub Actions
- [ ] Phase 5: Dashboard UI, Helm chart, advanced xpath/regex parsing
- [ ] Phase 6: Captcha solvers integration, deltafetch optimization

---

## Dependencies

- `goquery` — HTML/CSS selector parsing
- `go-redis/v9` — Redis client (queue, cache)
- `modernc.org/sqlite` — Pure-Go SQLite (queue, cache)
- `prometheus/client_golang` — Metrics
- `golang.org/x/time/rate` — Rate limiting
- `gopkg.in/yaml.v3` — Config parsing

---

## Contributing

Contributions welcome! Ensure:

1. All tests pass: `go test -race ./...`
2. Lint passes: `golangci-lint run`
3. Update CLAUDE.md if behavior changes
4. Add tests for new features

---

## License

MIT — See LICENSE file

---

## Support

- 📖 Read [CLAUDE.md](CLAUDE.md) for developer guide
- 🏗️ Architecture: [foxhound-architecture.md](foxhound-architecture.md)
- 🌐 Ecosystem: [foxhound-ecosystem.md](foxhound-ecosystem.md)
- 🐛 Issues: [GitHub Issues](https://github.com/sadewadee/foxhound/issues)

---

**Built with ❤️ for scrapers who want to do it right.**
