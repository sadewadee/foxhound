# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Foxhound is a Go scraping framework with native Camoufox (Firefox fork) anti-detection. It uses dual-mode fetching: a TLS-impersonating HTTP client for static pages and Camoufox via playwright-go for JS-heavy/protected pages, with automatic escalation when blocks are detected.

**Status**: Phases 1-5 implemented. 19 packages, 991 tests. Phase 5 added: resource-type blocking, network interception, domain blocking, Response.FollowURL, selector wait states, spider block detection/retry, ItemList/CrawlResult/CrawlStats export, Element.FindSimilar, SQLite adaptive storage, page pool stats, fingerprint-customizable dedup, Stats.ToMap.

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

# CLI commands
foxhound run --config config.yaml
foxhound init myproject
foxhound check
foxhound proxy-test --config config.yaml
foxhound shell
foxhound resume --hunt-id <id> --queue redis://localhost:6379

# Docker
docker compose up
docker compose up --scale foxhound=4
docker compose --profile monitoring up
```

## Architecture

### Dual-Mode Fetcher (core differentiator)
- **Static path** (`FetchStatic`): Go HTTP client with header ordering from identity profile. ~5-50ms/req. (`fetch/stealth.go`)
- **Browser path** (`FetchBrowser`): Camoufox (Firefox fork) via playwright-go using Juggler protocol. ~500ms-5s/req. (`fetch/camoufox.go` — Phase 1 stub, needs playwright-go install)
- **Smart Router** (`FetchAuto`): starts static, auto-escalates to browser on block detection (403/429/503). (`fetch/smart.go`)

### Identity System
Every request uses a complete, internally-consistent identity profile: UA + TLS fingerprint + header order + OS + hardware + screen + locale + geo must all match. 60 embedded device profiles via Go `embed` directive in `identity/data/`. Generate with functional options:
```go
id := identity.Generate(identity.WithBrowser(identity.BrowserFirefox), identity.WithOS(identity.OSWindows))
```

### Key Terminology
- **Hunt** (`engine/hunt.go`): scraping campaign orchestrator — seeds queue, launches walkers, collects stats
- **Trail** (`engine/trail.go`): fluent navigation path builder (Navigate → Click → Wait → Extract)
- **Walker** (`engine/walker.go`): goroutine that pops jobs, fetches, processes, writes items, enqueues discovered jobs
- **Job** (`foxhound.go`): unit of work (URL + FetchMode + Priority + Meta)

### Request Data Flow
```
Job → middleware chain (rate limit → dedup → autothrottle → cookies → referer → retry)
  → Smart Fetcher (static or browser) → Parser → User Process()
  → Result{Items, Jobs} → Pipeline chain (validate → clean → dedup → transform)
  → Writers (CSV/JSON/webhook) + Queue (new jobs)
```

### Package Map
```
foxhound/
  foxhound.go     — core types: Job, Response, Item, Result, Fetcher, Queue, Pipeline, Writer, Middleware
  config.go       — YAML config parser with env var expansion and defaults
  engine/         — hunt, walker, trail, scheduler, retry, stats
  identity/       — profile generation, embedded device/TLS/header databases (60 profiles)
  fetch/          — stealth (HTTP+headers), camoufox (browser stub), smart (auto-router)
  proxy/          — pool, health, cooldown, static provider
  proxy/providers — brightdata, oxylabs, smartproxy adapters
  behavior/       — timing (log-normal), mouse (bezier), scroll, keyboard, navigation, profiles
  middleware/     — ratelimit, dedup, depth, retry, autothrottle, cookies, referer, redirect, deltafetch, metrics
  parse/          — goquery (CSS), json (dot-path), xpath (subset→CSS), regex, structured (schema)
  pipeline/       — validate, clean, dedup, transform, chain
  pipeline/export — json/jsonl, csv, webhook writers
  queue/          — memory (heap), redis (sorted set), sqlite (persistent)
  cache/          — memory (LRU+TTL), file (SHA256), redis, sqlite
  monitor/        — stats (atomic counters), prometheus (isolated registry), alerting (webhook rules)
  captcha/        — detect (cloudflare/recaptcha/hcaptcha/geetest), capsolver, twocaptcha, turnstile
  cmd/foxhound/   — CLI: init, run, check, proxy-test, shell, resume
  examples/       — ecommerce (books.toscrape.com), travel, realtime price monitor
```

## Key Dependencies

- `goquery` — HTML/CSS selector parsing
- `go-redis/v9` — Redis client (queue, cache)
- `modernc.org/sqlite` — pure-Go SQLite (queue, cache)
- `prometheus/client_golang` — metrics
- `golang.org/x/time/rate` — rate limiting
- `gopkg.in/yaml.v3` — config parsing

## Anti-Detection Design Principles

These must be maintained in all implementation work:

1. **Consistency over randomness**: all identity attributes (UA, TLS, headers, OS, hardware, screen, locale, geo) must be internally consistent.
2. **Human timing uses log-normal distribution** (`behavior/timing.go`), not uniform random.
3. **Camoufox chosen over Chromium** because Juggler protocol is less targeted by anti-bot than CDP.
4. **Goal is to never trigger CAPTCHA**. If CAPTCHA appears, earlier layers failed.
5. **Proxy geo must match identity locale/timezone**.

## Config

Example config at `config/config.yaml`. All config structs in `config.go`. Supports env var expansion via `os.ExpandEnv`. Defaults applied for all missing values.
