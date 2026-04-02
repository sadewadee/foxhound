# Parsing Engine

Foxhound's `parse/` package provides automatic content extraction that goes beyond CSS selectors. Five specialized parsers handle the most common scraping patterns -- HTML tables, JavaScript-embedded data, business directories, paginated articles, and fully automatic page-type detection -- so you can extract structured data without writing boilerplate for each site.

All parsers operate on `*foxhound.Response` and return typed Go structs or `*foxhound.Item` slices ready for the pipeline.

## HTML Table Extraction

The table parser extracts HTML tables into a rectangular `Table` struct with proper colspan/rowspan resolution. A grid-fill algorithm walks every `<tr>`, expanding each cell into the correct region of a pre-allocated grid so that merged cells produce the right values in every column.

### Types

```go
type Table struct {
    Headers []string   // column names from <th> or first row
    Rows    [][]string // data rows (rectangular grid)
    Caption string     // <caption> text if present
}
```

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `ExtractTable` | `(resp *foxhound.Response, selector string) (*Table, error)` | Parse the first table matching `selector` |
| `ExtractTables` | `(resp *foxhound.Response) ([]*Table, error)` | Parse all `<table>` elements on the page |
| `Table.AsItems` | `() []*foxhound.Item` | Convert rows to Items using headers as field keys |
| `Table.Row` | `(i int) []string` | Return row at zero-based index (nil if out of bounds) |
| `Table.Column` | `(name string) []string` | Return all values for a named column |
| `Table.Cell` | `(row int, col string) string` | Return a single cell value |

### Colspan/Rowspan handling

The parser pre-scans all rows to determine the maximum column count, then builds a boolean `filled` grid that tracks cells already occupied by a previous rowspan. When a cell declares `colspan=N` and `rowspan=M`, the cell's text is written into the entire N x M region and those positions are marked filled so subsequent cells skip past them.

### Example

```go
func Process(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    // Extract a specific table by CSS selector.
    table, err := parse.ExtractTable(resp, "table.pricing")
    if err != nil {
        return nil, err
    }
    if table == nil {
        return &foxhound.Result{}, nil // no table found
    }

    // Convert rows directly to Items (each row becomes one Item).
    items := table.AsItems()

    // Or access cells programmatically.
    for i := 0; i < len(table.Rows); i++ {
        price := table.Cell(i, "Price")
        plan := table.Cell(i, "Plan")
        fmt.Printf("%s: %s\n", plan, price)
    }

    // Extract ALL tables from the page.
    tables, _ := parse.ExtractTables(resp)
    for _, t := range tables {
        items = append(items, t.AsItems()...)
    }

    return &foxhound.Result{Items: items}, nil
}
```

## JS Preloaded Data

Modern JavaScript frameworks (Next.js, Nuxt, React, Vue, Angular) embed server-rendered data directly in the page HTML as `window.__VARNAME__ = {...}` assignments or `<script id="__VARNAME__" type="application/json">` blocks. The preload parser extracts this data without running JavaScript, which is faster than browser-mode fetching and avoids API reverse-engineering.

### Extraction strategy

1. **Script tag lookup**: checks for `<script id="__VARNAME__" type="application/json">` and parses the JSON body.
2. **Balanced-brace extraction**: regex-matches `window.__VARNAME__ = ` in the raw HTML, then walks the string character by character tracking brace depth, string escaping, and nesting to extract the complete JSON object. This handles arbitrarily nested structures that a simple regex cannot.

### Types

```go
type PreloadedData struct {
    Framework string         // "nextjs", "nuxt", "react", "vue", "angular", "unknown"
    Variables map[string]any // all detected window.__VAR__ values
    NextData  map[string]any // shortcut to __NEXT_DATA__.props.pageProps (nil if not Next.js)
}
```

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `ExtractWindowVar` | `(resp *foxhound.Response, varName string) (any, error)` | Extract a single named window variable |
| `ExtractInlineJSON` | `(resp *foxhound.Response, varPattern string) (any, error)` | Extract a `var X = {...}` or `X = {...}` assignment |
| `ExtractPreloadedData` | `(resp *foxhound.Response) (*PreloadedData, error)` | Auto-detect all well-known preloaded variables |
| `DetectFramework` | `(resp *foxhound.Response) string` | Detect the JS framework from page markers |

### Well-known variables

`ExtractPreloadedData` checks all of these automatically:

| Variable | Framework |
|----------|-----------|
| `__NEXT_DATA__` | Next.js |
| `__NUXT__` | Nuxt |
| `__INITIAL_STATE__` | Generic SSR |
| `__APP_STATE__` | Generic SSR |
| `__APOLLO_STATE__` | Apollo GraphQL |
| `__RELAY_STORE__` | Relay (Meta) |
| `__PRELOADED_STATE__` | Redux SSR |
| `__REDUX_STATE__` | Redux |

### Example: Next.js site

```go
func Process(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    // Auto-detect everything at once.
    pd, err := parse.ExtractPreloadedData(resp)
    if err != nil {
        return nil, err
    }

    if pd.Framework == "nextjs" && pd.NextData != nil {
        // pd.NextData is __NEXT_DATA__.props.pageProps — the actual page data.
        products, ok := pd.NextData["products"].([]any)
        if ok {
            var items []*foxhound.Item
            for _, p := range products {
                m := p.(map[string]any)
                item := foxhound.NewItem()
                item.Set("name", m["name"])
                item.Set("price", m["price"])
                items = append(items, item)
            }
            return &foxhound.Result{Items: items}, nil
        }
    }

    return &foxhound.Result{}, nil
}
```

### Example: Nuxt or generic window variable

```go
// Extract a specific variable by name.
data, err := parse.ExtractWindowVar(resp, "__NUXT__")
if err != nil {
    return nil, err
}
if data != nil {
    m := data.(map[string]any)
    // Navigate the Nuxt state tree.
}

// Extract an inline JS variable (not a window property).
config, err := parse.ExtractInlineJSON(resp, "appConfig")
if err != nil {
    return nil, err
}
```

## Directory & Listing Extraction

The directory parser extracts business listings (restaurants, stores, hotels, services) from pages that use structured data or repeating DOM patterns. It tries three strategies in order of reliability and stops at the first one that produces results.

### Three-strategy approach

| Priority | Strategy | Signal |
|----------|----------|--------|
| 1 | **JSON-LD** | `<script type="application/ld+json">` with schema.org business types |
| 2 | **Microdata** | `itemscope` + `itemtype` attributes with schema.org types |
| 3 | **DOM patterns** | Repeating containers (`.listing`, `.card`, `.result`, etc.) scored by phone/email/address density |

The DOM pattern strategy scores candidate container selectors by counting how many child elements contain phone numbers, email addresses, or US-style street addresses. Candidates with a score below 3 are discarded. The highest-scoring selector is used.

### Recognized business types

The parser recognizes these schema.org `@type` values: `LocalBusiness`, `Organization`, `Restaurant`, `Store`, `Place`, `Hotel`, `MedicalBusiness`, `FinancialService`, `FoodEstablishment`, `HealthAndBeautyBusiness`, `HomeAndConstructionBusiness`, `LegalService`, `RealEstateAgent`, `TouristAttraction`.

### Types

```go
type Listing struct {
    Name        string
    Address     string
    Phone       string
    Email       string
    Website     string
    Categories  []string
    Rating      float64
    ReviewCount int
    Hours       map[string]string // day -> "opens - closes"
    Latitude    float64
    Longitude   float64
    Image       string
    RawFields   map[string]string // all extracted key-value pairs
}

type ListingSchema struct {
    Root   string            // CSS selector for each listing container
    Fields map[string]string // field name -> CSS selector (relative to root)
    Attrs  map[string]string // field name -> attribute to extract (default: text)
}
```

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `ExtractListings` | `(resp *foxhound.Response) ([]Listing, error)` | Auto-extract listings via JSON-LD, microdata, or DOM patterns |
| `ExtractListingsWithSchema` | `(resp *foxhound.Response, schema ListingSchema) ([]Listing, error)` | Extract listings using a custom CSS selector mapping |
| `NormalizeAddress` | `(raw string) (street, city, state, zip, country string)` | Parse a raw address into components |
| `NormalizeRating` | `(text string) (rating float64, reviewCount int)` | Parse rating text like "4.5 (123 reviews)" or Unicode stars |
| `Listing.AsItem` | `() *foxhound.Item` | Convert a Listing to a foxhound Item |

### Address normalization

`NormalizeAddress` first tries a US-pattern regex (`123 Main St, City, ST 12345`). On failure, it falls back to comma-split heuristics with position-based guessing for street, city, state, zip, and country components.

### Rating normalization

`NormalizeRating` handles multiple formats:
- Decimal ratings: `"4.5"`, `"4.5/5"`, `"4.5 stars"`
- Unicode stars: `"★★★★☆"` (counts filled star characters)
- Combined: `"4.5 (123 reviews)"` (extracts both rating and review count)
- Ratings above 5 are clamped to 5.

### Example

```go
func Process(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    // Automatic extraction (tries JSON-LD, microdata, DOM in order).
    listings, err := parse.ExtractListings(resp)
    if err != nil {
        return nil, err
    }

    var items []*foxhound.Item
    for _, l := range listings {
        items = append(items, l.AsItem())

        // Normalize the address into components.
        street, city, state, zip, _ := parse.NormalizeAddress(l.Address)
        fmt.Printf("%s: %s, %s, %s %s\n", l.Name, street, city, state, zip)
    }

    return &foxhound.Result{Items: items}, nil
}

// For non-standard layouts, define a custom schema.
func ProcessCustom(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    schema := parse.ListingSchema{
        Root: ".business-card",
        Fields: map[string]string{
            "name":    "h3.biz-name",
            "phone":   ".phone-number",
            "address": ".street-address",
            "rating":  ".star-rating",
            "image":   "img.biz-photo",
        },
        Attrs: map[string]string{
            "image": "src", // extract src attribute instead of text
        },
    }

    listings, err := parse.ExtractListingsWithSchema(resp, schema)
    if err != nil {
        return nil, err
    }

    var items []*foxhound.Item
    for _, l := range listings {
        items = append(items, l.AsItem())
    }
    return &foxhound.Result{Items: items}, nil
}
```

## Pagination Detection & Accumulation

The paginator detects "next page" links using a multi-signal scoring algorithm, then assembles content from multiple page responses into a single `PaginatedContent` struct with full text, markdown, and per-page breakdowns.

### Scoring algorithm

`DetectPagination` evaluates every `<a href>` on the page and assigns a score based on multiple signals. Links scoring below 50 are discarded.

| Signal | Score | Description |
|--------|-------|-------------|
| `rel="next"` / `rel="prev"` | +75 | Strongest signal (HTML standard) |
| Text matches next/prev pattern | +50 | "Next", "Continue", arrow characters, etc. |
| URL contains `?page=N` or `/page/N` | +25 | Common pagination URL pattern |
| Parent class/id matches pagination context | +25 | `.pagination`, `.pager`, `.page-nav`, etc. |
| Parent in comment/social/share context | -25 | False positive suppression |
| Text matches first/last pattern | -65 | "First"/"Last" links are not sequential |
| Text length > 10 chars | -(len-10) | Long text is unlikely to be pagination |
| Numeric text (page number) | +10-N | Lower page numbers score higher |

### Types

```go
type PaginatedContent struct {
    Title        string
    Author       string
    URL          string        // first page URL
    Pages        []PageContent
    FullText     string        // all pages joined
    FullMarkdown string        // all pages joined as markdown
    TotalPages   int
}

type PageContent struct {
    URL      string
    HTML     string
    Text     string
    Markdown string
    PageNum  int
}

type PaginationLink struct {
    URL       string
    Text      string
    Score     int
    Direction string // "next" or "prev"
}
```

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `DetectPagination` | `(resp *foxhound.Response) []PaginationLink` | Find and score all pagination links (sorted by score descending) |
| `AssemblePages` | `(pages []*foxhound.Response, contentSelector string) *PaginatedContent` | Combine multiple page responses into one document |
| `ExtractArticleFromPageBreaks` | `(resp *foxhound.Response, contentSelector string) *PaginatedContent` | Split a single response on `<!-- foxhound:page-break -->` markers |

### Combining pagination with the Trail API

The Paginate browser step (`JobStepPaginate`) uses `DetectPagination` internally. For manual control, combine detection with fetching:

```go
func Process(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    // Detect pagination links on the current page.
    links := parse.DetectPagination(resp)

    // Enqueue "next" pages as new jobs.
    var jobs []*foxhound.Job
    for _, link := range links {
        if link.Direction == "next" {
            jobs = append(jobs, &foxhound.Job{
                URL:       link.URL,
                FetchMode: foxhound.FetchStatic,
            })
        }
    }

    return &foxhound.Result{Jobs: jobs}, nil
}
```

### Assembling accumulated pages

When using the `Paginate` step or manual multi-page fetching, assemble all page responses into a single document:

```go
// After collecting responses from all pages:
assembled := parse.AssemblePages(pageResponses, "article.content")

fmt.Println(assembled.Title)
fmt.Println(assembled.Author)
fmt.Printf("%d pages, %d chars\n", assembled.TotalPages, len(assembled.FullText))

// Access individual pages.
for _, page := range assembled.Pages {
    fmt.Printf("Page %d: %s\n", page.PageNum, page.URL)
}
```

### Page-break markers

For single responses that contain accumulated HTML from a `Paginate` step (separated by `<!-- foxhound:page-break -->` markers), use `ExtractArticleFromPageBreaks`:

```go
assembled := parse.ExtractArticleFromPageBreaks(resp, "div.article-body")
fmt.Println(assembled.FullMarkdown)
```

## Auto-Detection Engine

The auto-detection engine classifies a page into one of five content types using a 7-factor heuristic, then dispatches to the appropriate specialized parser. This is useful when scraping heterogeneous sites where page structure varies across URLs.

### Content types

| Type | Constant | Trigger conditions |
|------|----------|-------------------|
| Article | `ContentArticle` | Long text (500+ chars), low link density (<=0.3), headings present, or `<article>` tags |
| Listing | `ContentListing` | 10+ list items, 5+ card-like elements, 50+ links with 20+ images, or JSON-LD business types |
| Product | `ContentProduct` | JSON-LD `Product`/`Offer` type, or price pattern + add-to-cart button |
| Search | `ContentSearch` | Search form + 3+ card elements + pagination links detected |
| Feed | `ContentFeed` | Infinite scroll / feed containers + 3+ timestamps |
| Unknown | `ContentUnknown` | No strong signals |

### 7-factor heuristic

`DetectContentType` evaluates these structural signals:

1. **Heading count**: `h1`, `h2`, `h3` elements
2. **List item count**: `<li>` elements
3. **Link density**: ratio of anchor text to total text
4. **Image count**: `<img>` elements
5. **Card pattern count**: elements with class names matching card/item/entry/listing/result/product
6. **JSON-LD type**: schema.org `@type` from embedded structured data
7. **Text length**: character count of the main content area

### Readability-style article scoring

`ExtractArticle` implements a scoring algorithm inspired by Mozilla's Readability:

1. **Prune unlikely nodes**: remove elements whose class/id matches sidebar, comment, footer, promo, share, widget, ad, etc. (unless overridden by "article", "body", "content", "main" in the same attribute).
2. **Score candidate paragraphs**: each `<p>`, `<td>`, `<pre>`, `<blockquote>`, `<div>` with 25+ characters gets a base score of 1, plus comma count, plus a length bonus (min of `len/100` or 3). Parent receives the full score; grandparent receives half.
3. **Tag and class weighting**: parent elements get +5 for `<div>`, +3 for `<pre>`/`<td>`/`<blockquote>`, -3 for form/list elements, -5 for headings. Class/id names matching "article", "content", "entry", "post" get +25; "sidebar", "comment", "ad", "widget" get -25.
4. **Link density penalty**: final score is multiplied by `(1 - linkDensity)`, where link density is the ratio of text inside `<a>` tags to total text.
5. **Winner selection**: the node with the highest final score is selected. Its HTML, plain text, and markdown are extracted along with metadata (title, author, date, images, tags, word count, reading time).

### Types

```go
type ContentType int

const (
    ContentUnknown ContentType = iota
    ContentArticle
    ContentListing
    ContentProduct
    ContentSearch
    ContentFeed
)

type AutoResult struct {
    Type     ContentType
    Article  *Article         // populated when Type == ContentArticle
    Listings []Listing        // populated when Type == ContentListing
    Items    []*foxhound.Item // generic extraction for other types
}

type Article struct {
    Title           string
    Author          string
    PublishedDate   string
    Content         string        // cleaned HTML
    ContentText     string        // plain text
    ContentMarkdown string        // markdown
    Summary         string        // first ~200 chars
    Images          []string
    Tags            []string
    WordCount       int
    ReadingTime     time.Duration // based on 200 wpm
    Score           float64       // readability confidence score
}
```

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `DetectContentType` | `(resp *foxhound.Response) ContentType` | Classify the page using structural heuristics |
| `AutoExtract` | `(resp *foxhound.Response) (*AutoResult, error)` | Detect type and extract content accordingly |
| `ExtractArticle` | `(resp *foxhound.Response) (*Article, error)` | Readability-style article extraction with metadata |

### Example

```go
func Process(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
    // Let the engine decide what the page contains.
    result, err := parse.AutoExtract(resp)
    if err != nil {
        return nil, err
    }

    var items []*foxhound.Item

    switch result.Type {
    case parse.ContentArticle:
        a := result.Article
        item := foxhound.NewItem()
        item.Set("title", a.Title)
        item.Set("author", a.Author)
        item.Set("date", a.PublishedDate)
        item.Set("content", a.ContentMarkdown)
        item.Set("word_count", a.WordCount)
        item.Set("reading_time", a.ReadingTime.String())
        items = append(items, item)

    case parse.ContentListing:
        for _, l := range result.Listings {
            items = append(items, l.AsItem())
        }

    default:
        items = result.Items
    }

    return &foxhound.Result{Items: items}, nil
}
```

### Direct article extraction

When you know the page is an article, call `ExtractArticle` directly for the full metadata:

```go
article, err := parse.ExtractArticle(resp)
if err != nil {
    return nil, err
}
if article != nil {
    fmt.Printf("%s by %s (%d words, %s read)\n",
        article.Title, article.Author, article.WordCount, article.ReadingTime)
    fmt.Println(article.ContentMarkdown)
}
```

## Quick Reference

| Function | Purpose | Returns |
|----------|---------|---------|
| `ExtractTable` | Parse one HTML table by selector | `*Table` |
| `ExtractTables` | Parse all HTML tables on page | `[]*Table` |
| `Table.AsItems` | Convert table rows to Items | `[]*foxhound.Item` |
| `Table.Row` | Get row by index | `[]string` |
| `Table.Column` | Get all values for a named column | `[]string` |
| `Table.Cell` | Get single cell by row index and column name | `string` |
| `ExtractWindowVar` | Extract a named `window.__VAR__` | `any` |
| `ExtractInlineJSON` | Extract a `var X = {...}` assignment | `any` |
| `ExtractPreloadedData` | Auto-detect all preloaded JS data | `*PreloadedData` |
| `DetectFramework` | Identify JS framework from page markers | `string` |
| `ExtractListings` | Auto-extract business listings (3 strategies) | `[]Listing` |
| `ExtractListingsWithSchema` | Extract listings with custom CSS mapping | `[]Listing` |
| `NormalizeAddress` | Parse raw address into components | `street, city, state, zip, country` |
| `NormalizeRating` | Parse rating text into numeric values | `rating, reviewCount` |
| `Listing.AsItem` | Convert Listing to Item | `*foxhound.Item` |
| `DetectPagination` | Find and score pagination links | `[]PaginationLink` |
| `AssemblePages` | Combine multiple page responses | `*PaginatedContent` |
| `ExtractArticleFromPageBreaks` | Split on page-break markers | `*PaginatedContent` |
| `DetectContentType` | Classify page type (7-factor heuristic) | `ContentType` |
| `AutoExtract` | Detect type and extract automatically | `*AutoResult` |
| `ExtractArticle` | Readability-style article extraction | `*Article` |
