# Examples

## E-commerce (books.toscrape.com)

Full working example at `examples/ecommerce/main.go`.

**Run:**

```bash
go run ./examples/ecommerce/
```

**What it does:**

- Generates a Firefox/Windows identity profile
- Creates a `StealthFetcher` with that identity
- Seeds the books.toscrape.com catalogue homepage
- Parses book titles, prices, ratings, and URLs using goquery
- Follows pagination links across all 50 catalogue pages
- Runs `Validate` → `Clean` pipeline on each item
- Exports to `books.csv`

**Key patterns demonstrated:**

```go
// 1. Generate a consistent identity
profile := identity.Generate(
    identity.WithBrowser(identity.BrowserFirefox),
    identity.WithOS(identity.OSWindows),
    identity.WithLocale("en-US", "en-US", "en"),
    identity.WithTimezone("America/New_York"),
)

// 2. Create the fetcher with that identity
fetcher := fetch.NewStealth(
    fetch.WithIdentity(profile),
    fetch.WithTimeout(30*time.Second),
)
defer fetcher.Close()

// 3. Create a CSV writer with fixed column order
csvWriter, err := export.NewCSV("books.csv", "title", "price", "rating", "url")

// 4. Build the pipeline
pipelineChain := pipeline.NewChain(
    &pipeline.Validate{Required: []string{"title", "price", "url"}},
    &pipeline.Clean{TrimWhitespace: true},
)

// 5. Processor with goquery extraction
processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
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
        item.Set("price", strings.TrimSpace(s.Find("p.price_color").Text()))

        ratingClass, _ := s.Find("p.star-rating").Attr("class")
        item.Set("rating", strings.TrimPrefix(ratingClass, "star-rating "))

        relHref, _ := s.Find("h3 a").Attr("href")
        item.Set("url", resolveURL(baseURL, relHref))

        processed, _ := pipelineChain.Process(ctx, item)
        if processed != nil {
            items = append(items, processed)
        }
    })

    // Pagination
    var nextJobs []*foxhound.Job
    nextHref := doc.Attr("li.next a", "href")
    if nextHref != "" {
        nextJobs = append(nextJobs, &foxhound.Job{
            ID:        fmt.Sprintf("page-%s", nextHref),
            URL:       resolveURL(resp.URL, nextHref),
            FetchMode: foxhound.FetchStatic,
            Priority:  foxhound.PriorityNormal,
        })
    }

    return &foxhound.Result{Items: items, Jobs: nextJobs}, nil
})

// 6. Wire and run the hunt
h := engine.NewHunt(engine.HuntConfig{
    Name:      "books-toscrape",
    Domain:    "books.toscrape.com",
    Walkers:   2,
    Fetcher:   fetcher,
    Processor: processor,
    Queue:     queue.NewMemoryQueue(),
    Writers:   []foxhound.Writer{csvWriter},
    Seeds: []*foxhound.Job{{
        ID:        "seed-catalogue",
        URL:       "http://books.toscrape.com/",
        FetchMode: foxhound.FetchStatic,
        Priority:  foxhound.PriorityHigh,
    }},
})

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
defer cancel()

h.Run(ctx)
fmt.Printf("Done. %s\n", h.Stats().Summary())
```

## Google Maps Scraping

Google Maps requires JavaScript execution and anti-bot bypass. Use `FetchBrowser` mode with Camoufox.

Requires: `go build -tags playwright ./...` and `playwright install firefox`.

```go
package main

import (
    "context"
    "log"
    "time"

    foxhound "github.com/sadewadee/foxhound"
    "github.com/sadewadee/foxhound/engine"
    "github.com/sadewadee/foxhound/fetch"
    "github.com/sadewadee/foxhound/identity"
    "github.com/sadewadee/foxhound/parse"
    "github.com/sadewadee/foxhound/pipeline/export"
    "github.com/sadewadee/foxhound/queue"
)

func main() {
    profile := identity.Generate(
        identity.WithBrowser(identity.BrowserFirefox),
        identity.WithOS(identity.OSWindows),
        identity.WithCountry("US"),
    )

    // Browser fetcher required for JS-heavy Google Maps pages.
    browserFetcher, err := fetch.NewCamoufox(
        fetch.WithBrowserIdentity(profile),
        fetch.WithHeadless("virtual"),
        fetch.WithBlockImages(false), // keep images — Maps needs them for loading detection
        fetch.WithBrowserTimeout(30*time.Second),
    )
    if err != nil {
        log.Fatalf("camoufox init: %v", err)
    }
    defer browserFetcher.Close()

    // Static fetcher as fallback (will escalate on auto mode).
    staticFetcher := fetch.NewStealth(fetch.WithIdentity(profile))
    defer staticFetcher.Close()

    smart := fetch.NewSmart(staticFetcher, browserFetcher)

    jsonlWriter, _ := export.NewJSON("maps.jsonl", export.JSONLines)
    defer jsonlWriter.Close()

    processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
        doc, err := parse.NewDocument(resp)
        if err != nil {
            return &foxhound.Result{}, nil
        }

        item := foxhound.NewItem()
        item.URL = resp.URL

        // Extract from rendered DOM (available after JS execution)
        item.Set("name", doc.Text("h1.DUwDvf"))
        item.Set("rating", doc.Text("span.MW4etd"))
        item.Set("reviews", doc.Text("span.UY7F9"))
        item.Set("address", doc.Text("button[data-item-id='address'] div.fontBodyMedium"))
        item.Set("phone", doc.Text("button[data-item-id*='phone'] div.fontBodyMedium"))

        return &foxhound.Result{Items: []*foxhound.Item{item}}, nil
    })

    h := engine.NewHunt(engine.HuntConfig{
        Name:            "google-maps-villas",
        Domain:          "www.google.com",
        Walkers:         1, // Maps rate-limits aggressively; keep concurrency low
        Fetcher:         smart,
        Processor:       processor,
        Queue:           queue.NewMemoryQueue(),
        Writers:         []foxhound.Writer{jsonlWriter},
        BehaviorProfile: "careful",
        Seeds: []*foxhound.Job{
            {
                ID:        "villa-1",
                URL:       "https://www.google.com/maps/place/Villa+Example/...",
                FetchMode: foxhound.FetchBrowser,
                Priority:  foxhound.PriorityNormal,
            },
        },
    })

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()
    h.Run(ctx)
}
```

## Alibaba Product Scraping

Alibaba uses bot detection that typically requires browser escalation on initial load, then may allow static fetching for subsequent pages.

```go
package main

import (
    "context"
    "strings"
    "time"

    foxhound "github.com/sadewadee/foxhound"
    "github.com/sadewadee/foxhound/engine"
    "github.com/sadewadee/foxhound/fetch"
    "github.com/sadewadee/foxhound/identity"
    "github.com/sadewadee/foxhound/middleware"
    "github.com/sadewadee/foxhound/parse"
    "github.com/sadewadee/foxhound/pipeline/export"
    "github.com/sadewadee/foxhound/queue"
)

func main() {
    profile := identity.Generate(
        identity.WithBrowser(identity.BrowserChrome),
        identity.WithOS(identity.OSWindows),
        identity.WithCountry("US"),
    )

    staticFetcher := fetch.NewStealth(fetch.WithIdentity(profile))
    defer staticFetcher.Close()

    browserFetcher, _ := fetch.NewCamoufox(
        fetch.WithBrowserIdentity(profile),
        fetch.WithHeadless("virtual"),
        fetch.WithBlockImages(true),
    )
    defer browserFetcher.Close()

    smart := fetch.NewSmart(staticFetcher, browserFetcher)

    jsonlWriter, _ := export.NewJSON("alibaba.jsonl", export.JSONLines)
    defer jsonlWriter.Close()

    mws := []foxhound.Middleware{
        middleware.NewRateLimit(1.0, 2),           // conservative: 1 req/s
        middleware.NewDedup(),
        middleware.NewCookies(),
        middleware.NewReferer(),
        middleware.NewRedirect(10),
        middleware.NewRetry(3, 2*time.Second),
    }

    processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
        if resp.StatusCode == 0 {
            return &foxhound.Result{}, nil // deduped
        }
        doc, err := parse.NewDocument(resp)
        if err != nil {
            return &foxhound.Result{}, nil
        }

        item := foxhound.NewItem()
        item.URL = resp.URL
        item.Set("title", strings.TrimSpace(doc.Text(".product-title-text")))
        item.Set("price", strings.TrimSpace(doc.Text(".price")))
        item.Set("supplier", strings.TrimSpace(doc.Text(".supplier-name-link")))
        item.Set("min_order", strings.TrimSpace(doc.Text(".moq")))

        return &foxhound.Result{Items: []*foxhound.Item{item}}, nil
    })

    h := engine.NewHunt(engine.HuntConfig{
        Name:            "alibaba-products",
        Domain:          "www.alibaba.com",
        Walkers:         2,
        Fetcher:         smart,
        Processor:       processor,
        Queue:           queue.NewMemoryQueue(),
        Writers:         []foxhound.Writer{jsonlWriter},
        Middlewares:     mws,
        BehaviorProfile: "careful",
        Seeds: []*foxhound.Job{
            {
                ID:        "search-1",
                URL:       "https://www.alibaba.com/trade/search?SearchText=laptop",
                FetchMode: foxhound.FetchAuto,  // try static, escalate if blocked
                Priority:  foxhound.PriorityHigh,
            },
        },
    })

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
    defer cancel()
    h.Run(ctx)
}
```

## Custom Processor Patterns

### Dispatching on domain

```go
type MultiDomainProcessor struct{}

func (p *MultiDomainProcessor) Process(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    switch resp.Job.Domain {
    case "books.toscrape.com":
        return p.parseBooks(ctx, resp)
    case "quotes.toscrape.com":
        return p.parseQuotes(ctx, resp)
    default:
        return &foxhound.Result{}, nil
    }
}
```

### Returning new jobs (crawling)

```go
processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    doc, _ := parse.NewDocument(resp)

    var jobs []*foxhound.Job

    // Discover product links.
    doc.Each("a.product-link", func(i int, s *goquery.Selection) {
        href, exists := s.Attr("href")
        if !exists {
            return
        }
        jobs = append(jobs, &foxhound.Job{
            URL:       resolveURL(resp.URL, href),
            FetchMode: foxhound.FetchAuto,
            Priority:  foxhound.PriorityNormal,
            Depth:     resp.Job.Depth + 1,
            Domain:    resp.Job.Domain,
            Meta:      map[string]any{"type": "product"},
        })
    })

    // Only extract data from product pages (using Meta from parent).
    var items []*foxhound.Item
    if resp.Job.Meta["type"] == "product" {
        item := foxhound.NewItem()
        item.URL = resp.URL
        item.Set("title", doc.Text("h1.product-title"))
        items = append(items, item)
    }

    return &foxhound.Result{Items: items, Jobs: jobs}, nil
})
```

### Using job Meta for context passing

```go
// Seed with metadata:
seeds := []*foxhound.Job{
    {
        URL:      "https://example.com/category/electronics",
        Meta:     map[string]any{"category": "electronics"},
        Priority: foxhound.PriorityHigh,
    },
}

// In processor, read metadata and pass to items:
processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    category := ""
    if resp.Job.Meta != nil {
        category, _ = resp.Job.Meta["category"].(string)
    }

    item := foxhound.NewItem()
    item.Set("category", category)
    // ...

    return &foxhound.Result{Items: []*foxhound.Item{item}}, nil
})
```
