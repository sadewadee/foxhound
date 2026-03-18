<div align="center">
  <img src="assets/foxhound-banner.webp" alt="Foxhound - Go Scraping Framework" width="600" height="450"/>
</div>

<p align="center">
  <strong>Go Scraping Framework with Native Camoufox Anti-Detection</strong>
</p>

# Foxhound v0.0.1

Go scraping framework with native Camoufox (Firefox fork) anti-detection and dual-mode fetching.

## Features

- **Dual-mode fetching**: TLS-impersonating HTTP client (~5-50ms) + Camoufox browser via playwright-go (~500ms-5s)
- **Smart auto-escalation**: starts static, escalates to browser on 401/403/407/429/503
- **60+ consistent device profiles**: UA + TLS fingerprint + header order + OS + hardware + screen + locale all match
- **Human behavior simulation**: log-normal timing, Bezier mouse curves, realistic scroll/keyboard patterns
- **11-layer middleware chain**: metrics, rate limit, robots.txt, delta-fetch, dedup, autothrottle, cookies, referer, redirect, depth limit, retry
- **Multiple export formats**: JSON, JSONL, CSV, webhook, PostgreSQL
- **Pluggable queue backends**: memory, Redis, SQLite

## Quick Start

**Install:**

```bash
git clone https://github.com/sadewadee/foxhound.git
cd foxhound
go build -o foxhound ./cmd/foxhound/
```

**Scaffold a project:**

```bash
foxhound init myproject
cd myproject
go mod tidy
foxhound run --config config.yaml
```

**Go API (books.toscrape.com):**

```go
package main

import (
    "context"
    "time"

    foxhound "github.com/sadewadee/foxhound"
    "github.com/sadewadee/foxhound/engine"
    "github.com/sadewadee/foxhound/fetch"
    "github.com/sadewadee/foxhound/identity"
    "github.com/sadewadee/foxhound/parse"
    "github.com/sadewadee/foxhound/queue"
    "github.com/sadewadee/foxhound/pipeline/export"
    "github.com/PuerkitoBio/goquery"
)

func main() {
    profile := identity.Generate(
        identity.WithBrowser(identity.BrowserFirefox),
        identity.WithOS(identity.OSWindows),
        identity.WithLocale("en-US", "en-US", "en"),
        identity.WithTimezone("America/New_York"),
    )

    fetcher := fetch.NewStealth(
        fetch.WithIdentity(profile),
        fetch.WithTimeout(30*time.Second),
    )
    defer fetcher.Close()

    q := queue.NewMemoryQueue()
    defer q.Close()

    csvWriter, _ := export.NewCSV("books.csv", "title", "price", "url")
    defer csvWriter.Close()

    processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
        doc, _ := parse.NewDocument(resp)
        var items []*foxhound.Item
        doc.Each("article.product_pod", func(i int, s *goquery.Selection) {
            item := foxhound.NewItem()
            title, _ := s.Find("h3 a").Attr("title")
            item.Set("title", title)
            item.Set("price", s.Find("p.price_color").Text())
            item.Set("url", resp.URL)
            items = append(items, item)
        })
        return &foxhound.Result{Items: items}, nil
    })

    h := engine.NewHunt(engine.HuntConfig{
        Name:      "books-toscrape",
        Domain:    "books.toscrape.com",
        Walkers:   2,
        Fetcher:   fetcher,
        Processor: processor,
        Queue:     q,
        Writers:   []foxhound.Writer{csvWriter},
        Seeds: []*foxhound.Job{{
            ID:        "seed",
            URL:       "http://books.toscrape.com/",
            FetchMode: foxhound.FetchStatic,
            Priority:  foxhound.PriorityHigh,
        }},
    })

    h.Run(context.Background())
}
```

## Documentation

Full documentation: [docs/](docs/)

- [Getting Started](docs/getting-started.md)
- [Configuration](docs/configuration.md)
- [CLI Reference](docs/cli.md)
- [Go API](docs/api.md)
- [Anti-Detection](docs/anti-detection.md)
- [Middleware](docs/middleware.md)
- [Pipeline & Export](docs/pipeline.md)
- [Proxy Management](docs/proxy.md)
- [Browser Mode](docs/browser.md)
- [Examples](docs/examples.md)
- [Deployment](docs/deployment.md)

## Real Scraping Results

| Target | Items | Mode | Duration |
|--------|-------|------|----------|
| books.toscrape.com | 1000 books | static | ~45s |
| Google Maps | 10 villa listings | browser | ~120s |
| Alibaba | 10 products | auto (escalated) | ~90s |

## Build Tags

| Tag | Effect |
|-----|--------|
| *(default)* | Standard net/http with correct header ordering |
| `-tags tls` | Real JA3/JA4 TLS impersonation via azuretls-client |
| `-tags playwright` | Real Camoufox browser via playwright-go |

## License

MIT
