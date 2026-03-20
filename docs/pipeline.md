# Pipeline & Export

The pipeline processes each `Item` after it is extracted by your `Processor`. Stages run left-to-right. If any stage returns `nil`, the item is dropped and subsequent stages are skipped. Items that survive the full chain are passed to all configured `Writer` instances.

## Item Convenience Methods

Before an item reaches any writer, your code can use these methods on `*foxhound.Item`:

```go
// Type-safe field access:
title := item.GetString("title")   // "" if absent or not string
price := item.GetFloat("price")    // 0 if absent or non-numeric
count := item.GetInt("count")      // 0 if absent or non-numeric
exists := item.Has("email")        // false if absent, nil, or ""

// Sorted field keys (deterministic output):
keys := item.Keys()                // []string, alphabetically sorted

// Serialisation:
data, err := item.ToJSON()                           // compact JSON bytes
data, err := item.ToJSONPretty()                     // indented JSON bytes
m := item.ToMap()                                    // shallow copy of Fields
row := item.ToCSVRow([]string{"title", "price"})     // []string in column order

// Text representations:
md := item.ToMarkdown()   // "- **firstVal** — val2 — val3"
txt := item.ToText()      // "title: Go\nprice: 12.99"
str := item.String()      // compact JSON (fallback to ToText on marshal error)
```

## Built-in Pipeline Stages

All stages are in the `pipeline` package (`github.com/sadewadee/foxhound/pipeline`).

### Validate

Drops items that are missing required fields.

```go
stage := &pipeline.Validate{
    Required: []string{"title", "url", "price"},
}
```

If any field in `Required` is absent from `item.Fields`, the item is dropped (returns `nil`).

```yaml
pipeline:
  - validate:
      required: [title, url, price]
```

### Clean

Normalises string values.

```go
stage := &pipeline.Clean{
    TrimWhitespace: true,   // trim leading/trailing whitespace from all string fields
    NormalizePrice: false,  // parse price strings like "£12.99" to float64
}
```

```yaml
pipeline:
  - clean:
      trim_whitespace: true
      normalize_price: true
```

### Dedup

Drops items with a duplicate field value within the pipeline run.

```go
stage := &pipeline.Dedup{
    Key: "url",  // deduplicate on this field
}
```

### Chain

Combine multiple stages into a single pipeline:

```go
chain := pipeline.NewChain(
    &pipeline.Validate{Required: []string{"title", "url"}},
    &pipeline.Clean{TrimWhitespace: true},
    myCustomStage,
)

// Use in a Processor:
processed, err := chain.Process(ctx, item)
if processed != nil {
    results = append(results, processed)
}
```

`NewChain` returns a `*pipeline.Chain` which also implements `foxhound.Pipeline`.

## Writers (Export)

Writers are defined in `github.com/sadewadee/foxhound/pipeline/export`.

All writers implement `foxhound.Writer`:

```go
type Writer interface {
    Write(ctx context.Context, item *Item) error
    Flush(ctx context.Context) error
    Close() error
}
```

Writers are passed to `HuntConfig.Writers`. The engine calls `Flush` on all writers after all walkers finish. Call `Close` in a defer.

### CSV

```go
// With explicit column headers (determines column order):
w, err := export.NewCSV("output.csv", "title", "price", "url")

// Without explicit headers (inferred alphabetically from first item):
w, err := export.NewCSV("output.csv")
```

```yaml
pipeline:
  - export:
      - type: csv
        path: output/results.csv
```

### JSON / JSONL

```go
w, err := export.NewJSON("output.json", export.JSONArray)  // single JSON array
w, err := export.NewJSON("output.jsonl", export.JSONLines) // one object per line
```

JSONL is recommended for large datasets — it can be processed line-by-line without loading the entire file.

```yaml
pipeline:
  - export:
      - type: jsonl
        path: output/results.jsonl
      - type: json
        path: output/results.json
```

### Markdown

Three format options:

```go
// GFM pipe table — header row from first item's sorted keys:
w, err := export.NewMarkdown("output.md", export.MarkdownTable)

// Bullet list — first field bolded, rest dash-separated:
// - **Go Programming** — $12.99 — Five
w, err := export.NewMarkdown("output.md", export.MarkdownList)

// Cards — H2 heading (first field) + bullet-key fields:
// ## Go Programming
// - **price**: $12.99
// - **rating**: Five
w, err := export.NewMarkdown("output.md", export.MarkdownCards)
```

### Text

Two format options:

```go
// One line per item: key=value key2=value2
w, err := export.NewText("output.txt", export.TextLines)

// Labelled blocks with separator lines:
// ────────────────────────────
// Price:    $12.99
// Title:    Go Programming
// ────────────────────────────
w, err := export.NewText("output.txt", export.TextPretty)
```

### XML

```go
// rootElement defaults to "items", itemElement defaults to "item"
w, err := export.NewXML("output.xml", "products", "product")
```

Output format:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<products>
  <product>
    <price>$12.99</price>
    <title>Go Programming</title>
  </product>
</products>
```

Fields are emitted in sorted key order for determinism.

### SQLite

```go
// dbPath is created if it does not exist; table defaults to "items"
w, err := export.NewSQLite("results.db", "books")
```

- Table is created automatically from the first item's fields
- New fields on subsequent items trigger `ALTER TABLE ADD COLUMN`
- WAL journal mode enabled for concurrent writes

### Webhook

```go
w := export.NewWebhook("https://api.example.com/items")

// Batched POST (sends array of N items per request):
w := export.NewWebhook("https://api.example.com/items",
    export.WithBatchSize(50),
)
```

```yaml
pipeline:
  - export:
      - type: webhook
        path: https://api.example.com/items
        batch_size: 50
```

### PostgreSQL

```go
w, err := export.NewPostgres("postgres://user:pass@host:5432/db", "items")

// Upsert on a unique key:
w, err := export.NewPostgres("postgres://user:pass@host:5432/db", "items",
    export.WithUpsert("url"),
    export.WithPGBatchSize(100),
)
```

The connection string can also be set via `FOXHOUND_EXPORT_DB`:

```yaml
pipeline:
  - export:
      - type: postgres
        table: scraped_items
        upsert_key: url
        batch_size: 100
```

## Complete Pipeline Example

```go
import (
    "github.com/sadewadee/foxhound/pipeline"
    "github.com/sadewadee/foxhound/pipeline/export"
)

// Writers.
csvWriter, _ := export.NewCSV("books.csv", "title", "price", "rating", "url")
jsonlWriter, _ := export.NewJSON("books.jsonl", export.JSONLines)
mdWriter, _ := export.NewMarkdown("books.md", export.MarkdownTable)
sqliteWriter, _ := export.NewSQLite("books.db", "books")

// Pipeline chain (applied inline in Processor).
chain := pipeline.NewChain(
    &pipeline.Validate{Required: []string{"title", "price", "url"}},
    &pipeline.Clean{TrimWhitespace: true, NormalizePrice: true},
)

h := engine.NewHunt(engine.HuntConfig{
    Writers: []foxhound.Writer{csvWriter, jsonlWriter, mdWriter, sqliteWriter},
    // ...
})
```

## FieldTransformPipeline

Transform, rename, and coerce item fields inline using `FieldTransform` rules:

```go
transforms := []pipeline.FieldTransform{
    {Field: "price", RegexFind: `[^\d.]`, RegexReplace: "", CoerceTo: "float"},
    {Field: "title", RenameTo: "name"},
}
p := pipeline.NewFieldTransformPipeline(transforms)
```

Each transform is applied in order. `RegexFind`/`RegexReplace` runs a regex substitution on the field value, `CoerceTo` converts the result to the specified type (`"float"`, `"int"`, `"string"`), and `RenameTo` renames the field key.

Use it in a chain like any other stage:

```go
chain := pipeline.NewChain(
    &pipeline.Validate{Required: []string{"title", "price"}},
    p,
    &pipeline.Dedup{Key: "url"},
)
```

## Custom Writer

```go
type StdoutWriter struct{}

func (w *StdoutWriter) Write(_ context.Context, item *foxhound.Item) error {
    fmt.Println(item.String()) // compact JSON
    return nil
}

func (w *StdoutWriter) Flush(_ context.Context) error { return nil }
func (w *StdoutWriter) Close() error                  { return nil }
```

## Custom Pipeline Stage

```go
type PriceFilterStage struct {
    MinPrice float64
}

func (p *PriceFilterStage) Process(ctx context.Context, item *foxhound.Item) (*foxhound.Item, error) {
    price := item.GetFloat("price")
    if price < p.MinPrice {
        return nil, nil // drop items below minimum price
    }
    return item, nil
}
```

## Pipeline vs HuntConfig.Pipelines

There are two places to apply pipeline stages:

**Inside the Processor (inline):** Call `chain.Process(ctx, item)` inside your `ProcessorFunc`. Items are processed before being added to the result.

**Via HuntConfig.Pipelines:** Pass stages to `HuntConfig.Pipelines`. The engine applies them to all items returned from every `Processor.Process` call before passing to writers.

Both can be combined. `HuntConfig.Pipelines` is useful for a consistent post-processing stage applied across all processors.
