package fetch

// smart.go — SmartFetcher: auto-routing fetcher with block detection.
//
// SmartFetcher selects between static (TLS-impersonating HTTP) and browser
// (Camoufox) fetchers based on the job's FetchMode and the response from
// block detection:
//
//   FetchBrowser → browser directly (no static attempt)
//   FetchStatic  → static directly (no escalation)
//   FetchAuto    → static first; if blocked, escalate to browser
//
// If no browser fetcher is provided, FetchAuto falls back to the static result
// even when a block is detected (with a warning log).

import (
	"context"
	"fmt"
	"log/slog"

	foxhound "github.com/sadewadee/foxhound"
)

// BlockDetector determines whether an HTTP response indicates that the server
// has blocked or rate-limited the scraper.
type BlockDetector interface {
	// IsBlocked returns true when the response should trigger escalation to
	// the browser fetcher.
	IsBlocked(resp *foxhound.Response) bool
}

// DefaultBlockDetector treats common anti-scraping status codes as blocks:
// 401 Unauthorised, 403 Forbidden, 407 Proxy Auth Required,
// 429 Too Many Requests, 503 Service Unavailable.
type DefaultBlockDetector struct{}

// IsBlocked returns true for status codes that commonly indicate blocking or
// rate-limiting by anti-bot systems.
func (d *DefaultBlockDetector) IsBlocked(resp *foxhound.Response) bool {
	switch resp.StatusCode {
	case 401, 403, 407, 429, 503:
		return true
	default:
		return false
	}
}

// SmartOption is a functional option for configuring a SmartFetcher after
// construction. Options override the fetchers and detector set in NewSmart.
type SmartOption func(*SmartFetcher)

// WithBlockDetector replaces the default block detector with a custom one.
func WithBlockDetector(d BlockDetector) SmartOption {
	return func(f *SmartFetcher) {
		f.detector = d
	}
}

// WithStaticFetcher replaces the static (HTTP) fetcher.
func WithStaticFetcher(fetcher foxhound.Fetcher) SmartOption {
	return func(f *SmartFetcher) {
		f.static = fetcher
	}
}

// WithBrowserFetcher replaces the browser (Camoufox) fetcher.
func WithBrowserFetcher(fetcher foxhound.Fetcher) SmartOption {
	return func(f *SmartFetcher) {
		f.browser = fetcher
	}
}

// SmartFetcher routes each job to the appropriate fetcher. It implements
// foxhound.Fetcher and can be used transparently wherever a Fetcher is expected.
type SmartFetcher struct {
	static   foxhound.Fetcher
	browser  foxhound.Fetcher
	detector BlockDetector
}

// NewSmart creates a SmartFetcher. static must not be nil; browser may be nil
// (in which case FetchAuto will never escalate beyond the static result).
//
// Additional SmartOption values may be used to override the detector or either
// fetcher after construction.
func NewSmart(static, browser foxhound.Fetcher, opts ...SmartOption) *SmartFetcher {
	f := &SmartFetcher{
		static:   static,
		browser:  browser,
		detector: &DefaultBlockDetector{},
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Fetch dispatches the job according to its FetchMode:
//
//   - FetchBrowser: calls the browser fetcher directly; returns an error if no
//     browser fetcher is configured.
//   - FetchStatic:  calls the static fetcher; no escalation regardless of the
//     response status.
//   - FetchAuto:    calls the static fetcher first. If the response is flagged
//     as blocked AND a browser fetcher is available, escalates to the browser.
//     If no browser fetcher is available, returns the static result with a
//     warning log.
func (f *SmartFetcher) Fetch(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
	if job == nil {
		return nil, fmt.Errorf("fetch/smart: job must not be nil")
	}

	switch job.FetchMode {
	case foxhound.FetchBrowser:
		return f.fetchBrowser(ctx, job)

	case foxhound.FetchStatic:
		return f.fetchStatic(ctx, job)

	default: // FetchAuto
		return f.fetchAuto(ctx, job)
	}
}

// fetchStatic calls the static fetcher directly.
func (f *SmartFetcher) fetchStatic(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
	slog.Debug("fetch/smart: using static fetcher", "url", job.URL, "job_id", job.ID)
	return f.static.Fetch(ctx, job)
}

// fetchBrowser calls the browser fetcher directly. Returns an error if the
// browser fetcher has not been configured.
func (f *SmartFetcher) fetchBrowser(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
	if f.browser == nil {
		return nil, fmt.Errorf("fetch/smart: browser fetcher requested but not configured for %s", job.URL)
	}
	slog.Debug("fetch/smart: using browser fetcher", "url", job.URL, "job_id", job.ID)
	return f.browser.Fetch(ctx, job)
}

// fetchAuto implements the try-static-then-escalate logic.
func (f *SmartFetcher) fetchAuto(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
	slog.Debug("fetch/smart: auto mode — trying static first", "url", job.URL, "job_id", job.ID)

	resp, err := f.static.Fetch(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("fetch/smart: static fetch failed for %s: %w", job.URL, err)
	}

	if !f.detector.IsBlocked(resp) {
		slog.Debug("fetch/smart: static succeeded, no escalation needed",
			"status", resp.StatusCode,
			"url", job.URL,
			"job_id", job.ID,
		)
		return resp, nil
	}

	// Static response was blocked.
	if f.browser == nil {
		slog.Warn("fetch/smart: block detected but no browser fetcher configured, returning static result",
			"status", resp.StatusCode,
			"url", job.URL,
			"job_id", job.ID,
		)
		return resp, nil
	}

	slog.Info("fetch/smart: block detected, escalating to browser fetcher",
		"status", resp.StatusCode,
		"url", job.URL,
		"job_id", job.ID,
	)

	browserResp, err := f.browser.Fetch(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("fetch/smart: browser escalation failed for %s: %w", job.URL, err)
	}
	return browserResp, nil
}

// Close releases resources held by both the static and browser fetchers.
// Errors from both fetchers are collected; if both fail the errors are joined.
func (f *SmartFetcher) Close() error {
	var staticErr, browserErr error

	if f.static != nil {
		staticErr = f.static.Close()
		if staticErr != nil {
			slog.Error("fetch/smart: error closing static fetcher", "err", staticErr)
		}
	}
	if f.browser != nil {
		browserErr = f.browser.Close()
		if browserErr != nil {
			slog.Error("fetch/smart: error closing browser fetcher", "err", browserErr)
		}
	}

	if staticErr != nil && browserErr != nil {
		return fmt.Errorf("fetch/smart: closing static fetcher: %w; closing browser fetcher: %v",
			staticErr, browserErr)
	}
	if staticErr != nil {
		return fmt.Errorf("fetch/smart: closing static fetcher: %w", staticErr)
	}
	if browserErr != nil {
		return fmt.Errorf("fetch/smart: closing browser fetcher: %w", browserErr)
	}
	return nil
}
