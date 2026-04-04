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
	"net/url"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/captcha"
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

// ContentBlockDetector extends status-code-only detection with body-based
// CAPTCHA and soft-block detection using the captcha package. This is the
// default detector used by NewSmart.
type ContentBlockDetector struct{}

// IsBlocked returns true when the response indicates a block — either via
// status code (same as DefaultBlockDetector) or via body content (CAPTCHA
// pages, soft blocks that return 200).
func (d *ContentBlockDetector) IsBlocked(resp *foxhound.Response) bool {
	// 1. Status code check (same as DefaultBlockDetector).
	switch resp.StatusCode {
	case 401, 403, 407, 429, 503:
		return true
	}
	// 2. Body-based: delegate to captcha.Detect.
	if len(resp.Body) > 0 {
		det := captcha.Detect(resp)
		if det.Type != captcha.CaptchaNone {
			return true
		}
	}
	return false
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

// WithDomainScorer enables adaptive domain learning. When a scorer is provided,
// FetchAuto uses Bayesian risk scores to decide whether to attempt static
// fetching or skip directly to browser.
func WithDomainScorer(scorer *DomainScorer) SmartOption {
	return func(f *SmartFetcher) {
		f.scorer = scorer
	}
}

// WithCautiousTimeout sets the timeout for static fetches when the domain
// scorer recommends caution (moderate risk). Default is 5 seconds.
func WithCautiousTimeout(d time.Duration) SmartOption {
	return func(f *SmartFetcher) {
		f.cautiousTimeout = d
	}
}

// SmartFetcher routes each job to the appropriate fetcher. It implements
// foxhound.Fetcher and can be used transparently wherever a Fetcher is expected.
type SmartFetcher struct {
	static          foxhound.Fetcher
	browser         foxhound.Fetcher
	detector        BlockDetector
	scorer          *DomainScorer // nil when learning is disabled
	cautiousTimeout time.Duration
}

// NewSmart creates a SmartFetcher. static must not be nil; browser may be nil
// (in which case FetchAuto will never escalate beyond the static result).
//
// Additional SmartOption values may be used to override the detector or either
// fetcher after construction.
func NewSmart(static, browser foxhound.Fetcher, opts ...SmartOption) *SmartFetcher {
	f := &SmartFetcher{
		static:          static,
		browser:         browser,
		detector:        &ContentBlockDetector{},
		cautiousTimeout: 5 * time.Second,
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
// When a DomainScorer is attached, it consults the learned risk score before
// deciding whether to attempt a static fetch or skip directly to browser.
func (f *SmartFetcher) fetchAuto(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
	domain := extractDomainSmart(job)
	parentCtx := ctx // preserve original context for browser escalation

	// Check learned risk score
	if f.scorer != nil {
		action := f.scorer.Recommend(domain)
		switch action {
		case ActionBrowserDirect:
			if f.browser != nil {
				slog.Info("fetch/smart: learned risk is high, going directly to browser",
					"domain", domain, "risk", fmt.Sprintf("%.2f", f.scorer.Risk(domain)),
					"url", job.URL)
				resp, err := f.browser.Fetch(ctx, job)
				if resp != nil {
					blocked := f.detector.IsBlocked(resp)
					f.scorer.RecordBrowser(domain, blocked)
				}
				return resp, err
			}
		case ActionStaticCautious:
			slog.Debug("fetch/smart: moderate risk, trying static with caution",
				"domain", domain, "risk", fmt.Sprintf("%.2f", f.scorer.Risk(domain)))
			// Fail-fast: use a shorter timeout for cautious static attempts
			// so we escalate to browser quickly if static is blocked.
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, f.cautiousTimeout)
			defer cancel()
		}
	}

	slog.Debug("fetch/smart: auto mode — trying static first", "url", job.URL, "job_id", job.ID)

	resp, err := f.static.Fetch(ctx, job)
	if err != nil {
		// Static fetch error (timeout, DNS, connection) — escalate to browser
		// instead of treating as terminal failure.
		if f.browser != nil {
			slog.Info("fetch/smart: static fetch error, escalating to browser",
				"err", err, "url", job.URL)
			if f.scorer != nil {
				f.scorer.RecordStatic(domain, true) // treat error as block
			}
			return f.browser.Fetch(parentCtx, job)
		}
		return nil, fmt.Errorf("fetch/smart: static fetch failed for %s: %w", job.URL, err)
	}

	blocked := f.detector.IsBlocked(resp)

	// Record outcome for learning
	if f.scorer != nil {
		f.scorer.RecordStatic(domain, blocked)
	}

	if !blocked {
		slog.Debug("fetch/smart: static succeeded, no escalation needed",
			"status", resp.StatusCode, "url", job.URL, "job_id", job.ID)
		return resp, nil
	}

	// Static response was blocked.
	if f.browser == nil {
		slog.Warn("fetch/smart: block detected but no browser fetcher configured, returning static result",
			"status", resp.StatusCode, "url", job.URL, "job_id", job.ID)
		return resp, nil
	}

	slog.Info("fetch/smart: block detected, escalating to browser fetcher",
		"status", resp.StatusCode, "url", job.URL, "job_id", job.ID)

	browserResp, err := f.browser.Fetch(parentCtx, job)
	if err != nil {
		return nil, fmt.Errorf("fetch/smart: browser escalation failed for %s: %w", job.URL, err)
	}

	// Record browser outcome too
	if f.scorer != nil && browserResp != nil {
		browserBlocked := f.detector.IsBlocked(browserResp)
		f.scorer.RecordBrowser(domain, browserBlocked)
	}

	return browserResp, nil
}

// extractDomainSmart returns the domain for the given job, preferring
// the explicit Domain field and falling back to parsing the URL.
func extractDomainSmart(job *foxhound.Job) string {
	if job.Domain != "" {
		return job.Domain
	}
	if u, err := url.Parse(job.URL); err == nil {
		return u.Hostname()
	}
	return "unknown"
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
