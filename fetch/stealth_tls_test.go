//go:build tls

package fetch_test

// stealth_tls_test.go: build-tagged tests that run only with -tags tls.
//
// These tests verify that the azuretls-backed StealthFetcher:
//   - Behaves identically to the default fetcher from the caller's perspective.
//   - Accepts proxy and identity options without panicking.
//   - Correctly maps the identity.Profile.BrowserName to the azuretls browser profile.
//
// Run: go test -tags tls ./fetch/...

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/fetch/presets"
	"github.com/sadewadee/foxhound/identity"
)

// TestTLSStealthFetcher_FetchReturnsResponse verifies the azuretls-backed fetcher
// performs a successful GET and returns FetchMode=FetchStatic with the correct body.
func TestTLSStealthFetcher_FetchReturnsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "hello tls foxhound")
	}))
	defer srv.Close()

	f := fetch.NewStealth(
		fetch.WithIdentity(identity.Generate(
			identity.WithBrowser(identity.BrowserFirefox),
			identity.WithOS(identity.OSWindows),
		)),
		fetch.WithTimeout(5*time.Second),
	)
	defer f.Close()

	resp, err := f.Fetch(context.Background(), newJob(srv.URL))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if string(resp.Body) != "hello tls foxhound" {
		t.Errorf("expected body 'hello tls foxhound', got %q", string(resp.Body))
	}
}

// TestTLSStealthFetcher_SetsUserAgentFromIdentity verifies that the azuretls session
// forwards the User-Agent derived from the identity profile.
func TestTLSStealthFetcher_SetsUserAgentFromIdentity(t *testing.T) {
	profile := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
	)

	var receivedUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := fetch.NewStealth(fetch.WithIdentity(profile))
	defer f.Close()

	_, err := f.Fetch(context.Background(), newJob(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedUA != profile.UA {
		t.Errorf("expected UA %q, got %q", profile.UA, receivedUA)
	}
}

// TestTLSStealthFetcher_ChromeIdentityAccepted verifies that a Chrome identity
// profile is accepted without error (azuretls maps it to its Chrome TLS profile).
func TestTLSStealthFetcher_ChromeIdentityAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := fetch.NewStealth(
		fetch.WithIdentity(identity.Generate(
			identity.WithBrowser(identity.BrowserChrome),
			identity.WithOS(identity.OSMacOS),
		)),
		fetch.WithTimeout(5*time.Second),
	)
	defer f.Close()

	resp, err := f.Fetch(context.Background(), newJob(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error with Chrome identity: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestTLSStealthFetcher_PostWithBody verifies that a POST request sends the body
// through the azuretls session correctly.
func TestTLSStealthFetcher_PostWithBody(t *testing.T) {
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		buf := make([]byte, 512)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	job := newJob(srv.URL)
	job.Method = http.MethodPost
	job.Body = []byte(`{"key":"value"}`)

	f := fetch.NewStealth(fetch.WithTimeout(5 * time.Second))
	defer f.Close()

	_, err := f.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedBody != `{"key":"value"}` {
		t.Errorf("expected body %q, got %q", `{"key":"value"}`, receivedBody)
	}
}

// TestTLSStealthFetcher_WithProxy_InvalidURLLogged verifies that passing an invalid
// proxy URL does not panic and returns a usable fetcher (the error is logged and
// the proxy setting is skipped).
func TestTLSStealthFetcher_WithProxy_InvalidURLLogged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Invalid proxy: should be silently skipped with a log line, not panic.
	f := fetch.NewStealth(
		fetch.WithProxy("not-a-valid-proxy-url"),
		fetch.WithTimeout(5*time.Second),
	)
	defer f.Close()

	// Should still work when hitting a local server (proxy skipped).
	resp, err := f.Fetch(context.Background(), newJob(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error after invalid proxy: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestTLSStealthFetcher_AcceptLanguageFromIdentity verifies Accept-Language is
// set from the identity profile in the TLS-backed implementation.
func TestTLSStealthFetcher_AcceptLanguageFromIdentity(t *testing.T) {
	profile := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithLocale("en-US", "en-US", "en"),
	)

	var receivedLang string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedLang = r.Header.Get("Accept-Language")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := fetch.NewStealth(fetch.WithIdentity(profile))
	defer f.Close()

	_, err := f.Fetch(context.Background(), newJob(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedLang == "" {
		t.Error("expected Accept-Language to be set")
	}
	if !strings.Contains(receivedLang, "en") {
		t.Errorf("Accept-Language %q does not contain 'en'", receivedLang)
	}
}

// TestTLSStealthFetcher_ContextCancellation verifies that cancelling the context
// causes Fetch to return an error in the TLS implementation.
func TestTLSStealthFetcher_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	f := fetch.NewStealth(fetch.WithTimeout(5 * time.Second))
	defer f.Close()

	_, err := f.Fetch(ctx, newJob(srv.URL))
	if err == nil {
		t.Error("expected error on cancelled context, got nil")
	}
}

// TestTLSStealthFetcher_IsImpersonating pins the build-tag contract: the tls
// build of NewStealth must report IsImpersonating()==true.
func TestTLSStealthFetcher_IsImpersonating(t *testing.T) {
	f := fetch.NewStealth()
	defer f.Close()
	if !f.IsImpersonating() {
		t.Fatal("tls build must return IsImpersonating()==true")
	}
}

// TestTLSStealthFetcher_AppliesFirefoxPreset runs the curated Firefox JA3
// through ApplyJa3 inside NewStealth. azuretls validates the JA3 string at
// apply time, so a parsing regression surfaces here.
func TestTLSStealthFetcher_AppliesFirefoxPreset(t *testing.T) {
	b := presets.FirefoxLatest()
	f := fetch.NewStealth(
		fetch.WithIdentity(identity.Generate(
			identity.WithBrowser(identity.BrowserFirefox),
		)),
		fetch.WithJA3(b.JA3),
	)
	defer f.Close()
	if f.Session() == nil {
		t.Fatal("Session() returned nil")
	}
}

// TestTLSStealthFetcher_InvalidJA3FallsBack confirms a malformed JA3 is logged
// and ignored rather than panicking. The fetcher must remain usable.
func TestTLSStealthFetcher_InvalidJA3FallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := fetch.NewStealth(
		fetch.WithIdentity(identity.Generate()),
		fetch.WithJA3("not-a-real-ja3"),
		fetch.WithTimeout(5*time.Second),
	)
	defer f.Close()

	resp, err := f.Fetch(context.Background(), newJob(srv.URL))
	if err != nil {
		t.Fatalf("invalid JA3 should not break Fetch, got: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestTLSStealthFetcher_JA3PoolPicks verifies a non-empty pool is accepted.
// Pools are caller-supplied (e.g. multiple Firefox JA3 captures from
// tls.peet.ws over time). foxhound itself only ships one curated JA3 because
// the browser layer is Camoufox (Firefox) only.
func TestTLSStealthFetcher_JA3PoolPicks(t *testing.T) {
	pool := []string{presets.FirefoxLatest().JA3}
	f := fetch.NewStealth(
		fetch.WithIdentity(identity.Generate()),
		fetch.WithJA3Pool(pool),
	)
	defer f.Close()
	if f.Session() == nil {
		t.Fatal("Session() returned nil")
	}
}

// TestWithIdentity_FirefoxSetsBrowser verifies that a Firefox identity
// configures session.Browser="firefox" so azuretls's built-in Firefox
// ClientHelloSpec is used at request time. We deliberately do NOT auto-apply
// any captured JA3 — the built-in spec tracks current Firefox releases more
// reliably than hand-captured strings (see issue #41 commentary).
func TestWithIdentity_FirefoxSetsBrowser(t *testing.T) {
	p := identity.Generate(identity.WithBrowser(identity.BrowserFirefox))
	f := fetch.NewStealth(fetch.WithIdentity(p))
	defer f.Close()

	if got := f.Session().Browser; got != "firefox" {
		t.Errorf("session.Browser = %q, want %q", got, "firefox")
	}
	// GetClientHelloSpec must be nil — azuretls's built-in
	// GetLastFirefoxVersion handles ClientHello at request time.
	if f.Session().GetClientHelloSpec != nil {
		t.Error("session.GetClientHelloSpec is non-nil; WithIdentity must not auto-apply a captured JA3")
	}
	// HTTP2Transport must be nil — never call ApplyHTTP2 from WithIdentity.
	if f.Session().HTTP2Transport != nil {
		t.Error("session.HTTP2Transport is non-nil; WithIdentity must not call ApplyHTTP2 (issue #41)")
	}
}

// TestWithIdentity_ExplicitJA3StillWorks verifies that an explicit WithJA3
// is still applied when paired with WithIdentity. Power users with a freshly-
// captured JA3 from tls.peet.ws can override the azuretls built-in.
func TestWithIdentity_ExplicitJA3StillWorks(t *testing.T) {
	custom := presets.FirefoxLatest().JA3
	p := identity.Generate(identity.WithBrowser(identity.BrowserFirefox))
	f := fetch.NewStealth(
		fetch.WithIdentity(p),
		fetch.WithJA3(custom),
	)
	defer f.Close()
	if f.Session().GetClientHelloSpec == nil {
		t.Error("session.GetClientHelloSpec is nil; explicit WithJA3 should be applied")
	}
}

// TestWithIdentity_ChromeSetsBrowser verifies non-Firefox identity paths
// also configure session.Browser correctly so azuretls picks the matching
// built-in ClientHello.
func TestWithIdentity_ChromeSetsBrowser(t *testing.T) {
	p := identity.Generate(identity.WithBrowser(identity.BrowserChrome))
	f := fetch.NewStealth(fetch.WithIdentity(p))
	defer f.Close()

	if got := f.Session().Browser; got != "chrome" {
		t.Errorf("session.Browser = %q, want %q", got, "chrome")
	}
	if f.Session().GetClientHelloSpec != nil {
		t.Error("session.GetClientHelloSpec must be nil; rely on azuretls built-in spec")
	}
}

// TestNewStealth_DefaultInsecureSkipVerify verifies that NewStealth with no
// options sets InsecureSkipVerify=true by default. This disables azuretls's
// DefaultPinManager which would otherwise break against multi-edge CDN targets
// (Bing, Google, Cloudflare) by caching SPKI pins from the first edge and then
// failing on the second edge which presents a different certificate. No network
// required — introspects the session field directly.
func TestNewStealth_DefaultInsecureSkipVerify(t *testing.T) {
	f := fetch.NewStealth()
	defer f.Close()
	if !f.Session().InsecureSkipVerify {
		t.Error("default: InsecureSkipVerify must be true to prevent PinManager multi-edge failures (v0.0.20)")
	}
}

// TestNewStealth_WithStrictTLSVerify_DisablesInsecureSkipVerify verifies that
// WithStrictTLSVerify() re-enables full certificate chain, hostname, and pin
// verification by flipping InsecureSkipVerify back to false. No network.
func TestNewStealth_WithStrictTLSVerify_DisablesInsecureSkipVerify(t *testing.T) {
	f := fetch.NewStealth(fetch.WithStrictTLSVerify())
	defer f.Close()
	if f.Session().InsecureSkipVerify {
		t.Error("WithStrictTLSVerify: InsecureSkipVerify must be false (strict mode on)")
	}
}

// TestNewStealth_WithStrictTLSVerify_CoexistsWithIdentity verifies that
// WithStrictTLSVerify and WithIdentity can be combined without interference:
// strict mode is active and the browser family is correctly set from the
// identity profile. Order of options must not matter.
func TestNewStealth_WithStrictTLSVerify_CoexistsWithIdentity(t *testing.T) {
	p := identity.Generate(identity.WithBrowser(identity.BrowserFirefox))
	f := fetch.NewStealth(
		fetch.WithIdentity(p),
		fetch.WithStrictTLSVerify(),
	)
	defer f.Close()
	if f.Session().InsecureSkipVerify {
		t.Error("WithStrictTLSVerify + WithIdentity: InsecureSkipVerify must be false")
	}
	if got := f.Session().Browser; got != "firefox" {
		t.Errorf("session.Browser = %q, want %q after WithIdentity(firefox)", got, "firefox")
	}
}
