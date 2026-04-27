//go:build !tls

// Package fetch provides the dual-mode fetching layer for Foxhound.
//
// stealth_default.go is a FALLBACK with NO TLS fingerprint impersonation.
// Production code should build with -tags tls to use stealth_tls.go.
// Without -tags tls, the TLS ClientHello is Go's crypto/tls default —
// trivially detected by every modern anti-bot system. Headers will look like
// Firefox but the TLS layer will not. This is almost always the wrong choice
// for scraping production targets that have any bot defence.
//
// To verify which build you have:
//
//	fetcher := fetch.NewStealth(...)
//	if !fetcher.IsImpersonating() { log.Fatal("built without -tags tls") }
//
// Or check the binary:
//
//	go tool nm /path/to/binary | grep -q azuretls && echo OK || echo MISSING
//
// Build the impersonating variant with:
//
//	go build -tags tls ./...
//	go test  -tags tls ./fetch/...
package fetch

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/andybalholm/brotli"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/identity"
)

// defaultStealthTimeout is used when no explicit timeout is supplied.
const defaultStealthTimeout = 30 * time.Second

// StealthOption is a functional option for configuring a StealthFetcher.
type StealthOption func(*StealthFetcher)

// WithIdentity sets the identity profile used to populate request headers.
// All headers (User-Agent, Accept-Language, Sec-Fetch-* etc.) are derived from
// this profile, ensuring internal consistency across UA, header order, and locale.
func WithIdentity(p *identity.Profile) StealthOption {
	return func(f *StealthFetcher) {
		f.identity = p
	}
}

// WithTimeout overrides the HTTP client timeout.
func WithTimeout(d time.Duration) StealthOption {
	return func(f *StealthFetcher) {
		f.client.Timeout = d
	}
}

// WithProxy sets a proxy URL on the underlying HTTP transport.
// The proxyURL must be a fully-qualified URL, e.g. "http://user:pass@host:port".
func WithProxy(proxyURL string) StealthOption {
	return func(f *StealthFetcher) {
		f.proxy = proxyURL
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			slog.Error("fetch/stealth: invalid proxy URL, ignoring",
				"proxy", proxyURL,
				"err", err,
			)
			return
		}
		transport, ok := f.client.Transport.(*http.Transport)
		if !ok || transport == nil {
			transport = &http.Transport{}
		}
		transport.Proxy = http.ProxyURL(parsed)
		f.client.Transport = transport
	}
}

// WithJA3 is a no-op in the default (non-tls) build — the underlying
// net/http transport cannot customise the TLS ClientHello. Build with
// -tags tls for the real implementation.
func WithJA3(_ string) StealthOption {
	return func(f *StealthFetcher) { f.ja3Requested = true }
}

// WithJA3Pool is a no-op in the default build. Build with -tags tls.
func WithJA3Pool(pool []string) StealthOption {
	return func(f *StealthFetcher) {
		if len(pool) > 0 {
			f.ja3Requested = true
		}
	}
}

// WithHTTP2Fingerprint is a no-op in the default build. Build with -tags tls.
func WithHTTP2Fingerprint(_ string) StealthOption {
	return func(f *StealthFetcher) { f.http2Requested = true }
}

// WithHTTP3Fingerprint is a no-op in the default build. Build with -tags tls.
func WithHTTP3Fingerprint(_ string) StealthOption {
	return func(f *StealthFetcher) { f.http3Requested = true }
}

// StealthFetcher is the non-impersonating fallback used when the binary is
// built without -tags tls. The TLS ClientHello is Go's crypto/tls default
// (well-known JA3) — header ordering from the identity profile still applies
// but the TLS layer will not match a real browser.
type StealthFetcher struct {
	client   *http.Client
	identity *identity.Profile
	proxy    string

	// ja3Requested / http2Requested / http3Requested record whether the caller
	// asked for fingerprint customisation that this build cannot honour. The
	// startup log surfaces this so operators can spot a missing -tags tls.
	ja3Requested   bool
	http2Requested bool
	http3Requested bool
}

// IsImpersonating reports whether this fetcher performs real JA3/JA4 TLS
// fingerprint impersonation. In the default (non-tls) build it always returns
// false; build with -tags tls to enable real impersonation.
func (f *StealthFetcher) IsImpersonating() bool { return false }

// NewStealth creates a StealthFetcher. Call with any number of StealthOption
// functional options to configure identity, timeout, and proxy.
//
// IMPORTANT: this build does NOT perform TLS impersonation. See the package
// doc and IsImpersonating().
func NewStealth(opts ...StealthOption) *StealthFetcher {
	jar, _ := cookiejar.New(nil)
	f := &StealthFetcher{
		client: &http.Client{
			Timeout:   defaultStealthTimeout,
			Transport: &http.Transport{},
			Jar:       jar,
		},
	}
	for _, opt := range opts {
		opt(f)
	}

	level := slog.LevelWarn
	msg := "fetch/stealth: initialized WITHOUT TLS impersonation (built without -tags tls; ClientHello is Go default and trivially fingerprintable)"
	if f.ja3Requested || f.http2Requested || f.http3Requested {
		level = slog.LevelError
		msg = "fetch/stealth: TLS fingerprint customisation requested but ignored — binary built without -tags tls"
	}
	slog.Log(context.Background(), level, msg,
		"tls_impersonation", false,
		"build_tag", "default",
		"ja3_requested", f.ja3Requested,
		"http2_requested", f.http2Requested,
		"http3_requested", f.http3Requested,
	)

	return f
}

// Fetch performs an HTTP request using the stealth client and returns a
// foxhound.Response. The response FetchMode is always FetchStatic.
//
// Header ordering follows identity.HeaderOrder to match browser fingerprints.
// If no identity is set a bare request is sent with minimal headers.
func (f *StealthFetcher) Fetch(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
	if job == nil {
		return nil, fmt.Errorf("fetch/stealth: job must not be nil")
	}
	if job.URL == "" {
		return nil, fmt.Errorf("fetch/stealth: job URL must not be empty")
	}

	method := job.Method
	if method == "" {
		method = http.MethodGet
	}

	var bodyReader io.Reader
	if len(job.Body) > 0 {
		bodyReader = bytes.NewReader(job.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, job.URL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("fetch/stealth: building request for %s: %w", job.URL, err)
	}

	f.applyIdentityHeaders(req)

	// Job-level headers override / extend identity headers.
	for k, values := range job.Headers {
		for _, v := range values {
			req.Header.Set(k, v)
		}
	}

	slog.Debug("fetch/stealth: sending request",
		"method", method,
		"url", job.URL,
		"job_id", job.ID,
	)

	start := time.Now()
	var httpResp *http.Response
	var lastErr error
	const maxRetries = 2
	for attempt := 0; attempt <= maxRetries; attempt++ {
		httpResp, lastErr = f.client.Do(req)
		if lastErr == nil {
			break
		}
		if !isTransientError(lastErr) || attempt == maxRetries {
			break
		}
		delay := time.Duration(500*(attempt+1)) * time.Millisecond
		slog.Debug("fetch/stealth: transient error, retrying",
			"url", job.URL, "attempt", attempt+1, "delay", delay, "err", lastErr)
		time.Sleep(delay)
	}
	duration := time.Since(start)
	if lastErr != nil {
		return nil, fmt.Errorf("fetch/stealth: request to %s failed: %w", job.URL, lastErr)
	}
	defer httpResp.Body.Close()

	// Decompress response body when we manually set Accept-Encoding
	// (Go's Transport disables auto-decompression in that case).
	var reader io.Reader = httpResp.Body
	switch strings.ToLower(httpResp.Header.Get("Content-Encoding")) {
	case "gzip":
		if gr, gzErr := gzip.NewReader(httpResp.Body); gzErr == nil {
			defer gr.Close()
			reader = gr
		}
	case "br":
		reader = brotli.NewReader(httpResp.Body)
	}

	// Limit response body to 10MB to avoid unbounded memory allocation.
	const maxResponseSize = 10 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(reader, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("fetch/stealth: reading response body from %s: %w", job.URL, err)
	}

	finalURL := job.URL
	if httpResp.Request != nil && httpResp.Request.URL != nil {
		finalURL = httpResp.Request.URL.String()
	}

	slog.Debug("fetch/stealth: received response",
		"status", httpResp.StatusCode,
		"url", finalURL,
		"duration_ms", duration.Milliseconds(),
		"body_bytes", len(body),
		"job_id", job.ID,
	)

	return &foxhound.Response{
		StatusCode: httpResp.StatusCode,
		Headers:    httpResp.Header,
		Body:       body,
		URL:        finalURL,
		FetchMode:  foxhound.FetchStatic,
		Duration:   duration,
		Job:        job,
		Cookies:    httpResp.Cookies(),
	}, nil
}

// Close is a no-op for StealthFetcher; the underlying http.Client manages its
// own idle connections via the transport.
func (f *StealthFetcher) Close() error {
	if transport, ok := f.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	return nil
}

// applyIdentityHeaders sets all request headers in the correct browser order
// derived from the identity profile. If no identity is configured, a sensible
// minimal header set is applied so requests are always well-formed.
func (f *StealthFetcher) applyIdentityHeaders(req *http.Request) {
	if f.identity == nil {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:135.0) Gecko/20100101 Firefox/135.0")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.5")
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		return
	}

	p := f.identity

	// Build the full header map keyed by canonical name. This map is populated
	// in priority order so that identity values can be selectively overridden by
	// job-level headers after this function returns.
	headerValues := map[string]string{
		"User-Agent":                p.UA,
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/png,image/svg+xml,*/*;q=0.8",
		"Accept-Language":           buildAcceptLanguage(p.Languages),
		"Accept-Encoding":           "gzip, deflate, br, zstd",
		"Upgrade-Insecure-Requests": "1",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
		"TE":                        "trailers",
		"Priority":                  "u=0, i",
	}

	// Apply headers in the canonical browser order to match the TLS fingerprint.
	// Headers not present in the order list are appended afterwards.
	applied := make(map[string]bool, len(headerValues))
	for _, name := range p.HeaderOrder {
		if val, ok := headerValues[name]; ok {
			req.Header.Set(name, val)
			applied[name] = true
		}
	}
	// Append any remaining headers not covered by the order list.
	for name, val := range headerValues {
		if !applied[name] {
			req.Header.Set(name, val)
		}
	}
}

// buildAcceptLanguage constructs an Accept-Language header value from a list of
// language tags, matching Firefox's actual quality factor pattern.
//
// Firefox uses: primary,secondary;q=0.5 (for 2 langs) or
// primary,second;q=0.8,third;q=0.5,fourth;q=0.3 (for more).
//
// Example: ["en-US", "en"] → "en-US,en;q=0.5"
func buildAcceptLanguage(langs []string) string {
	if len(langs) == 0 {
		return "en-US,en;q=0.5"
	}
	if len(langs) == 1 {
		return langs[0]
	}

	// Firefox quality factors: for 2 languages, second gets q=0.5.
	// For 3+, they decrease: 0.8, 0.5, 0.3, 0.1
	firefoxQ := []float64{0.8, 0.6, 0.4, 0.2}
	if len(langs) == 2 {
		// Special case: Firefox uses q=0.5 for 2-language configs
		firefoxQ = []float64{0.5}
	}

	var b strings.Builder
	b.WriteString(langs[0])
	for i, lang := range langs[1:] {
		q := 0.1
		if i < len(firefoxQ) {
			q = firefoxQ[i]
		}
		fmt.Fprintf(&b, ",%s;q=%.1f", lang, q)
	}
	return b.String()
}
