# Getting Started

## Prerequisites

- Go 1.23 or later (`go version` to check)
- Git

Optional, for browser mode:

- `go build -tags playwright ./...` — enables the real Camoufox backend
- After building with the playwright tag, install the browser once:
  ```bash
  go run github.com/playwright-community/playwright-go/cmd/playwright install firefox
  ```

## Installation

### Option 1 — Build from source

```bash
git clone https://github.com/sadewadee/foxhound.git
cd foxhound
go build -o foxhound ./cmd/foxhound/
```

Move the binary to your PATH:

```bash
mv foxhound /usr/local/bin/
```

### Option 2 — Use as a library

```bash
go get github.com/sadewadee/foxhound
```

### Option 3 — Docker

```bash
docker compose up
```

See [Deployment](deployment.md) for full Docker usage.

## Your First Scrape

This example scrapes book titles and prices from books.toscrape.com, a public scraping sandbox.

### Step 1 — Scaffold the project

```bash
foxhound init mybookscraper
cd mybookscraper
go mod tidy
```

The `init` command creates:

```
mybookscraper/
  go.mod        — Go module file
  main.go       — skeleton scraper with ProcessorFunc
  config.yaml   — full configuration template
  .env.example  — environment variable reference
```

### Step 2 — Write the processor

Open `main.go`. Replace the placeholder `Process` body with real extraction logic:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "log/slog"
    "os"
    "time"

    foxhound "github.com/sadewadee/foxhound"
    "github.com/sadewadee/foxhound/engine"
    "github.com/sadewadee/foxhound/fetch"
    "github.com/sadewadee/foxhound/identity"
    "github.com/sadewadee/foxhound/parse"
    "github.com/sadewadee/foxhound/pipeline"
    "github.com/sadewadee/foxhound/pipeline/export"
    "github.com/sadewadee/foxhound/queue"
    "github.com/PuerkitoBio/goquery"
)

const baseURL = "http://books.toscrape.com/"

func main() {
    slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

    // 1. Generate a consistent identity profile.
    profile := identity.Generate(
        identity.WithBrowser(identity.BrowserFirefox),
        identity.WithOS(identity.OSWindows),
        identity.WithLocale("en-US", "en-US", "en"),
        identity.WithTimezone("America/New_York"),
    )

    // 2. Create a stealth HTTP fetcher.
    fetcher := fetch.NewStealth(
        fetch.WithIdentity(profile),
        fetch.WithTimeout(30*time.Second),
    )
    defer fetcher.Close()

    // 3. Create an in-memory queue.
    q := queue.NewMemoryQueue()
    defer q.Close()

    // 4. Create a CSV writer with explicit column order.
    csvWriter, err := export.NewCSV("books.csv", "title", "price", "rating", "url")
    if err != nil {
        log.Fatalf("creating CSV writer: %v", err)
    }
    defer csvWriter.Close()

    // 5. Build the pipeline.
    pipelineChain := pipeline.NewChain(
        &pipeline.Validate{Required: []string{"title", "price", "url"}},
        &pipeline.Clean{TrimWhitespace: true},
    )

    // 6. Define the processor.
    processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
        if resp.StatusCode != 200 {
            return &foxhound.Result{}, nil
        }
        doc, err := parse.NewDocument(resp)
        if err != nil {
            return nil, err
        }

        var items []*foxhound.Item
        doc.Each("article.product_pod", func(i int, s *goquery.Selection) {
            item := foxhound.NewItem()
            item.URL = resp.URL
            title, _ := s.Find("h3 a").Attr("title")
            item.Set("title", title)
            item.Set("price", s.Find("p.price_color").Text())
            ratingClass, _ := s.Find("p.star-rating").Attr("class")
            item.Set("rating", ratingClass)
            href, _ := s.Find("h3 a").Attr("href")
            item.Set("url", baseURL+href)

            processed, _ := pipelineChain.Process(ctx, item)
            if processed != nil {
                items = append(items, processed)
            }
        })

        // Follow pagination.
        var jobs []*foxhound.Job
        nextHref := doc.Attr("li.next a", "href")
        if nextHref != "" {
            jobs = append(jobs, &foxhound.Job{
                URL:       baseURL + "catalogue/" + nextHref,
                FetchMode: foxhound.FetchStatic,
                Priority:  foxhound.PriorityNormal,
            })
        }

        return &foxhound.Result{Items: items, Jobs: jobs}, nil
    })

    // 7. Create and run the hunt.
    h := engine.NewHunt(engine.HuntConfig{
        Name:      "books-toscrape",
        Domain:    "books.toscrape.com",
        Walkers:   2,
        Fetcher:   fetcher,
        Processor: processor,
        Queue:     q,
        Writers:   []foxhound.Writer{csvWriter},
        Seeds: []*foxhound.Job{{
            ID:        "seed-catalogue",
            URL:       baseURL,
            FetchMode: foxhound.FetchStatic,
            Priority:  foxhound.PriorityHigh,
        }},
    })

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()

    if err := h.Run(ctx); err != nil {
        log.Fatalf("hunt failed: %v", err)
    }

    fmt.Printf("Done. Stats: %s\n", h.Stats().Summary())
}
```

### Step 3 — Run

```bash
go run .
```

Output is written to `books.csv`. You should see log lines like:

```
time=2026-03-18T10:00:00Z level=INFO msg="identity generated" ua="Mozilla/5.0..."
time=2026-03-18T10:00:00Z level=INFO msg="hunt started" walkers=2 seeds=1
time=2026-03-18T10:00:01Z level=INFO msg="hunt complete"
```

### Step 4 — Use the CLI instead

To run the same hunt from the CLI without writing Go code, edit `config.yaml`:

```yaml
hunt:
  domain: books.toscrape.com
  walkers: 2
```

Then:

```bash
foxhound run --config config.yaml
```

The CLI uses a built-in default processor that extracts titles and links.

## Project Structure (after `foxhound init`)

```
myproject/
  main.go        — your processor implementation
  config.yaml    — hunt, identity, proxy, middleware, pipeline config
  .env.example   — environment variable template
  go.mod         — Go module
  output/        — export files written here by default
```

## Running Modes

| Mode | Command | Use When |
|------|---------|----------|
| CLI | `foxhound run --config config.yaml` | Quick runs, no Go code needed |
| Go API | `go run .` | Custom processors, library embedding |
| Docker | `docker compose up` | Production, multi-worker |
| Static-only | `FOXHOUND_MODE=static foxhound run ...` | No browser dependency |
| Browser | `-tags playwright` build | JS-heavy / protected sites |

## Next Steps

- [Configuration reference](configuration.md) — all config.yaml fields
- [CLI reference](cli.md) — all commands and flags
- [Go API](api.md) — types, interfaces, and patterns
- [Anti-detection](anti-detection.md) — how the identity system works
- [Examples](examples.md) — more complete worked examples
