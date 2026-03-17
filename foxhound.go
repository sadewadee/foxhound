// Package foxhound is a Go scraping framework with native Camoufox anti-detection.
//
// Foxhound provides dual-mode fetching: a TLS-impersonating HTTP client for static
// pages and Camoufox (Firefox fork) via playwright-go for JS-heavy/protected pages,
// with automatic escalation when blocks are detected.
//
// Key concepts:
//   - Hunt: a scraping campaign (top-level orchestrator)
//   - Trail: navigation path with ordered steps
//   - Walker: virtual user with its own session, identity, and behavior profile
//   - Job: unit of work (URL + metadata) consumed from queue
package foxhound

import (
	"context"
	"net/http"
	"time"
)

// FetchMode indicates which fetcher to use for a request.
type FetchMode int

const (
	// FetchAuto lets the smart router decide between static and browser.
	FetchAuto FetchMode = iota
	// FetchStatic forces the TLS-impersonating HTTP client.
	FetchStatic
	// FetchBrowser forces the Camoufox browser.
	FetchBrowser
)

// String returns the string representation of a FetchMode.
func (m FetchMode) String() string {
	switch m {
	case FetchAuto:
		return "auto"
	case FetchStatic:
		return "static"
	case FetchBrowser:
		return "browser"
	default:
		return "unknown"
	}
}

// Priority represents job priority in the queue.
type Priority int

const (
	PriorityLow    Priority = 0
	PriorityNormal Priority = 5
	PriorityHigh   Priority = 10
)

// Job represents a unit of work to be processed by the engine.
type Job struct {
	// ID is a unique identifier for this job.
	ID string
	// URL is the target URL to fetch.
	URL string
	// Method is the HTTP method (default GET).
	Method string
	// Headers are additional HTTP headers to include.
	Headers http.Header
	// Body is the request body for POST/PUT requests.
	Body []byte
	// FetchMode determines which fetcher to use.
	FetchMode FetchMode
	// Priority determines processing order.
	Priority Priority
	// MaxRetries overrides the default retry count.
	MaxRetries int
	// Meta is arbitrary metadata passed through the pipeline.
	Meta map[string]any
	// Depth is the crawl depth from the seed URL.
	Depth int
	// Domain is the target domain extracted from URL.
	Domain string
	// CreatedAt is when the job was created.
	CreatedAt time.Time
}

// Response wraps an HTTP response with additional metadata.
type Response struct {
	// StatusCode is the HTTP status code.
	StatusCode int
	// Headers are the response headers.
	Headers http.Header
	// Body is the response body bytes.
	Body []byte
	// URL is the final URL after redirects.
	URL string
	// FetchMode indicates which fetcher was used.
	FetchMode FetchMode
	// Duration is how long the fetch took.
	Duration time.Duration
	// Job is the original job that produced this response.
	Job *Job
}

// Item represents a scraped data item passing through the pipeline.
type Item struct {
	// Fields holds the extracted data as key-value pairs.
	Fields map[string]any
	// Meta carries metadata from the originating job.
	Meta map[string]any
	// URL is the source URL.
	URL string
	// Timestamp is when the item was created.
	Timestamp time.Time
}

// NewItem creates a new Item with initialized fields.
func NewItem() *Item {
	return &Item{
		Fields:    make(map[string]any),
		Meta:      make(map[string]any),
		Timestamp: time.Now(),
	}
}

// Set sets a field on the item.
func (it *Item) Set(key string, value any) {
	it.Fields[key] = value
}

// Get retrieves a field from the item.
func (it *Item) Get(key string) (any, bool) {
	v, ok := it.Fields[key]
	return v, ok
}

// Result is the output of processing a job. It contains scraped items
// and optionally new jobs to enqueue (for crawling).
type Result struct {
	// Items are the extracted data items.
	Items []*Item
	// Jobs are new jobs to enqueue (discovered links, pagination, etc.).
	Jobs []*Job
}

// Fetcher defines the interface for making HTTP requests.
type Fetcher interface {
	// Fetch performs an HTTP request and returns the response.
	Fetch(ctx context.Context, job *Job) (*Response, error)
	// Close releases any resources held by the fetcher.
	Close() error
}

// Processor defines the user-provided logic for handling responses.
// This is the main extension point: users implement this to extract data.
type Processor interface {
	// Process takes a response and returns extracted items and new jobs.
	Process(ctx context.Context, resp *Response) (*Result, error)
}

// ProcessorFunc is an adapter to allow use of ordinary functions as Processors.
type ProcessorFunc func(ctx context.Context, resp *Response) (*Result, error)

// Process calls f(ctx, resp).
func (f ProcessorFunc) Process(ctx context.Context, resp *Response) (*Result, error) {
	return f(ctx, resp)
}

// FetcherFunc is an adapter to allow use of ordinary functions as Fetchers.
type FetcherFunc func(ctx context.Context, job *Job) (*Response, error)

// Fetch calls f(ctx, job).
func (f FetcherFunc) Fetch(ctx context.Context, job *Job) (*Response, error) { return f(ctx, job) }

// Close is a no-op to satisfy the Fetcher interface.
func (f FetcherFunc) Close() error { return nil }

// Middleware wraps a Fetcher to add cross-cutting behavior.
type Middleware interface {
	// Wrap takes a Fetcher and returns a wrapped Fetcher.
	Wrap(next Fetcher) Fetcher
}

// MiddlewareFunc is an adapter for using functions as Middleware.
type MiddlewareFunc func(next Fetcher) Fetcher

// Wrap calls f(next).
func (f MiddlewareFunc) Wrap(next Fetcher) Fetcher {
	return f(next)
}

// Pipeline processes items after extraction.
type Pipeline interface {
	// Process takes an item and returns the (possibly modified) item.
	// Return nil to drop the item. Return an error to log and continue.
	Process(ctx context.Context, item *Item) (*Item, error)
}

// PipelineFunc is an adapter for using functions as Pipeline stages.
type PipelineFunc func(ctx context.Context, item *Item) (*Item, error)

// Process calls f(ctx, item).
func (f PipelineFunc) Process(ctx context.Context, item *Item) (*Item, error) {
	return f(ctx, item)
}

// Queue defines the interface for job storage and retrieval.
type Queue interface {
	// Push adds a job to the queue.
	Push(ctx context.Context, job *Job) error
	// Pop removes and returns the highest priority job. Blocks until available
	// or context is cancelled.
	Pop(ctx context.Context) (*Job, error)
	// Len returns the number of jobs in the queue.
	Len() int
	// Close releases queue resources.
	Close() error
}

// Writer defines the interface for exporting scraped items.
type Writer interface {
	// Write outputs an item to the destination.
	Write(ctx context.Context, item *Item) error
	// Flush ensures all buffered items are written.
	Flush(ctx context.Context) error
	// Close releases writer resources.
	Close() error
}
