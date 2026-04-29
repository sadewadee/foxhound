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
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	azuretls "github.com/Noooste/azuretls-client"
	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/identity"
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
// the azuretls TLS browser family. The browser name, header ordering,
// User-Agent, and Accept-Language are derived from this profile.
//
// The TLS ClientHello itself is left to azuretls's built-in browser preset
// (GetLastFirefoxVersion / GetLastChromeVersion / etc.), which tracks current
// browser releases more reliably than a hand-captured JA3 string. Empirically
// (see issue #41), some captured JA3 strings drift far enough from current
// browsers that targets like Bing reject them with `tls: illegal parameter`,
// while azuretls's built-in spec passes — so the safe default is to NOT
// auto-apply a captured JA3.
//
// Power users with a freshly-captured JA3 from https://tls.peet.ws can still
// override via WithJA3 / WithJA3Pool. fetch/presets ships a single curated
// Firefox JA3 for that opt-in path.
func WithIdentity(p *identity.Profile) StealthOption {
	return func(f *StealthFetcher) {
		f.identity = p
		if p == nil {
			return
		}
		// Reconfigure the session browser so azuretls selects the matching
		// built-in TLS ClientHello spec at request time.
		f.session.Browser = azureBrowserName(p.BrowserName)
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

// WithJA3 pins a specific JA3 ClientHello fingerprint on the azuretls session.
// The browser argument supplied to azuretls.Session.ApplyJa3 is whichever
// browser family is active at apply time (Firefox by default, or whatever
// WithIdentity selected). Apply order is deferred to the end of NewStealth so
// WithIdentity always has a chance to set the browser family first regardless
// of the order options are passed.
//
// Capture a real fingerprint from https://tls.peet.ws to use here. An invalid
// JA3 string is logged and ignored — the session falls back to its default
// preset for the chosen browser.
func WithJA3(ja3 string) StealthOption {
	return func(f *StealthFetcher) {
		f.pendingJA3 = ja3
	}
}

// WithJA3Pool selects a JA3 fingerprint at random from the pool. Pair this
// with periodic fetcher recycling (e.g. every N requests) to rotate JA3 across
// session lifetimes. Empty pools are ignored.
func WithJA3Pool(pool []string) StealthOption {
	return func(f *StealthFetcher) {
		if len(pool) == 0 {
			return
		}
		f.pendingJA3 = pool[rand.IntN(len(pool))]
	}
}

// WithHTTP2Fingerprint sets the Akamai-style HTTP/2 fingerprint on the session
// via azuretls.Session.ApplyHTTP2. Format:
//
//	<SETTINGS>|<WINDOW_UPDATE>|<PRIORITY>|<PSEUDO_HEADER>
//
// e.g. Chrome: "1:65536;2:0;3:1000;4:6291456;6:262144|15663105|0|m,s,a,p".
// An invalid string is logged and ignored.
func WithHTTP2Fingerprint(fp string) StealthOption {
	return func(f *StealthFetcher) {
		f.pendingHTTP2 = fp
	}
}

// WithHTTP3Fingerprint sets the QUIC/HTTP3 fingerprint on the session via
// azuretls.Session.ApplyHTTP3. HTTP/3 is opt-in per request via the
// ForceHTTP3 flag on azuretls.Request — Foxhound does not currently expose
// that flag through the public Fetch API, so this option is reserved for
// advanced consumers that wrap StealthFetcher.Session() directly. Invalid
// fingerprints are logged and ignored.
func WithHTTP3Fingerprint(fp string) StealthOption {
	return func(f *StealthFetcher) {
		f.pendingHTTP3 = fp
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

	// pendingJA3, pendingHTTP2, pendingHTTP3 are captured by the With* options
	// and applied to the session in NewStealth after every option has run, so
	// browser selection from WithIdentity is final before ApplyJa3 sees it.
	pendingJA3   string
	pendingHTTP2 string
	pendingHTTP3 string
}

// IsImpersonating reports whether this fetcher performs real JA3/JA4 TLS
// fingerprint impersonation. In the tls build it always returns true; in the
// default build (no -tags tls) it returns false so consumers can fail-fast at
// startup if impersonation is required for production correctness.
func (f *StealthFetcher) IsImpersonating() bool { return true }

// Session returns the underlying azuretls session for advanced configuration
// the public option API does not yet cover (e.g. GetClientHelloSpec rotation,
// per-request ForceHTTP3, custom transport settings). Callers who reach for
// this accept that they're using an unstable surface — the field set may
// change with future azuretls releases.
func (f *StealthFetcher) Session() *azuretls.Session { return f.session }

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

	// Warn when both pendingJA3 and pendingHTTP2 are set via the bare With*
	// options. azuretls.Session.ApplyHTTP2 bypasses browser-aware HTTP/2
	// defaults (defaultHeaderPriorities, defaultStreamPriorities), producing a
	// half-Firefox/half-generic HTTP/2 fingerprint that deep validators
	// (Akamai, Bing) reject. WithBundle is the safe path. See issue #41.
	if f.pendingJA3 != "" && f.pendingHTTP2 != "" {
		slog.Warn("fetch/stealth: pairing WithJA3 + WithHTTP2Fingerprint may be rejected by deep fingerprint validators (issue #41); prefer fetch.WithBundle for matched bundles",
			"browser", f.session.Browser,
		)
	}

	// Apply fingerprint customisations after all options ran so browser family
	// selection from WithIdentity (or default Firefox) is finalised before
	// ApplyJa3 inherits its defaults.
	if f.pendingJA3 != "" {
		if err := f.session.ApplyJa3(f.pendingJA3, f.session.Browser); err != nil {
			slog.Error("fetch/stealth: invalid JA3, falling back to default preset",
				"ja3", f.pendingJA3, "browser", f.session.Browser, "err", err)
			f.pendingJA3 = ""
		}
	}
	if f.pendingHTTP2 != "" {
		if err := f.session.ApplyHTTP2(f.pendingHTTP2); err != nil {
			slog.Error("fetch/stealth: invalid HTTP/2 fingerprint, falling back to default",
				"http2", f.pendingHTTP2, "err", err)
			f.pendingHTTP2 = ""
		}
	}
	if f.pendingHTTP3 != "" {
		if err := f.session.ApplyHTTP3(f.pendingHTTP3); err != nil {
			slog.Error("fetch/stealth: invalid HTTP/3 fingerprint, falling back to default",
				"http3", f.pendingHTTP3, "err", err)
			f.pendingHTTP3 = ""
		}
	}

	slog.Info("fetch/stealth: initialized",
		"tls_impersonation", true,
		"build_tag", "tls",
		"browser", f.session.Browser,
		"ja3_custom", f.pendingJA3 != "",
		"http2_custom", f.pendingHTTP2 != "",
		"http3_custom", f.pendingHTTP3 != "",
	)

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
	var azureResp *azuretls.Response
	var lastErr error
	const maxRetries = 2
	for attempt := 0; attempt <= maxRetries; attempt++ {
		azureResp, lastErr = f.session.Do(req)
		if lastErr == nil {
			break
		}
		if !isTransientError(lastErr) || attempt == maxRetries {
			break
		}
		delay := time.Duration(500*(attempt+1)) * time.Millisecond
		slog.Debug("fetch/stealth: transient error, retrying",
			"url", job.URL, "attempt", attempt+1, "delay", delay, "err", lastErr)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	duration := time.Since(start)
	if lastErr != nil {
		return nil, fmt.Errorf("fetch/stealth: request to %s failed: %w", job.URL, lastErr)
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

	// Convert azuretls cookies (map[string]string) to []*http.Cookie.
	var respCookies []*http.Cookie
	for name, value := range azureResp.Cookies {
		respCookies = append(respCookies, &http.Cookie{
			Name:  name,
			Value: value,
		})
	}

	return &foxhound.Response{
		StatusCode: azureResp.StatusCode,
		Headers:    stdHeaders,
		Body:       azureResp.Body,
		URL:        finalURL,
		FetchMode:  foxhound.FetchStatic,
		Duration:   duration,
		Job:        job,
		Cookies:    respCookies,
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
			"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:135.0) Gecko/20100101 Firefox/135.0",
			"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"Accept-Language": "en-US,en;q=0.5",
			"Accept-Encoding": "gzip, deflate, br",
		}
		headerOrder = []string{"User-Agent", "Accept", "Accept-Language", "Accept-Encoding"}
	} else {
		p := f.identity
		headerValues = map[string]string{
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
