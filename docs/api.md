# Go API

Module path: `github.com/sadewadee/foxhound`

## Core Types

All types are defined in the root `foxhound` package (`foxhound.go`).

### Job

A unit of work consumed from the queue by a walker.

```go
type Job struct {
    ID         string          // unique identifier
    URL        string          // target URL to fetch
    Method     string          // HTTP method (default "GET")
    Headers    http.Header     // additional request headers
    Body       []byte          // request body for POST/PUT
    FetchMode  FetchMode       // FetchAuto | FetchStatic | FetchBrowser
    Priority   Priority        // PriorityLow(0) | PriorityNormal(5) | PriorityHigh(10)
    MaxRetries int             // overrides default retry count
    Meta       map[string]any  // arbitrary metadata passed through the pipeline
    Depth      int             // crawl depth from the seed URL
    Domain     string          // target domain (extracted from URL)
    CreatedAt  time.Time       // creation timestamp
}
```

**FetchMode constants:**

```go
FetchAuto    // smart router: tries static, escalates to browser on block
FetchStatic  // forces TLS-impersonating HTTP client
FetchBrowser // forces Camoufox browser
```

**Priority constants:**

```go
PriorityLow    Priority = 0
PriorityNormal Priority = 5
PriorityHigh   Priority = 10
```

### Response

Wraps an HTTP response with metadata.

```go
type Response struct {
    StatusCode int           // HTTP status code
    Headers    http.Header   // response headers
    Body       []byte        // response body bytes
    URL        string        // final URL after redirects
    FetchMode  FetchMode     // which fetcher was used
    Duration   time.Duration // fetch duration
    Job        *Job          // the original job
}
```

### Item

A scraped data item flowing through the pipeline.

```go
type Item struct {
    Fields    map[string]any  // extracted key-value data
    Meta      map[string]any  // metadata from the originating job
    URL       string          // source URL
    Timestamp time.Time       // creation time
}

// Create a new item with initialized maps:
item := foxhound.NewItem()
```

**Item methods:**

```go
// Set and get fields:
item.Set("title", "Go Programming")
val, ok := item.Get("title")

// Type-safe getters:
s := item.GetString("title")          // "" if absent or not string
f := item.GetFloat("price")           // 0 if absent or non-numeric
n := item.GetInt("count")             // 0 if absent or non-numeric

// Presence check (also returns false for nil and "" values):
if item.Has("email") { ... }

// Sorted field keys:
keys := item.Keys() // []string, alphabetically sorted

// Serialisation:
data, err := item.ToJSON()            // compact JSON bytes
data, err := item.ToJSONPretty()      // indented JSON bytes
m := item.ToMap()                     // shallow copy of Fields
row := item.ToCSVRow([]string{"title", "price"}) // []string in column order

// Text representations:
md := item.ToMarkdown()  // "- **firstVal** — val2 — val3"
txt := item.ToText()     // "key: value\nkey2: value2"
str := item.String()     // compact JSON (fallback to ToText on error)
```

### Result

The output of a `Processor.Process` call.

```go
type Result struct {
    Items []*Item  // extracted data items
    Jobs  []*Job   // new jobs to enqueue (pagination, discovered links)
}
```

## Interfaces

### Fetcher

```go
type Fetcher interface {
    Fetch(ctx context.Context, job *Job) (*Response, error)
    Close() error
}
```

Use `FetcherFunc` to adapt a plain function:

```go
var f foxhound.Fetcher = foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
    return resp, nil
})
```

### Processor

The main user extension point. Implement this to extract data from responses.

```go
type Processor interface {
    Process(ctx context.Context, resp *Response) (*Result, error)
}
```

Use `ProcessorFunc` to avoid defining a named type:

```go
processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    return &foxhound.Result{Items: items, Jobs: nextJobs}, nil
})
```

### Pipeline

Processes an item after extraction. Return `nil` to drop the item.

```go
type Pipeline interface {
    Process(ctx context.Context, item *Item) (*Item, error)
}
```

Use `PipelineFunc` for inline stages:

```go
stage := foxhound.PipelineFunc(func(ctx context.Context, item *foxhound.Item) (*foxhound.Item, error) {
    if item.Fields["price"] == "" {
        return nil, nil  // drop item
    }
    return item, nil
})
```

### Middleware

Wraps a Fetcher to add cross-cutting behaviour.

```go
type Middleware interface {
    Wrap(next Fetcher) Fetcher
}
```

Use `MiddlewareFunc` for inline middleware:

```go
mw := foxhound.MiddlewareFunc(func(next foxhound.Fetcher) foxhound.Fetcher {
    return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
        resp, err := next.Fetch(ctx, job)
        return resp, err
    })
})
```

### Queue

```go
type Queue interface {
    Push(ctx context.Context, job *Job) error
    Pop(ctx context.Context) (*Job, error)  // blocks until job available or ctx done
    Len() int
    Close() error
}
```

### Writer

```go
type Writer interface {
    Write(ctx context.Context, item *Item) error
    Flush(ctx context.Context) error
    Close() error
}
```

## Creating a Hunt

`engine.NewHunt` takes a `HuntConfig` struct. All fields are explicit — there are no builder methods.

```go
import (
    "context"
    "time"

    foxhound "github.com/sadewadee/foxhound"
    "github.com/sadewadee/foxhound/engine"
    "github.com/sadewadee/foxhound/fetch"
    "github.com/sadewadee/foxhound/identity"
    "github.com/sadewadee/foxhound/middleware"
    "github.com/sadewadee/foxhound/pipeline"
    "github.com/sadewadee/foxhound/pipeline/export"
    "github.com/sadewadee/foxhound/queue"
)

func main() {
    profile := identity.Generate(
        identity.WithBrowser(identity.BrowserFirefox),
        identity.WithOS(identity.OSLinux),
    )

    fetcher := fetch.NewStealth(fetch.WithIdentity(profile))
    defer fetcher.Close()

    q := queue.NewMemoryQueue()
    defer q.Close()

    w, _ := export.NewJSON("output.jsonl", export.JSONLines)
    defer w.Close()

    mws := []foxhound.Middleware{
        middleware.NewConcurrency(2),
        middleware.NewRateLimit(2.0, 5),
        middleware.NewDedup(),
        middleware.NewCookies(),
        middleware.NewRetry(3, 500*time.Millisecond),
    }

    pipelineStages := []foxhound.Pipeline{
        &pipeline.Validate{Required: []string{"title", "url"}},
        &pipeline.Clean{TrimWhitespace: true},
    }

    h := engine.NewHunt(engine.HuntConfig{
        Name:            "example",
        Domain:          "example.com",
        Walkers:         4,
        MaxConcurrency:  8,
        Fetcher:         fetcher,
        Processor:       myProcessor,
        Queue:           q,
        Writers:         []foxhound.Writer{w},
        Middlewares:     mws,
        Pipelines:       pipelineStages,
        BehaviorProfile: "moderate",
        Checkpoint: engine.CheckpointConfig{
            Enabled:  true,
            Path:     "/tmp/example.checkpoint.json",
            Interval: 100,
        },
        Seeds: []*foxhound.Job{{
            ID:        "seed",
            URL:       "https://example.com",
            FetchMode: foxhound.FetchAuto,
            Priority:  foxhound.PriorityHigh,
        }},
    })

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
    defer cancel()

    if err := h.Run(ctx); err != nil {
        panic(err)
    }
    fmt.Println(h.Stats().Summary())
}
```

### HuntConfig Fields

```go
type HuntConfig struct {
    Name            string                // human-readable label for logs/metrics
    Domain          string                // primary target domain
    Walkers         int                   // concurrent walker goroutines
    MaxConcurrency  int                   // global cap on in-flight requests (0 = walkers)
    Seeds           []*foxhound.Job       // initial jobs pushed before walkers start
    Processor       foxhound.Processor    // required: user response handler
    Fetcher         foxhound.Fetcher      // required: base fetcher (before middleware)
    Queue           foxhound.Queue        // required: job storage backend
    Pipelines       []foxhound.Pipeline   // applied to each Item in order
    Writers         []foxhound.Writer     // receive items that survive the pipeline
    Middlewares     []foxhound.Middleware  // wrapped outermost-first
    BehaviorProfile string                // "careful" | "moderate" | "aggressive"
    Checkpoint      engine.CheckpointConfig // optional: auto-save state
}
```

## Streaming API

Use `Stream` when you want items as they arrive, without waiting for the hunt to finish:

```go
ch, err := hunt.Stream(ctx)
if err != nil {
    log.Fatal(err)
}
for item := range ch {
    fmt.Println(item.GetString("title"))
}
// ch is closed when the hunt completes
```

Use `StreamWithStats` to also receive periodic stats snapshots:

```go
events, err := hunt.StreamWithStats(ctx, 5*time.Second) // stats every 5s
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

`StreamEvent` type:

```go
type StreamEvent struct {
    Item  *foxhound.Item // non-nil for item events
    Stats *Stats         // non-nil for stats events
}
```

The item channel is buffered (100 items). Items are dropped with a warning log when the buffer is full — keep consumers fast or use `HuntConfig.Writers` for durable output.

## CheckpointConfig

```go
type CheckpointConfig struct {
    Enabled  bool   // turn auto-checkpointing on
    Path     string // file path for the JSON checkpoint
    Interval int    // save every N items processed (default 100 when 0)
}
```

The checkpoint file is written atomically. Load it with:

```go
cp, err := engine.LoadCheckpoint("/tmp/hunt.checkpoint.json")
// cp.HuntName, cp.Domain, cp.ItemsProcessed, cp.RequestsDone,
// cp.ErrorCount, cp.LastURL, cp.Timestamp, cp.QueueLen, cp.ElapsedMs
```

Save on demand:

```go
h.SaveCheckpoint("/tmp/snapshot.json")
```

## Adaptive Parsing

`AdaptiveExtractor` tracks selectors across page structure changes:

```go
import "github.com/sadewadee/foxhound/parse"

ae := parse.NewAdaptiveExtractor("selectors.json") // loads saved signatures
ae.Register("price", "span.price-current")
ae.Register("title", "h1.product-name")

// In processor:
doc, _ := parse.NewDocument(resp)
price := ae.ExtractText(doc, "price")   // tries CSS first, falls back to similarity
title := ae.ExtractText(doc, "title")

ae.Save() // persist updated signatures for next run
```

## Element Type and Document Finders

```go
import "github.com/sadewadee/foxhound/parse"

doc, _ := parse.NewDocument(resp)

// CSS selectors:
el := doc.First("h1.title")           // *Element or nil
els := doc.FindAll("article.product") // []*Element

// Text-based finders:
els = doc.FindByText("Buy Now")                    // exact text match
els = doc.FindByTextContains("Add to")             // substring match
els = doc.FindByTextRegex(`\$[\d,.]+`)             // regex match

// Attribute finders:
els = doc.FindByAttr("data-type", "product")       // exact attribute value
els = doc.FindByAttrContains("class", "product")   // attribute contains substring

// Similarity matching (used internally by AdaptiveExtractor):
sig := parse.CaptureSignature(el)
matches := doc.FindSimilar(sig, 0.6) // []SimilarMatch, sorted by Score desc

// Element methods:
el.Text()               // trimmed text content
el.HTML()               // inner HTML
el.Attr("href")         // attribute value
el.Tag()                // lowercase tag name
el.HasClass("active")   // class check
el.Attrs()              // map[string]string of all attributes
el.Children()           // direct child elements
el.Parent()             // parent element or nil
el.Siblings()           // all siblings
el.Next()               // next sibling or nil
el.Prev()               // previous sibling or nil
el.Find("selector")     // all descendants matching selector
el.CSS("selector")      // first descendant matching selector
```

## Identity Generation

```go
import "github.com/sadewadee/foxhound/identity"

// Minimal — random OS, Firefox:
profile := identity.Generate()

// Firefox on Windows, US locale:
profile := identity.Generate(
    identity.WithBrowser(identity.BrowserFirefox),
    identity.WithOS(identity.OSWindows),
    identity.WithLocale("en-US", "en-US", "en"),
    identity.WithTimezone("America/New_York"),
)

// Chrome on macOS:
profile := identity.Generate(
    identity.WithBrowser(identity.BrowserChrome),
    identity.WithOS(identity.OSMacOS),
)

// Constrained by country code:
profile := identity.Generate(
    identity.WithCountry("DE"),
)

// With explicit geo coordinates:
profile := identity.Generate(
    identity.WithGeo(51.5074, -0.1278), // London
)

// With proxy geo-matching:
profile := identity.Generate(
    identity.WithProxy("1.2.3.4"), // IP used to look up timezone/locale
)
```

## Config Loading

```go
cfg, err := foxhound.LoadConfig("config.yaml")
// cfg is *foxhound.Config with all defaults applied
```

`LoadConfig` expands `${ENV_VAR}` throughout the file before parsing.

## Custom Processor

For multi-domain crawlers, implement `Processor` and dispatch on the job domain:

```go
type MyScraper struct{}

func (s *MyScraper) Process(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    switch resp.Job.Domain {
    case "books.toscrape.com":
        return s.scrapeBooks(ctx, resp)
    case "quotes.toscrape.com":
        return s.scrapeQuotes(ctx, resp)
    default:
        return &foxhound.Result{}, nil
    }
}
```

## Custom Writer

```go
type MyDBWriter struct {
    db *sql.DB
}

func (w *MyDBWriter) Write(ctx context.Context, item *foxhound.Item) error {
    _, err := w.db.ExecContext(ctx,
        "INSERT INTO items (title, url, scraped_at) VALUES ($1, $2, $3)",
        item.GetString("title"), item.GetString("url"), item.Timestamp,
    )
    return err
}

func (w *MyDBWriter) Flush(_ context.Context) error { return nil }
func (w *MyDBWriter) Close() error                  { return w.db.Close() }
```

## SmartFetcher

Route requests between static and browser based on FetchMode or block detection:

```go
import "github.com/sadewadee/foxhound/fetch"

staticFetcher := fetch.NewStealth(fetch.WithIdentity(profile))

// Optional: browser fetcher (requires -tags playwright build)
browserFetcher, _ := fetch.NewCamoufox(
    fetch.WithBrowserIdentity(profile),
    fetch.WithHeadless("virtual"),
    fetch.WithBlockImages(true),
)

// SmartFetcher: static first, escalates to browser on 401/403/407/429/503
smart := fetch.NewSmart(staticFetcher, browserFetcher)
```

## Trail API

`engine.Trail` is a fluent builder for multi-step browser navigation sequences. Each step is queued and executed in order when the trail runs.

```go
import (
    "time"
    "github.com/sadewadee/foxhound/engine"
)

trail := engine.NewTrail("product-search").
    Navigate("https://example.com").
    Fill("input#search", "query").
    Click("button.submit").
    Wait("div.results", 10*time.Second).
    ClickOptional("button.cookie-dismiss").
    WaitOptional("div.popup", 3*time.Second).
    InfiniteScroll(50).
    InfiniteScrollIn("div.panel", 50).
    InfiniteScrollUntil("div.item", 20, 100).
    InfiniteScrollInUntil("div.panel", "div.item", 20, 100).
    Evaluate("() => document.title").
    Paginate("a.next", 10).
    LoadMore("button.more", 20)
```

| Method | Description |
|--------|-------------|
| `Navigate(url)` | Navigate the browser to the given URL. |
| `Fill(selector, value)` | Type `value` into the element matching `selector`. |
| `Click(selector)` | Click the element. Fails if not found. |
| `ClickOptional(selector)` | Click if element exists, skip silently otherwise. |
| `Wait(selector, timeout)` | Wait for element to appear, fail on timeout. |
| `WaitOptional(selector, timeout)` | Wait for element, skip if timeout expires. |
| `InfiniteScroll(maxScrolls)` | Scroll the page until no new content or `maxScrolls` reached. |
| `InfiniteScrollIn(container, maxScrolls)` | Scroll within a specific container element. |
| `InfiniteScrollUntil(itemSelector, minItems, maxScrolls)` | Scroll until `minItems` matching elements exist. |
| `InfiniteScrollInUntil(container, itemSelector, minItems, maxScrolls)` | Scroll in container until `minItems` found. |
| `Evaluate(js)` | Run JavaScript in the page. Result stored in `Response.StepResults`. |
| `Paginate(nextSelector, maxPages)` | Click a "next" link repeatedly, up to `maxPages`. |
| `LoadMore(buttonSelector, maxClicks)` | Click a "load more" button repeatedly. |

Use a trail as the processor for a hunt by passing it to `engine.HuntConfig.Trail`.

## Response.StepResults

When a trail includes `Evaluate` steps, their return values are stored in `Response.StepResults`:

```go
// StepResults is map[string]any, keyed by "step_N" (0-indexed position in the trail)
resp.StepResults["step_4"] // result of the 5th step (an Evaluate call)
```

## Response.CapturedXHR

Captures matching XHR/fetch responses when `WithCaptureXHR` patterns are configured on the Camoufox fetcher:

```go
fetcher, _ := fetch.NewCamoufox(
    fetch.WithCaptureXHR("*/api/products*", "*/graphql*"),
)

// After fetch, resp.CapturedXHR contains matching network responses:
// []map[string]any with keys: "url", "status", "headers", "body"
for _, xhr := range resp.CapturedXHR {
    fmt.Println(xhr["url"], xhr["status"])
    bodyJSON := xhr["body"] // raw response body as string
}
```

## parse/metadata

Extract structured metadata from HTML documents.

```go
import "github.com/sadewadee/foxhound/parse"

doc, _ := parse.NewDocument(resp)

// JSON-LD structured data (returns []map[string]any)
jsonLD := parse.ExtractJSONLD(doc)

// Open Graph tags (returns map[string]string: "og:title" -> value)
og := parse.ExtractOpenGraph(doc)

// All <meta> tags (returns map[string]string keyed by name or property)
meta := parse.ExtractMeta(doc)

// Next.js __NEXT_DATA__ payload (returns map[string]any)
nextData := parse.ExtractNextData(doc)

// Nuxt.js __NUXT_DATA__ payload (returns map[string]any)
nuxtData := parse.ExtractNuxtData(doc)
```

## parse/contact

Extract contact information from HTML.

```go
import "github.com/sadewadee/foxhound/parse"

doc, _ := parse.NewDocument(resp)

// Decode Cloudflare email protection (data-cfemail attributes)
email := parse.DecodeCFEmail("abc123def") // returns decoded email string

// Extract all email addresses from document text and mailto: links
emails := parse.ExtractEmails(doc) // []string

// Extract phone numbers from document text and tel: links
phones := parse.ExtractPhones(doc) // []string
```

## parse/sitemap

Parse XML sitemaps.

```go
import "github.com/sadewadee/foxhound/parse"

// Parse a sitemap.xml (returns []SitemapURL with Loc, LastMod, Priority, ChangeFreq)
urls, err := parse.ParseSitemap(sitemapBytes)

// Parse a sitemap index (returns []SitemapIndexEntry with Loc, LastMod)
entries, err := parse.ParseSitemapIndex(indexBytes)
```

## parse/feed

Parse RSS and Atom feeds.

```go
import "github.com/sadewadee/foxhound/parse"

// Parse RSS feed (returns *Feed with Title, Link, Items)
feed, err := parse.ParseRSS(rssBytes)

// Parse Atom feed (returns *Feed with Title, Link, Items)
feed, err := parse.ParseAtom(atomBytes)
```

## engine/collect

Helpers for collecting items and metrics from a hunt.

### ItemList

Thread-safe accumulator for scraped items.

```go
import "github.com/sadewadee/foxhound/engine"

list := engine.NewItemList()
list.Append(item)            // add an item (goroutine-safe)
list.Len()                   // number of items
list.Items()                 // []*foxhound.Item snapshot
list.Clear()                 // remove all items

// Serialisation:
jsonBytes, _ := list.ToJSON()    // JSON array
jsonlBytes, _ := list.ToJSONL() // one JSON object per line
csvBytes, _ := list.ToCSV()     // CSV with auto-detected columns
```

### HuntMetrics

Live metrics snapshot from a running or completed hunt.

```go
metrics := hunt.Metrics() // returns engine.HuntMetrics
// Fields: RequestsTotal, RequestsOK, RequestsFailed, RequestsBlocked,
//         ItemsScraped, BytesDownloaded, Elapsed, RequestsPerSec, AvgLatency
```

### HuntResult

Returned by `hunt.RunCollect(ctx)` -- runs the hunt and returns all items plus final metrics.

```go
result, err := hunt.RunCollect(ctx) // engine.HuntResult
items := result.Items               // []*foxhound.Item
metrics := result.Metrics           // engine.HuntMetrics
```

## middleware/cookies_persist

Persistent cookie jar that saves/loads cookies to/from a JSON file.

```go
import "github.com/sadewadee/foxhound/middleware"

cookieMW := middleware.NewPersistentCookies("/tmp/cookies.json")
// Use as middleware -- cookies survive across program restarts.
// The file is loaded on creation and saved after each response.
```

## pipeline/field_transform

Transform individual item fields using regex, renaming, or type coercion.

```go
import "github.com/sadewadee/foxhound/pipeline"

p := pipeline.NewFieldTransformPipeline([]pipeline.FieldTransform{
    {
        Field:        "price",
        RegexFind:    `[\d,.]+`,    // extract numeric portion
    },
    {
        Field:        "price",
        CoerceTo:     "float",      // convert string to float64
    },
    {
        Field:        "old_name",
        RenameTo:     "new_name",   // rename the field
    },
    {
        Field:        "description",
        RegexReplace: []string{`\s+`, " "}, // [pattern, replacement]
    },
})
```

`FieldTransform` fields:

| Field | Type | Description |
|-------|------|-------------|
| `Field` | string | The item field to operate on. |
| `RegexFind` | string | Extract the first regex match, replacing the field value. |
| `RegexReplace` | []string | `[pattern, replacement]` -- regex find-and-replace on the value. |
| `RenameTo` | string | Rename the field key. |
| `CoerceTo` | string | Coerce the value: `"float"`, `"int"`, `"bool"`, `"string"`. |

## proxy Pool.GetForGeo

Select a proxy matching a specific geographic location.

```go
import "github.com/sadewadee/foxhound/proxy"

pool := proxy.NewPool(proxies, proxy.RotatePerSession)

// Get a proxy in a specific country (city is optional, pass "" to match any city)
p, err := pool.GetForGeo("US", "")           // any US proxy
p, err := pool.GetForGeo("DE", "Berlin")     // proxy in Berlin, Germany
```

Returns an error if no proxy matches the requested location.
