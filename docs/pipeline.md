# Pipeline & Export

The pipeline processes each `Item` after it is extracted by your `Processor`. Stages run left-to-right. If any stage returns `nil`, the item is dropped and subsequent stages are skipped. Items that survive the full chain are passed to all configured `Writer` instances.

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

Config:

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

Config:

```yaml
pipeline:
  - clean:
      trim_whitespace: true
      normalize_price: true
```

### Dedup

Drops items with a duplicate field value (e.g. duplicate URLs within the pipeline run).

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

Writers are passed to `HuntConfig.Writers`. The engine calls `Flush` on all writers after all walkers finish, then the caller is responsible for calling `Close`.

### CSV

Writes a CSV file with one item per row.

```go
// With explicit column headers (determines column order):
w, err := export.NewCSV("output.csv", "title", "price", "url")

// Without explicit headers (inferred alphabetically from first item):
w, err := export.NewCSV("output.csv")
```

If headers are not provided, they are inferred from the first item's field keys, sorted alphabetically. Missing field values are written as empty strings.

Config:

```yaml
pipeline:
  - export:
      - type: csv
        path: output/results.csv
```

### JSON / JSONL

Writes items as JSON.

```go
// JSON array (single file):
w, err := export.NewJSON("output.json", export.JSONArray)

// JSON Lines — one object per line, streaming-friendly:
w, err := export.NewJSON("output.jsonl", export.JSONLines)
```

JSONL (JSON Lines) is recommended for large datasets because it can be streamed and processed line-by-line without loading the entire file.

Config:

```yaml
pipeline:
  - export:
      - type: jsonl
        path: output/results.jsonl
      - type: json
        path: output/results.json
```

### Webhook

POSTs items (or batches of items) to an HTTP endpoint.

```go
// Single-item POST:
w := export.NewWebhook("https://api.example.com/items")

// Batched POST (sends array of N items per request):
w := export.NewWebhook("https://api.example.com/items",
    export.WithBatchSize(50),
)
```

Config:

```yaml
pipeline:
  - export:
      - type: webhook
        path: https://api.example.com/items
        batch_size: 50
```

### PostgreSQL

Inserts or upserts items into a PostgreSQL table.

```go
// Simple insert:
w, err := export.NewPostgres("postgres://user:pass@host:5432/db", "items")

// Upsert on a unique key:
w, err := export.NewPostgres("postgres://user:pass@host:5432/db", "items",
    export.WithUpsert("url"),
    export.WithPGBatchSize(100),
)
```

The connection string can also be set via the `FOXHOUND_EXPORT_DB` environment variable, which takes precedence over the `path` config field.

Config:

```yaml
pipeline:
  - export:
      - type: postgres
        table: scraped_items
        upsert_key: url
        batch_size: 100
```

Set `FOXHOUND_EXPORT_DB=postgres://user:pass@host:5432/db?sslmode=disable` in your environment.

## Complete Pipeline Example

```go
import (
    "github.com/sadewadee/foxhound/pipeline"
    "github.com/sadewadee/foxhound/pipeline/export"
)

// Writers.
csvWriter, _ := export.NewCSV("books.csv", "title", "price", "rating", "url")
jsonlWriter, _ := export.NewJSON("books.jsonl", export.JSONLines)

// Pipeline chain (applied inline in Processor).
chain := pipeline.NewChain(
    &pipeline.Validate{Required: []string{"title", "price", "url"}},
    &pipeline.Clean{TrimWhitespace: true, NormalizePrice: true},
)

// In HuntConfig:
h := engine.NewHunt(engine.HuntConfig{
    Writers: []foxhound.Writer{csvWriter, jsonlWriter},
    // Pipeline stages applied in Processor:
    // processed, _ := chain.Process(ctx, item)
    // ...
})
```

## Custom Writer

Implement `foxhound.Writer` for any custom destination:

```go
type StdoutWriter struct{}

func (w *StdoutWriter) Write(_ context.Context, item *foxhound.Item) error {
    data, err := json.Marshal(item.Fields)
    if err != nil {
        return err
    }
    fmt.Println(string(data))
    return nil
}

func (w *StdoutWriter) Flush(_ context.Context) error { return nil }
func (w *StdoutWriter) Close() error                  { return nil }
```

## Custom Pipeline Stage

Implement `foxhound.Pipeline` or use `foxhound.PipelineFunc`:

```go
// Inline using PipelineFunc:
dedupeByURL := foxhound.PipelineFunc(func(ctx context.Context, item *foxhound.Item) (*foxhound.Item, error) {
    url, _ := item.Get("url")
    if url == "" {
        return nil, nil  // drop items without URL
    }
    return item, nil
})

// Named type for reusable stages:
type EnrichStage struct {
    DB *sql.DB
}

func (s *EnrichStage) Process(ctx context.Context, item *foxhound.Item) (*foxhound.Item, error) {
    url, _ := item.Get("url")
    // Look up metadata from a database.
    var category string
    _ = s.DB.QueryRowContext(ctx, "SELECT category FROM urls WHERE url = $1", url).Scan(&category)
    item.Set("category", category)
    return item, nil
}
```

## Pipeline vs HuntConfig.Pipelines

There are two places to apply pipeline stages:

**Inside the Processor (inline):** Call `chain.Process(ctx, item)` inside your `ProcessorFunc`. Items are processed before being added to the result. This is the pattern used in the ecommerce example.

**Via HuntConfig.Pipelines:** Pass stages to `HuntConfig.Pipelines`. The engine applies them to all items returned from every `Processor.Process` call. Items that survive are passed to `HuntConfig.Writers`.

Both approaches can be combined. The `HuntConfig.Pipelines` path is useful when you want a consistent post-processing stage applied across all processors.
