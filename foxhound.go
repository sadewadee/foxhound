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
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
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

// Step action constants for JobStep. These are package-level int constants
// (not engine.StepAction) to avoid an import cycle between foxhound ↔ engine.
const (
	JobStepNavigate       = 0
	JobStepClick          = 1
	JobStepWait           = 2
	JobStepExtract        = 3
	JobStepScroll         = 4
	JobStepInfiniteScroll = 5 // scroll to bottom until no new content loads
	JobStepLoadMore       = 6 // click "load more" button repeatedly until gone
	JobStepPaginate       = 7 // detect and follow pagination links
	JobStepEvaluate       = 8 // execute custom JavaScript on the page
	JobStepFill           = 9 // type text into input field with human-like keystrokes
)

// JobStep is a single browser-side action that should be executed after the
// page loads. Steps are attached to a Job by Trail.ToJobs() and executed by
// the CamoufoxFetcher before content extraction.
type JobStep struct {
	// Action identifies the step type (JobStepClick, JobStepWait, etc.).
	// Zero value (JobStepNavigate) is intentionally NOT omitempty so it
	// always serializes.
	Action int `json:"action"`
	// Selector is the CSS selector for Click, Wait, and Extract steps.
	Selector string `json:"selector,omitempty"`
	// Duration is the timeout for Wait steps.
	Duration time.Duration `json:"duration,omitempty"`
	// ScrollAxis is 0 for vertical, 1 for horizontal (only for Scroll steps).
	ScrollAxis int `json:"scroll_axis,omitempty"`
	// ScrollExtent is the target scroll distance in pixels. Defaults to 3000
	// when zero.
	ScrollExtent int `json:"scroll_extent,omitempty"`
	// ScrollMode is 0 for ScrollReading, 1 for ScrollScan. Zero value
	// (omitted in JSON) defaults to ScrollReading.
	ScrollMode int `json:"scroll_mode,omitempty"`
	// MaxScrolls is the maximum number of scroll-to-bottom iterations for
	// InfiniteScroll steps. Defaults to 50 when zero.
	MaxScrolls int `json:"max_scrolls,omitempty"`
	// MaxClicks is the maximum number of "load more" button clicks for
	// LoadMore steps. Defaults to 20 when zero.
	MaxClicks int `json:"max_clicks,omitempty"`
	// MaxPages is the maximum number of pagination pages to follow for
	// Paginate steps. Defaults to 10 when zero.
	MaxPages int `json:"max_pages,omitempty"`
	// Script is the JavaScript code to execute for Evaluate steps.
	Script string `json:"script,omitempty"`
	// WaitState specifies what state to wait for in Wait steps:
	// "attached" (default), "detached", "visible", or "hidden".
	// Maps to playwright's WaitForSelectorState.
	WaitState string `json:"wait_state,omitempty"`
	// Optional marks this step as non-fatal: if it fails, execution continues
	// instead of aborting the fetch. Useful for steps that may not always be
	// present on the page (e.g. a cookie banner dismiss button).
	Optional bool `json:"optional,omitempty"`
	// StopSelector is a CSS selector that signals InfiniteScroll to stop
	// when the target element count is reached. Used with StopCount to scroll
	// until N items exist (e.g. "div.result" + StopCount=20).
	StopSelector string `json:"stop_selector,omitempty"`
	// StopCount is the target element count for StopSelector. InfiniteScroll
	// stops when document.querySelectorAll(StopSelector).length >= StopCount.
	// Only used when StopSelector is set. Defaults to 1 when zero.
	StopCount int `json:"stop_count,omitempty"`
	// ScrollWait is the duration to wait after each scroll iteration before
	// checking for new content. Defaults to 2s when zero. Increase for slow
	// sites like Google Maps (3-5s recommended).
	ScrollWait time.Duration `json:"scroll_wait,omitempty"`
	// Value is the text to type into an input field for Fill steps.
	Value string `json:"value,omitempty"`
}

// ---------------------------------------------------------------------------
// Response selection helpers — CSS(), XPath(), Follow()
// ---------------------------------------------------------------------------

// Selector provides CSS-selector-based querying on the Response body.
// It wraps a lazily-parsed HTML document so multiple CSS/XPath calls share
// the same parse result.
type Selector struct {
	resp *Response
	body []byte
}

// CSS returns a Selector bound to this Response. Subsequent calls share the
// same parsed document, making it efficient to chain multiple selectors:
//
//	title := resp.CSS("h1").Text()
//	links := resp.CSS("a[href]").Attrs("href")
func (r *Response) CSS(selector string) *Selection {
	sel := &Selector{resp: r, body: r.Body}
	return sel.CSS(selector)
}

// XPath evaluates a simplified XPath expression against the response body
// and returns the first matching element's text.
// See parse.XPath for supported syntax.
func (r *Response) XPath(expr string) string {
	sel := &Selector{resp: r, body: r.Body}
	return sel.XPath(expr)
}

// XPathAll evaluates a simplified XPath expression and returns text content
// of all matching elements.
func (r *Response) XPathAll(expr string) []string {
	sel := &Selector{resp: r, body: r.Body}
	return sel.XPathAll(expr)
}

// TextBody returns the response body as a string.
func (r *Response) TextBody() string {
	return string(r.Body)
}

// CSS returns a Selection for the given CSS selector.
func (s *Selector) CSS(selector string) *Selection {
	return &Selection{
		selector: selector,
		body:     s.body,
	}
}

// XPath evaluates a simplified XPath expression and returns the first match text.
func (s *Selector) XPath(expr string) string {
	results := s.XPathAll(expr)
	if len(results) == 0 {
		return ""
	}
	return results[0]
}

// XPathAll evaluates a simplified XPath expression and returns all match texts.
func (s *Selector) XPathAll(expr string) []string {
	css := xpathToCSSSimple(expr)
	sel := &Selection{selector: css, body: s.body}
	return sel.Texts()
}

// Selection represents a CSS selector applied to an HTML body. It provides
// convenience methods for extracting text, attributes, and HTML from matched
// elements without requiring the user to import the parse package directly.
type Selection struct {
	selector string
	body     []byte
}

// Text returns the trimmed text content of the first element matching the selector.
func (s *Selection) Text() string {
	texts := s.Texts()
	if len(texts) == 0 {
		return ""
	}
	return texts[0]
}

// Texts returns the trimmed text content of all elements matching the selector.
func (s *Selection) Texts() []string {
	return htmlSelectTexts(s.body, s.selector)
}

// Attr returns an attribute value from the first matching element.
func (s *Selection) Attr(attr string) string {
	attrs := s.Attrs(attr)
	if len(attrs) == 0 {
		return ""
	}
	return attrs[0]
}

// Attrs returns attribute values from all matching elements.
func (s *Selection) Attrs(attr string) []string {
	return htmlSelectAttrs(s.body, s.selector, attr)
}

// Len returns the number of elements matching the selector.
func (s *Selection) Len() int {
	return htmlSelectCount(s.body, s.selector)
}

// ---------------------------------------------------------------------------
// Response.Follow() — generate follow-up jobs from links
// ---------------------------------------------------------------------------

// FollowOption configures how Follow generates jobs from discovered links.
type FollowOption func(*followConfig)

type followConfig struct {
	fetchMode  FetchMode
	priority   Priority
	meta       map[string]any
	callback   string // callback name stored in Meta["callback"]
	dontFilter bool   // skip deduplication for this job
	referer    bool   // set current URL as referer in meta
}

// WithFollowMode sets the FetchMode for generated follow-up jobs.
func WithFollowMode(mode FetchMode) FollowOption {
	return func(c *followConfig) { c.fetchMode = mode }
}

// WithFollowPriority sets the Priority for generated follow-up jobs.
func WithFollowPriority(p Priority) FollowOption {
	return func(c *followConfig) { c.priority = p }
}

// WithFollowMeta sets metadata on generated follow-up jobs.
func WithFollowMeta(meta map[string]any) FollowOption {
	return func(c *followConfig) { c.meta = meta }
}

// WithFollowCallback sets a callback name in Meta["callback"] for generated
// jobs, allowing spider-style routing of responses to different handlers.
func WithFollowCallback(callback string) FollowOption {
	return func(c *followConfig) { c.callback = callback }
}

// WithFollowDontFilter marks generated jobs to skip deduplication filtering.
// Useful for pages that need to be re-fetched (e.g. pagination, monitoring).
func WithFollowDontFilter(dontFilter bool) FollowOption {
	return func(c *followConfig) { c.dontFilter = dontFilter }
}

// WithFollowReferer sets the current response URL as referer in the generated
// job's Meta["referer"]. This maintains referer chain for natural browsing
// simulation.
func WithFollowReferer(referer bool) FollowOption {
	return func(c *followConfig) { c.referer = referer }
}

// Follow generates follow-up Jobs from all links matching the CSS selector
// in the response body. Links are resolved relative to the response URL,
// deduplicated, and filtered to HTTP(S) schemes. The generated jobs inherit
// Depth+1 from the originating job.
//
// Example:
//
//	jobs := resp.Follow("a.product-link[href]", foxhound.WithFollowCallback("parseProduct"))
func (r *Response) Follow(selector string, opts ...FollowOption) []*Job {
	cfg := &followConfig{
		fetchMode: FetchAuto,
		priority:  PriorityNormal,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	hrefs := htmlSelectAttrs(r.Body, selector, "href")
	if len(hrefs) == 0 {
		return nil
	}

	base, err := url.Parse(r.URL)
	if err != nil {
		return nil
	}

	parentDepth := 0
	if r.Job != nil {
		parentDepth = r.Job.Depth
	}

	seen := make(map[string]struct{})
	var jobs []*Job

	for _, href := range hrefs {
		href = strings.TrimSpace(href)
		if href == "" || strings.HasPrefix(href, "#") {
			continue
		}

		ref, err := url.Parse(href)
		if err != nil {
			continue
		}

		resolved := base.ResolveReference(ref)
		resolved.Fragment = ""
		link := resolved.String()

		scheme := strings.ToLower(resolved.Scheme)
		if scheme != "http" && scheme != "https" {
			continue
		}

		// Skip metadata/specification URLs that are never real pages.
		host := strings.ToLower(resolved.Hostname())
		if isMetadataHost(host) {
			continue
		}

		if _, dup := seen[link]; dup {
			continue
		}
		seen[link] = struct{}{}

		meta := make(map[string]any)
		for k, v := range cfg.meta {
			meta[k] = v
		}
		if cfg.callback != "" {
			meta["callback"] = cfg.callback
		}

		job := &Job{
			ID:         link,
			URL:        link,
			Method:     "GET",
			FetchMode:  cfg.fetchMode,
			Priority:   cfg.priority,
			Depth:      parentDepth + 1,
			Domain:     resolved.Host,
			Meta:       meta,
			DontFilter: cfg.dontFilter,
			CreatedAt:  time.Now(),
		}
		if cfg.referer {
			job.Meta["referer"] = r.URL
		}
		jobs = append(jobs, job)
	}

	return jobs
}

// FollowAll generates follow-up Jobs for all anchor links (a[href]) in the
// response body. It is shorthand for Follow("a[href]", opts...).
func (r *Response) FollowAll(opts ...FollowOption) []*Job {
	return r.Follow("a[href]", opts...)
}

// FollowURL creates a single follow-up Job for the given URL. The URL is
// resolved relative to the response URL. The generated job inherits Depth+1
// from the originating job.
//
// Unlike Follow, which extracts links from HTML via CSS selectors, FollowURL
// is for programmatically following a known URL (e.g. an API endpoint or a
// URL extracted from JSON data).
//
// Example:
//
//	nextPage := resp.FollowURL("/api/products?page=2", foxhound.WithFollowReferer(true))
func (r *Response) FollowURL(rawURL string, opts ...FollowOption) *Job {
	cfg := &followConfig{
		fetchMode: FetchAuto,
		priority:  PriorityNormal,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	base, err := url.Parse(r.URL)
	if err != nil {
		return nil
	}

	ref, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}

	resolved := base.ResolveReference(ref)
	resolved.Fragment = ""
	link := resolved.String()

	// Skip metadata/specification URLs that are never real pages.
	host := strings.ToLower(resolved.Hostname())
	if isMetadataHost(host) {
		return nil
	}

	parentDepth := 0
	if r.Job != nil {
		parentDepth = r.Job.Depth
	}

	meta := make(map[string]any)
	for k, v := range cfg.meta {
		meta[k] = v
	}
	if cfg.callback != "" {
		meta["callback"] = cfg.callback
	}
	if cfg.referer {
		meta["referer"] = r.URL
	}

	return &Job{
		ID:         link,
		URL:        link,
		Method:     "GET",
		FetchMode:  cfg.fetchMode,
		Priority:   cfg.priority,
		Depth:      parentDepth + 1,
		Domain:     resolved.Host,
		Meta:       meta,
		DontFilter: cfg.dontFilter,
		CreatedAt:  time.Now(),
	}
}

// ---------------------------------------------------------------------------
// Minimal HTML selector helpers (avoid import cycle with parse package)
// ---------------------------------------------------------------------------

// htmlSelectTexts extracts text from elements matching a CSS selector.
// Uses a minimal byte-scan approach to avoid importing goquery here.
// For full-featured parsing, use the parse package directly.
func htmlSelectTexts(body []byte, selector string) []string {
	// Delegate to a package-level callback that parse registers during init.
	if htmlSelectTextsFunc != nil {
		return htmlSelectTextsFunc(body, selector)
	}
	return nil
}

// htmlSelectAttrs extracts attribute values from elements matching a selector.
func htmlSelectAttrs(body []byte, selector, attr string) []string {
	if htmlSelectAttrsFunc != nil {
		return htmlSelectAttrsFunc(body, selector, attr)
	}
	return nil
}

// htmlSelectCount returns the number of elements matching a selector.
func htmlSelectCount(body []byte, selector string) int {
	if htmlSelectCountFunc != nil {
		return htmlSelectCountFunc(body, selector)
	}
	return 0
}

// xpathToCSSSimple converts a limited XPath expression to CSS.
func xpathToCSSSimple(expr string) string {
	if xpathToCSSFunc != nil {
		return xpathToCSSFunc(expr)
	}
	// Fallback: strip leading // and return as-is
	return strings.TrimPrefix(strings.TrimPrefix(expr, "//"), "/")
}

// Package-level function hooks set by the parse package's init().
// This avoids an import cycle: foxhound -> parse -> foxhound.
var (
	htmlSelectTextsFunc func(body []byte, selector string) []string
	htmlSelectAttrsFunc func(body []byte, selector, attr string) []string
	htmlSelectCountFunc func(body []byte, selector string) int
	xpathToCSSFunc      func(expr string) string
)

// RegisterHTMLSelectors is called by the parse package to provide the
// HTML selection implementations used by Response.CSS() and Response.XPath().
func RegisterHTMLSelectors(
	textsFunc func(body []byte, selector string) []string,
	attrsFunc func(body []byte, selector, attr string) []string,
	countFunc func(body []byte, selector string) int,
	xpathFunc func(expr string) string,
) {
	htmlSelectTextsFunc = textsFunc
	htmlSelectAttrsFunc = attrsFunc
	htmlSelectCountFunc = countFunc
	xpathToCSSFunc = xpathFunc
}

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
	// Steps are browser-side actions to execute after page load (optional).
	// When non-empty, the job requires a browser fetcher. The omitempty tag
	// ensures backward compatibility with existing queue serialization.
	Steps []JobStep `json:"steps,omitempty"`
	// DontFilter when true skips deduplication for this specific job.
	// Useful for pages that need to be re-fetched (e.g. pagination, monitoring).
	DontFilter bool `json:"dont_filter,omitempty"`
	// Callback is an optional handler name that the spider routes to a
	// specific Parse method. When empty, the default processor is used.
	Callback string `json:"callback,omitempty"`
}

// IsSuccess returns true when the HTTP status code indicates success (2xx).
func (r *Response) IsSuccess() bool {
	return r.StatusCode >= 200 && r.StatusCode < 300
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
	// StepResults holds return values from JobStepEvaluate steps, keyed by
	// step index (e.g. "step_0", "step_2"). Only populated when steps
	// produce output.
	StepResults map[string]any
	// CapturedXHR holds captured XHR/fetch responses when capture patterns are configured.
	// Each entry is a map with keys: request_url, request_method, status, headers, body.
	CapturedXHR []map[string]any
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

// ToJSON returns item.Fields serialised as compact JSON bytes.
func (it *Item) ToJSON() ([]byte, error) {
	return json.Marshal(it.Fields)
}

// ToJSONPretty returns item.Fields serialised as indented JSON bytes.
func (it *Item) ToJSONPretty() ([]byte, error) {
	return json.MarshalIndent(it.Fields, "", "  ")
}

// ToMap returns a shallow copy of item.Fields.
// Mutations to the returned map do not affect the Item.
func (it *Item) ToMap() map[string]any {
	m := make(map[string]any, len(it.Fields))
	for k, v := range it.Fields {
		m[k] = v
	}
	return m
}

// ToCSVRow returns field values as a string slice following the given column
// order. Missing fields are returned as empty strings.
func (it *Item) ToCSVRow(columns []string) []string {
	row := make([]string, len(columns))
	for i, col := range columns {
		val, ok := it.Fields[col]
		if !ok || val == nil {
			row[i] = ""
		} else {
			row[i] = fmt.Sprintf("%v", val)
		}
	}
	return row
}

// ToMarkdown returns a compact Markdown representation of the item as a
// bullet list: the first key (sorted) is bolded; the rest are appended.
func (it *Item) ToMarkdown() string {
	keys := it.Keys()
	if len(keys) == 0 {
		return ""
	}
	first := fmt.Sprintf("**%v**", it.Fields[keys[0]])
	parts := []string{first}
	for _, k := range keys[1:] {
		parts = append(parts, fmt.Sprintf("%v", it.Fields[k]))
	}
	return "- " + strings.Join(parts, " — ")
}

// ToText returns a plain-text representation with one "key: value" line per
// field, fields in sorted order.
func (it *Item) ToText() string {
	keys := it.Keys()
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s: %v", k, it.Fields[k]))
	}
	return strings.Join(lines, "\n")
}

// String implements fmt.Stringer. It returns a compact JSON representation
// of the item fields, falling back to a key=value format on marshal error.
func (it *Item) String() string {
	data, err := it.ToJSON()
	if err != nil {
		return it.ToText()
	}
	return string(data)
}

// Keys returns the item's field names in sorted (ascending) order.
func (it *Item) Keys() []string {
	keys := make([]string, 0, len(it.Fields))
	for k := range it.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Has reports whether the field exists and has a non-empty string
// representation. A field set to nil or "" is treated as absent.
func (it *Item) Has(key string) bool {
	val, ok := it.Fields[key]
	if !ok || val == nil {
		return false
	}
	return strings.TrimSpace(fmt.Sprintf("%v", val)) != ""
}

// GetString returns the field value as a string. Returns "" if the field is
// absent or its underlying type is not string.
func (it *Item) GetString(key string) string {
	val, ok := it.Fields[key]
	if !ok {
		return ""
	}
	s, ok := val.(string)
	if !ok {
		return ""
	}
	return s
}

// GetFloat returns the field value as float64. Accepts float64 and int/int64
// stored in the Fields map. Returns 0 if the field is absent or non-numeric.
func (it *Item) GetFloat(key string) float64 {
	val, ok := it.Fields[key]
	if !ok || val == nil {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	default:
		return 0
	}
}

// GetInt returns the field value as int. Accepts int, int64, and float64
// stored in the Fields map (float64 is truncated). Returns 0 if the field is
// absent or non-numeric.
func (it *Item) GetInt(key string) int {
	val, ok := it.Fields[key]
	if !ok || val == nil {
		return 0
	}
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
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

// isMetadataHost returns true for hosts that are metadata/specification URIs,
// not real web pages (e.g. schema.org, w3.org XML namespaces).
func isMetadataHost(host string) bool {
	metadataHosts := []string{
		"schema.org",
		"www.schema.org",
		"w3.org",
		"www.w3.org",
		"xmlns.com",
		"purl.org",
		"ogp.me",
		"rdfs.org",
	}
	for _, h := range metadataHosts {
		if host == h {
			return true
		}
	}
	return false
}
