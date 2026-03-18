//go:build tls

// Package fetch provides the dual-mode fetching layer for Foxhound.
//
// This file is the real TLS-impersonating StealthFetcher, compiled only when
// the build tag "tls" is present:
//
//	go build -tags tls ./...
//	go test -tags tls ./fetch/...
//
// It replaces fetch/stealth_default.go (the standard net/http fallback) and uses
// github.com/Noooste/azuretls-client to perform full JA3/JA4 + HTTP/2 fingerprint
// impersonation. The azuretls Session selects a browser TLS ClientHello spec
// (Firefox or Chrome) that is derived from the identity.Profile supplied via
// WithIdentity. Every attribute — UA, TLS cipher suites, header order, ALPN,
// HTTP/2 SETTINGS frame — is internally consistent because the browser profile
// drives them all.
//
// Design constraints (from foxhound-architecture.md §3.1):
//   - Go net/http has a Go-specific JA3 that anti-bot detects immediately.
//   - azuretls replaces the TLS handshake with a full browser ClientHello spec.
//   - Header ordering is preserved via azuretls.OrderedHeaders (not http.Header).
//   - Proxy is set on the azuretls Session, not on an http.Transport.
//   - Context cancellation is respected via session.SetContext per request.
package fetch

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	azuretls "github.com/Noooste/azuretls-client"
	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/identity"
)

// defaultStealthTimeout is used when no explicit timeout is supplied.
const defaultStealthTimeout = 30 * time.Second

// azureBrowserName maps foxhound identity browser names to azuretls browser
// profile constants. azuretls uses these to select the matching TLS ClientHello
// spec and HTTP/2 settings frame, ensuring JA3/JA4 consistency.
func azureBrowserName(b identity.Browser) string {
	switch b {
	case identity.BrowserFirefox:
		return azuretls.Firefox
	case identity.BrowserChrome:
		return azuretls.Chrome
	default:
		// Default to Firefox: Camoufox is Firefox-based, so defaulting to Firefox
		// ensures consistency when a profile is generated without an explicit browser.
		return azuretls.Firefox
	}
}

// StealthOption is a functional option for configuring a StealthFetcher.
type StealthOption func(*StealthFetcher)

// WithIdentity sets the identity profile used to populate request headers and
// the azuretls TLS browser profile. The browser name, TLS fingerprint, header
// ordering, User-Agent, and Accept-Language are all derived from this profile,
// ensuring they remain internally consistent.
func WithIdentity(p *identity.Profile) StealthOption {
	return func(f *StealthFetcher) {
		f.identity = p
		// Reconfigure the session browser so azuretls selects the matching
		// TLS ClientHello spec immediately when the option is applied.
		if p != nil {
			f.session.Browser = azureBrowserName(p.BrowserName)
		}
	}
}

// WithTimeout overrides the azuretls session timeout.
func WithTimeout(d time.Duration) StealthOption {
	return func(f *StealthFetcher) {
		f.session.SetTimeout(d)
	}
}

// WithProxy sets the proxy on the azuretls session. The proxyURL must be a
// fully-qualified URL with an explicit scheme and host, e.g.
// "http://user:pass@host:port" or "socks5://host:port".
// URLs that fail to parse or that lack a scheme/host are logged and skipped —
// the fetcher remains usable without a proxy.
func WithProxy(proxyURL string) StealthOption {
	return func(f *StealthFetcher) {
		parsed, err := url.Parse(proxyURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			slog.Error("fetch/stealth: invalid proxy URL (must include scheme and host), ignoring",
				"proxy", proxyURL,
			)
			return
		}
		f.proxy = proxyURL
		if err := f.session.SetProxy(proxyURL); err != nil {
			slog.Error("fetch/stealth: failed to set proxy on azuretls session, ignoring",
				"proxy", proxyURL,
				"err", err,
			)
		}
	}
}

// StealthFetcher is a TLS-impersonating HTTP client backed by azuretls-client.
// It performs full JA3/JA4 + HTTP/2 fingerprint impersonation so that the TLS
// ClientHello matches a real browser, not Go's standard crypto/tls handshake.
//
// The browser profile (Firefox or Chrome) is derived from the identity.Profile
// supplied via WithIdentity. The azuretls session handles TLS and HTTP/2; this
// type handles header construction in the browser-specific canonical order and
// maps foxhound.Job into azuretls.Request.
type StealthFetcher struct {
	session  *azuretls.Session
	identity *identity.Profile
	proxy    string
}

// NewStealth creates a StealthFetcher backed by azuretls. Call with any number
// of StealthOption functional options to configure identity, timeout, and proxy.
// The default browser profile is Firefox with a 30-second timeout.
func NewStealth(opts ...StealthOption) *StealthFetcher {
	sess := azuretls.NewSession()
	// Default: Firefox. WithIdentity will override if a Chrome profile is provided.
	sess.Browser = azuretls.Firefox
	sess.SetTimeout(defaultStealthTimeout)

	f := &StealthFetcher{session: sess}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Fetch performs an HTTP request using the azuretls TLS-impersonating session
// and returns a foxhound.Response. The response FetchMode is always FetchStatic.
//
// Header ordering follows identity.HeaderOrder to match the browser fingerprint.
// If no identity is set, a sensible Firefox header set is used.
// The context is respected: a cancelled context causes Fetch to return an error.
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

	// Build ordered headers in the canonical browser order. azuretls preserves
	// OrderedHeaders exactly — unlike http.Header which sorts alphabetically.
	ordered := f.buildOrderedHeaders(job)

	req := &azuretls.Request{
		Method:         method,
		Url:            job.URL,
		OrderedHeaders: ordered,
	}
	// SetContext attaches the caller's context so that cancellation and deadlines
	// are respected within the azuretls transport layer.
	req.SetContext(ctx)

	if len(job.Body) > 0 {
		req.Body = bytes.NewReader(job.Body)
	}

	slog.Debug("fetch/stealth: sending request",
		"method", method,
		"url", job.URL,
		"job_id", job.ID,
		"tls_browser", f.session.Browser,
	)

	start := time.Now()
	azureResp, err := f.session.Do(req)
	duration := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("fetch/stealth: request to %s failed: %w", job.URL, err)
	}

	finalURL := job.URL
	if azureResp.Url != "" {
		finalURL = azureResp.Url
	}

	slog.Debug("fetch/stealth: received response",
		"status", azureResp.StatusCode,
		"url", finalURL,
		"duration_ms", duration.Milliseconds(),
		"body_bytes", len(azureResp.Body),
		"job_id", job.ID,
	)

	// Convert azuretls headers (fhttp.Header) to standard net/http.Header so
	// downstream foxhound code can work with them without import azuretls.
	stdHeaders := make(http.Header, len(azureResp.Header))
	for k, vals := range azureResp.Header {
		stdHeaders[k] = vals
	}

	return &foxhound.Response{
		StatusCode: azureResp.StatusCode,
		Headers:    stdHeaders,
		Body:       azureResp.Body,
		URL:        finalURL,
		FetchMode:  foxhound.FetchStatic,
		Duration:   duration,
		Job:        job,
	}, nil
}

// Close cleans up the underlying azuretls session. This releases connection
// pool resources and any in-flight state held by the session.
func (f *StealthFetcher) Close() error {
	f.session.Close()
	return nil
}

// buildOrderedHeaders constructs an azuretls.OrderedHeaders slice in the
// canonical browser header order from the identity profile. Job-level headers
// are merged last so they can override identity headers when needed.
//
// azuretls.OrderedHeaders preserves insertion order during the HTTP/2 HPACK
// encoding, which is part of the Akamai fingerprint. Using an unordered
// http.Header here would break that part of the fingerprint.
func (f *StealthFetcher) buildOrderedHeaders(job *foxhound.Job) azuretls.OrderedHeaders {
	var headerValues map[string]string
	var headerOrder []string

	if f.identity == nil {
		// Minimal Firefox-like defaults when no identity is configured.
		headerValues = map[string]string{
			"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:148.0) Gecko/20100101 Firefox/148.0",
			"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"Accept-Language": "en-US,en;q=0.5",
			"Accept-Encoding": "gzip, deflate, br",
		}
		headerOrder = []string{"User-Agent", "Accept", "Accept-Language", "Accept-Encoding"}
	} else {
		p := f.identity
		headerValues = map[string]string{
			"User-Agent":                p.UA,
			"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
			"Accept-Language":           buildAcceptLanguage(p.Languages),
			"Accept-Encoding":           "gzip, deflate, br",
			"Connection":                "keep-alive",
			"Upgrade-Insecure-Requests": "1",
			"Sec-Fetch-Dest":            "document",
			"Sec-Fetch-Mode":            "navigate",
			"Sec-Fetch-Site":            "none",
			"Sec-Fetch-User":            "?1",
			"Priority":                  "u=0, i",
			"Cache-Control":             "max-age=0",
		}
		headerOrder = p.HeaderOrder
	}

	// Merge job-level headers into headerValues so they appear in the ordered
	// output. Job headers override identity headers with the same canonical name.
	for k, vals := range job.Headers {
		if len(vals) > 0 {
			headerValues[http.CanonicalHeaderKey(k)] = vals[0]
		}
	}

	// Build OrderedHeaders in browser-canonical order first.
	seen := make(map[string]bool, len(headerValues))
	ordered := make(azuretls.OrderedHeaders, 0, len(headerValues))
	for _, name := range headerOrder {
		canonical := http.CanonicalHeaderKey(name)
		if val, ok := headerValues[canonical]; ok {
			ordered = append(ordered, []string{canonical, val})
			seen[canonical] = true
		}
	}
	// Append any remaining headers (identity or job) not covered by the order list.
	for name, val := range headerValues {
		if !seen[http.CanonicalHeaderKey(name)] {
			ordered = append(ordered, []string{http.CanonicalHeaderKey(name), val})
		}
	}

	return ordered
}

// buildAcceptLanguage constructs an Accept-Language header value from a list of
// language tags, applying decreasing quality weights to all tags after the first.
//
// Example: ["en-US", "en"] → "en-US,en;q=0.7"
func buildAcceptLanguage(langs []string) string {
	if len(langs) == 0 {
		return "en-US,en;q=0.5"
	}
	if len(langs) == 1 {
		return langs[0]
	}

	var b strings.Builder
	b.WriteString(langs[0])
	q := 0.9
	for _, lang := range langs[1:] {
		fmt.Fprintf(&b, ",%s;q=%.1f", lang, q)
		if q > 0.2 {
			q -= 0.1
		}
	}
	return b.String()
}

