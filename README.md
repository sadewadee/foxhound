<div align="center">
  <img src="assets/foxhound-banner.webp" alt="Foxhound - Go Scraping Framework" width="600" height="450"/>
</div>

<p align="center">
  <strong>Go Scraping Framework with Native Camoufox Anti-Detection</strong>
</p>

# Foxhound v0.0.1

Go scraping framework with native Camoufox (Firefox fork) anti-detection, dual-mode fetching, and 13-layer middleware.

## Highlights

- **Dual-mode fetching**: TLS-impersonating HTTP client (~5-50ms) + Camoufox browser via playwright-go (~500ms-5s), with automatic escalation on block detection
- **Consistent identity profiles**: UA + TLS fingerprint + header order + OS + hardware + screen + locale all match — randomness without consistency causes instant blocks
- **13-layer middleware chain**: concurrency, metrics, rate limit, robots.txt, delta-fetch, dedup, autothrottle, cookies, referer, blocked detector, redirect, depth limit, retry
- **9 export formats**: JSON, JSONL, CSV, Markdown (Table/List/Cards), Text (Lines/Pretty), XML, SQLite, PostgreSQL, Webhook
- **Adaptive parsing**: CSS selectors with automatic similarity-based fallback when page structure changes
- **Streaming API**: `Hunt.Stream(ctx)` and `Hunt.StreamWithStats(ctx, interval)` for real-time item processing
- **Checkpoint/resume**: auto-save hunt state every N items; `engine.LoadCheckpoint` to inspect
- **37,003 lines of Go across 24 packages, 700+ tests**

## Quick Start

```bash
git clone https://github.com/sadewadee/foxhound.git
cd foxhound
go build -o foxhound ./cmd/foxhound/
foxhound init myproject && cd myproject
go mod tidy
```

Scrape books.toscrape.com in under 20 lines:

```go
h := engine.NewHunt(engine.HuntConfig{
    Name:    "books",
    Domain:  "books.toscrape.com",
    Walkers: 3,
    Fetcher: fetch.NewStealth(fetch.WithIdentity(identity.Generate())),
    Queue:   queue.NewMemoryQueue(),
    Processor: foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
        doc, _ := parse.NewDocument(resp)
        item := foxhound.NewItem()
        item.Set("title", doc.Text("h1"))
        return &foxhound.Result{Items: []*foxhound.Item{item}}, nil
    }),
    Seeds: []*foxhound.Job{{URL: "http://books.toscrape.com/", FetchMode: foxhound.FetchStatic}},
})
h.Run(context.Background())
```

## Real Results

| Target | Mode | Items | Time | Notes |
|--------|------|-------|------|-------|
| books.toscrape.com | Static | 1000 books | ~8s | 50 pages, 2 walkers |
| quotes.toscrape.com | Static | 100 quotes | ~3s | 10 pages, 2 walkers |
| Google Maps listing | Browser | 1 place | ~4s | FetchBrowser, Camoufox |

## Documentation

| File | Contents |
|------|----------|
| [docs/getting-started.md](docs/getting-started.md) | Install, first scrape, running modes |
| [docs/configuration.md](docs/configuration.md) | Full config.yaml reference |
| [docs/cli.md](docs/cli.md) | All CLI commands and flags |
| [docs/api.md](docs/api.md) | Go types, interfaces, Hunt/Stream API |
| [docs/anti-detection.md](docs/anti-detection.md) | Identity system, TLS, behavior simulation |
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
