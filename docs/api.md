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

// Set and get fields:
item.Set("title", "Go Programming")
val, ok := item.Get("title")
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
    // your implementation
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

Use `ProcessorFunc` to avoid defining a named type for simple cases:

```go
processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    // extract items, return next jobs
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
        // pre-processing
        resp, err := next.Fetch(ctx, job)
        // post-processing
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
    // 1. Generate identity.
    profile := identity.Generate(
        identity.WithBrowser(identity.BrowserFirefox),
        identity.WithOS(identity.OSLinux),
    )

    // 2. Build the fetcher.
    fetcher := fetch.NewStealth(fetch.WithIdentity(profile))
    defer fetcher.Close()

    // 3. Build the queue.
    q := queue.NewMemoryQueue()
    defer q.Close()

    // 4. Build writers.
    w, _ := export.NewJSON("output.jsonl", export.JSONLines)
    defer w.Close()

    // 5. Build middleware chain.
    mws := []foxhound.Middleware{
        middleware.NewRateLimit(2.0, 5),
        middleware.NewDedup(),
        middleware.NewCookies(),
        middleware.NewRetry(3, 500*time.Millisecond),
    }

    // 6. Build pipeline.
    pipelineStages := []foxhound.Pipeline{
        &pipeline.Validate{Required: []string{"title", "url"}},
        &pipeline.Clean{TrimWhitespace: true},
    }

    // 7. Wire the hunt.
    h := engine.NewHunt(engine.HuntConfig{
        Name:            "example",
        Domain:          "example.com",
        Walkers:         4,
        Fetcher:         fetcher,
        Processor:       myProcessor,
        Queue:           q,
        Writers:         []foxhound.Writer{w},
        Middlewares:     mws,
        Pipelines:       pipelineStages,
        BehaviorProfile: "moderate",
        Seeds: []*foxhound.Job{{
            ID:        "seed",
            URL:       "https://example.com",
            FetchMode: foxhound.FetchAuto,
            Priority:  foxhound.PriorityHigh,
        }},
    })

    // 8. Run with context.
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
    Name            string               // human-readable label for logs/metrics
    Domain          string               // primary target domain
    Walkers         int                  // concurrent walker goroutines
    Seeds           []*foxhound.Job      // initial jobs pushed before walkers start
    Processor       foxhound.Processor   // required: user response handler
    Fetcher         foxhound.Fetcher     // required: base fetcher (before middleware)
    Queue           foxhound.Queue       // required: job storage backend
    Pipelines       []foxhound.Pipeline  // applied to each Item in order
    Writers         []foxhound.Writer    // receive items that survive the pipeline
    Middlewares     []foxhound.Middleware // wrapped outermost-first
    BehaviorProfile string               // "careful" | "moderate" | "aggressive"
}
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

### Profile Fields

```go
type Profile struct {
    UA          string          // e.g. "Mozilla/5.0 (Windows NT 10.0...) Firefox/134.0"
    BrowserName identity.Browser
    BrowserVer  string
    TLSProfile  string          // e.g. "firefox_134.0"
    HeaderOrder []string        // canonical header order for this browser
    OS          identity.OS
    OSVersion   string
    Platform    string          // "Win32" | "MacIntel" | "Linux x86_64"
    Cores       int
    Memory      float64
    GPU         string
    ScreenW     int
    ScreenH     int
    ColorDepth  int
    PixelRatio  float64
    Languages   []string
    Timezone    string
    Locale      string
    Lat         float64
    Lng         float64
    CamoufoxEnv map[string]string  // CAMOU_CONFIG_* env vars for browser mode
}
```

## Custom Processor

For multi-domain crawlers, implement the `Processor` interface and dispatch on the job domain:

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

func (s *MyScraper) scrapeBooks(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    doc, err := parse.NewDocument(resp)
    if err != nil {
        return nil, err
    }
    var items []*foxhound.Item
    doc.Each("article.product_pod", func(i int, s *goquery.Selection) {
        item := foxhound.NewItem()
        title, _ := s.Find("h3 a").Attr("title")
        item.Set("title", title)
        item.URL = resp.URL
        items = append(items, item)
    })
    return &foxhound.Result{Items: items}, nil
}
```

## Custom Pipeline Stage

```go
type PriceFilterStage struct {
    MinPrice float64
}

func (p *PriceFilterStage) Process(ctx context.Context, item *foxhound.Item) (*foxhound.Item, error) {
    raw, ok := item.Get("price")
    if !ok {
        return nil, nil  // drop items without a price field
    }
    priceStr := fmt.Sprintf("%v", raw)
    // Strip currency symbol and parse.
    priceStr = strings.TrimPrefix(priceStr, "$")
    price, err := strconv.ParseFloat(priceStr, 64)
    if err != nil {
        return nil, nil  // drop unparseable prices
    }
    if price < p.MinPrice {
        return nil, nil  // drop items below minimum price
    }
    item.Set("price_float", price)
    return item, nil
}
```

## Custom Writer

```go
type MyDBWriter struct {
    db *sql.DB
}

func (w *MyDBWriter) Write(ctx context.Context, item *foxhound.Item) error {
    title, _ := item.Get("title")
    url, _ := item.Get("url")
    _, err := w.db.ExecContext(ctx,
        "INSERT INTO items (title, url, scraped_at) VALUES ($1, $2, $3)",
        title, url, item.Timestamp,
    )
    return err
}

func (w *MyDBWriter) Flush(_ context.Context) error { return nil }
func (w *MyDBWriter) Close() error                  { return w.db.Close() }
```

## Custom Middleware

```go
type HeaderMiddleware struct {
    headers map[string]string
}

func (m *HeaderMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
    return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
        // Clone job headers to avoid mutating shared state.
        cloned := *job
        cloned.Headers = job.Headers.Clone()
        if cloned.Headers == nil {
            cloned.Headers = make(http.Header)
        }
        for k, v := range m.headers {
            cloned.Headers.Set(k, v)
        }
        return next.Fetch(ctx, &cloned)
    })
}
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

// Custom block detector:
smart = fetch.NewSmart(staticFetcher, browserFetcher,
    fetch.WithBlockDetector(myDetector),
)
```

## Config Loading

```go
cfg, err := foxhound.LoadConfig("config.yaml")
// cfg is *foxhound.Config with all defaults applied
```

`LoadConfig` expands `${ENV_VAR}` throughout the file before parsing.
