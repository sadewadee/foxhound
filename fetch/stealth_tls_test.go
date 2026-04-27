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

// TestTLSStealthFetcher_AppliesPresetBundle runs every curated bundle through
// ApplyJa3 + ApplyHTTP2 inside NewStealth. azuretls validates the strings
// during apply, so a parsing regression in any preset surfaces here.
func TestTLSStealthFetcher_AppliesPresetBundle(t *testing.T) {
	for _, b := range presets.All() {
		t.Run(b.Name, func(t *testing.T) {
			f := fetch.NewStealth(
				fetch.WithIdentity(identity.Generate(
					identity.WithBrowser(identity.BrowserFirefox),
				)),
				fetch.WithJA3(b.JA3),
				fetch.WithHTTP2Fingerprint(b.HTTP2),
			)
			defer f.Close()
			if f.Session() == nil {
				t.Fatal("Session() returned nil")
			}
		})
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
func TestTLSStealthFetcher_JA3PoolPicks(t *testing.T) {
	pool := presets.JA3Pool(presets.All())
	f := fetch.NewStealth(
		fetch.WithIdentity(identity.Generate()),
		fetch.WithJA3Pool(pool),
	)
	defer f.Close()
	if f.Session() == nil {
		t.Fatal("Session() returned nil")
	}
}
