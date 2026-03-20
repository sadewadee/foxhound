# Getting Started

## Prerequisites

- Go 1.25 or later (`go version` to check)
- Git

Optional, for browser mode:

- `go build -tags playwright ./...` — enables the real Camoufox backend
- After building with the playwright tag, install the browser once:
  ```bash
  go run github.com/playwright-community/playwright-go/cmd/playwright install firefox
  ```

## Stats

- 37,003 lines of Go across 18 packages
- 1000+ tests
- 174 Go source files

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

## Quick Trail API Example

The Trail API provides a fluent interface for browser-based navigation flows. Here is a Google Maps search:

```go
trail := engine.NewTrail("maps").
    Navigate("https://www.google.com/maps").
    Fill("input#searchboxinput", "cafe in canggu").
    Click("button#searchbox-searchbutton").
    Wait("div[role='feed']", 10*time.Second).
    InfiniteScrollInUntil("div[role='feed']", "div.Nv2PK", 20, 100)
```

## NopeCHA Extension

NopeCHA is auto-downloaded on the first Camoufox browser launch. It handles hCaptcha, reCAPTCHA, and Turnstile challenges automatically. No manual setup required.

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

    csvWriter, err := export.NewCSV("books.csv", "title", "price", "rating", "url")
    if err != nil {
        log.Fatalf("creating CSV writer: %v", err)
    }
    defer csvWriter.Close()

    pipelineChain := pipeline.NewChain(
        &pipeline.Validate{Required: []string{"title", "price", "url"}},
        &pipeline.Clean{TrimWhitespace: true},
    )

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
time=2026-03-18T10:00:00Z level=INFO msg="hunt started" walkers=2 seeds=1
time=2026-03-18T10:00:01Z level=INFO msg="hunt complete"
```

### Step 4 — Streaming mode

Instead of waiting for the hunt to finish, receive items as they arrive:

```go
ch, err := h.Stream(ctx)
if err != nil {
    log.Fatal(err)
}
for item := range ch {
    fmt.Printf("title=%s price=%s\n", item.GetString("title"), item.GetString("price"))
}
```

Or with periodic stats:

```go
events, err := h.StreamWithStats(ctx, 5*time.Second)
if err != nil {
    log.Fatal(err)
}
for event := range events {
    if event.Item != nil {
        fmt.Println(event.Item.GetString("title"))
    }
    if event.Stats != nil {
        fmt.Println(event.Stats.Summary())
    }
}
```

### Step 5 — Checkpoint / resume

Enable auto-checkpointing to survive crashes:

```go
h := engine.NewHunt(engine.HuntConfig{
    Checkpoint: engine.CheckpointConfig{
        Enabled:  true,
        Path:     "/tmp/books.checkpoint.json",
        Interval: 100, // save every 100 items
    },
    // ...
})
```

The checkpoint is written atomically. Inspect it:

```go
cp, err := engine.LoadCheckpoint("/tmp/books.checkpoint.json")
fmt.Printf("items=%d queue=%d elapsed=%dms\n", cp.ItemsProcessed, cp.QueueLen, cp.ElapsedMs)
```

### Step 6 — Use the CLI instead

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
| Streaming | `hunt.Stream(ctx)` | Real-time item processing |
| Docker | `docker compose up` | Production, multi-worker |
| Static-only | `FOXHOUND_MODE=static foxhound run ...` | No browser dependency |
| Browser | `-tags playwright` build | JS-heavy / protected sites |

## Next Steps

- [Configuration reference](configuration.md) — all config.yaml fields
- [CLI reference](cli.md) — all commands and flags
- [Go API](api.md) — types, interfaces, and patterns
- [Anti-detection](anti-detection.md) — how the identity system works
- [Examples](examples.md) — more complete worked examples
