# Examples

## E-commerce (books.toscrape.com)

Full working example at `examples/ecommerce/main.go`.

```bash
go run ./examples/ecommerce/
```

Scrapes book titles, prices, ratings, and URLs. Follows pagination across all 50 catalogue pages. Exports to `books.csv`.

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

## Streaming Mode

Use `Stream` or `StreamWithStats` when you need items as they arrive:

```go
h := engine.NewHunt(engine.HuntConfig{
    // ... same config as above
})

// Option A: plain item stream
ch, err := h.Stream(ctx)
if err != nil {
    log.Fatal(err)
}
for item := range ch {
    // Process each item as it arrives — ch is closed when hunt finishes
    fmt.Printf("title=%s price=%s\n",
        item.GetString("title"),
        item.GetString("price"),
    )
}

// Option B: items + periodic stats
events, err := h.StreamWithStats(ctx, 10*time.Second)
if err != nil {
    log.Fatal(err)
}
for event := range events {
    if event.Item != nil {
        fmt.Println(event.Item.GetString("title"))
    }
    if event.Stats != nil {
        fmt.Printf("[stats] items=%d errors=%d\n",
            event.Stats.ItemCount.Load(),
            event.Stats.ErrorCount.Load(),
        )
    }
}
```

The channel is buffered (100 items). Use `HuntConfig.Writers` for durable output alongside streaming.

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

    browserFetcher, err := fetch.NewCamoufox(
        fetch.WithBrowserIdentity(profile),
        fetch.WithHeadless("virtual"),
        fetch.WithBlockImages(false), // Maps needs images for loading detection
        fetch.WithBrowserTimeout(30*time.Second),
    )
    if err != nil {
        log.Fatalf("camoufox init: %v", err)
    }
    defer browserFetcher.Close()

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
        item.Set("name", doc.Text("h1.DUwDvf"))
        item.Set("rating", doc.Text("span.MW4etd"))
        item.Set("reviews", doc.Text("span.UY7F9"))
        item.Set("address", doc.Text("button[data-item-id='address'] div.fontBodyMedium"))
        item.Set("phone", doc.Text("button[data-item-id*='phone'] div.fontBodyMedium"))

        return &foxhound.Result{Items: []*foxhound.Item{item}}, nil
    })

    h := engine.NewHunt(engine.HuntConfig{
        Name:            "google-maps",
        Domain:          "www.google.com",
        Walkers:         1, // Maps rate-limits aggressively; keep concurrency low
        Fetcher:         smart,
        Processor:       processor,
        Queue:           queue.NewMemoryQueue(),
        Writers:         []foxhound.Writer{jsonlWriter},
        BehaviorProfile: "careful",
        Seeds: []*foxhound.Job{
            {
                ID:        "place-1",
                URL:       "https://www.google.com/maps/place/...",
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

## Maps to Website to Contacts Pipeline

A three-stage pipeline: discover businesses on Maps, visit each website, extract contact details.

```go
processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    doc, _ := parse.NewDocument(resp)
    var items []*foxhound.Item
    var jobs []*foxhound.Job

    switch {
    case resp.Job.Meta["stage"] == "maps":
        // Stage 1: extract website URL from Maps listing
        website := doc.Attr("a[data-item-id='authority']", "href")
        if website != "" {
            jobs = append(jobs, &foxhound.Job{
                URL:       website,
                FetchMode: foxhound.FetchStatic,
                Priority:  foxhound.PriorityNormal,
                Meta: map[string]any{
                    "stage":       "website",
                    "maps_name":   resp.Job.Meta["maps_name"],
                },
            })
        }

    case resp.Job.Meta["stage"] == "website":
        // Stage 2: extract contact info
        item := foxhound.NewItem()
        item.URL = resp.URL
        item.Set("business", resp.Job.Meta["maps_name"])
        item.Set("phone", doc.Text("a[href^='tel:']"))
        item.Set("email", doc.Text("a[href^='mailto:']"))
        item.Set("website", resp.URL)

        // Stage 3: look for /contact page
        contactHref := doc.Attr("a[href*='contact']", "href")
        if contactHref != "" && !item.Has("email") {
            jobs = append(jobs, &foxhound.Job{
                URL:       resolveURL(resp.URL, contactHref),
                FetchMode: foxhound.FetchStatic,
                Meta: map[string]any{
                    "stage":     "contact",
                    "business":  item.GetString("business"),
                },
            })
        } else if item.Has("phone") || item.Has("email") {
            items = append(items, item)
        }

    case resp.Job.Meta["stage"] == "contact":
        item := foxhound.NewItem()
        item.Set("business", resp.Job.Meta["business"])
        item.Set("email", doc.Text("a[href^='mailto:']"))
        item.Set("phone", doc.Text("a[href^='tel:']"))
        items = append(items, item)
    }

    return &foxhound.Result{Items: items, Jobs: jobs}, nil
})
```

## Adaptive Parsing Example

Adaptive selectors survive page redesigns by falling back to similarity matching:

```go
import "github.com/sadewadee/foxhound/parse"

// Create extractor, loading previously saved signatures
ae := parse.NewAdaptiveExtractor("selectors.json")
ae.Register("price", "span.price-current")
ae.Register("title", "h1.product-name")
ae.Register("rating", "div.rating-stars")

processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    doc, err := parse.NewDocument(resp)
    if err != nil {
        return nil, err
    }

    item := foxhound.NewItem()
    item.URL = resp.URL

    // CSS selector tried first; falls back to ElementSignature similarity if no match
    item.Set("title", ae.ExtractText(doc, "title"))
    item.Set("price", ae.ExtractText(doc, "price"))
    item.Set("rating", ae.ExtractText(doc, "rating"))

    // Persist updated signatures for the next run
    _ = ae.Save()

    return &foxhound.Result{Items: []*foxhound.Item{item}}, nil
})
```

When the site changes its CSS classes, the similarity-based fallback finds the element that best matches the saved signature (tag, classes, text, parent tag, position, depth). On successful extraction the signature is updated.

## Alibaba Product Scraping

Uses `FetchAuto` mode (static first, escalates to browser on block) with conservative rate limiting:

```go
profile := identity.Generate(
    identity.WithBrowser(identity.BrowserChrome),
    identity.WithOS(identity.OSWindows),
    identity.WithCountry("US"),
)

staticFetcher := fetch.NewStealth(fetch.WithIdentity(profile))
browserFetcher, _ := fetch.NewCamoufox(
    fetch.WithBrowserIdentity(profile),
    fetch.WithHeadless("virtual"),
    fetch.WithBlockImages(true),
)

smart := fetch.NewSmart(staticFetcher, browserFetcher)

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
            FetchMode: foxhound.FetchAuto,
            Priority:  foxhound.PriorityHigh,
        },
    },
})
```

## Trail API — Google Maps Search

The Trail API provides a fluent builder for browser-based navigation. This example searches Google Maps and scrolls through results:

```go
trail := engine.NewTrail("maps").
    Navigate("https://www.google.com/maps").
    Fill("input#searchboxinput", "cafe in canggu").
    Click("button#searchbox-searchbutton").
    Wait("div[role='feed']", 10*time.Second).
    InfiniteScrollInUntil("div[role='feed']", "div.Nv2PK", 20, 100)

result, err := trail.Run(ctx, browserFetcher)
if err != nil {
    log.Fatal(err)
}

// Extract from the final page state
doc, _ := parse.NewDocument(result.Response)
doc.Each("div.Nv2PK", func(i int, s *goquery.Selection) {
    item := foxhound.NewItem()
    item.Set("name", s.Find("div.qBF1Pd").Text())
    item.Set("rating", s.Find("span.MW4etd").Text())
    item.Set("reviews", s.Find("span.UY7F9").Text())
    // ...
})
```

## Metadata Extraction (JSON-LD)

Extract structured data from JSON-LD `<script>` tags embedded in pages:

```go
processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    doc, _ := parse.NewDocument(resp)

    // ExtractJSONLD parses all <script type="application/ld+json"> blocks
    jsonld := doc.ExtractJSONLD()
    for _, ld := range jsonld {
        item := foxhound.NewItem()
        item.URL = resp.URL
        item.Set("type", ld["@type"])
        item.Set("name", ld["name"])
        item.Set("description", ld["description"])
        // ...
    }
    return &foxhound.Result{Items: items}, nil
})
```

## Contact Extraction (Emails with CloudFlare cfemail)

Extract email addresses from pages, including those obfuscated by CloudFlare's email protection:

```go
processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    doc, _ := parse.NewDocument(resp)

    // ExtractEmails finds mailto: links and decodes CloudFlare cfemail-protected addresses
    emails := doc.ExtractEmails()

    item := foxhound.NewItem()
    item.URL = resp.URL
    item.Set("emails", strings.Join(emails, ", "))
    return &foxhound.Result{Items: []*foxhound.Item{item}}, nil
})
```

`ExtractEmails` handles three sources: `mailto:` href links, plaintext email patterns, and CloudFlare `data-cfemail` encoded spans.

## XHR Capture

Capture XHR/fetch responses made by the page during browser-mode navigation. Useful for extracting API data that populates dynamic content:

```go
browserFetcher, _ := fetch.NewCamoufox(
    fetch.WithBrowserIdentity(profile),
    fetch.WithNetworkCapture(true),
)

processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    var items []*foxhound.Item

    // resp.CapturedRequests contains all XHR/fetch responses captured during page load
    for _, req := range resp.CapturedRequests {
        if strings.Contains(req.URL, "/api/listings") {
            var listings []map[string]any
            json.Unmarshal(req.Body, &listings)
            for _, l := range listings {
                item := foxhound.NewItem()
                item.Set("name", l["name"])
                item.Set("price", l["price"])
                items = append(items, item)
            }
        }
    }

    return &foxhound.Result{Items: items}, nil
})
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

// In processor, read and propagate metadata:
processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    category := ""
    if resp.Job.Meta != nil {
        category, _ = resp.Job.Meta["category"].(string)
    }

    item := foxhound.NewItem()
    item.Set("category", category)

    return &foxhound.Result{Items: []*foxhound.Item{item}}, nil
})
```
