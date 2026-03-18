//go:build playwright

// Package fetch_test — playwright-tagged tests for CamoufoxFetcher.
//
// These tests exercise the real playwright-go implementation.
// They are only compiled when you run:
//
//	go test -tags playwright ./fetch/...
//
// Each test calls t.Skip when playwright / Camoufox binaries are not present
// so the suite does not fail in a bare CI environment.
package fetch_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/fetch"
	"github.com/foxhound-scraper/foxhound/identity"
)

// skipIfPlaywrightUnavailable skips the calling test when playwright or
// Camoufox binaries are not installed in the current environment.
// The CamoufoxFetcher constructor returns an error containing "not found" or
// "executable" when the binary is missing; we use that as the signal.
func skipIfPlaywrightUnavailable(t *testing.T, constructErr error) {
	t.Helper()
	if constructErr == nil {
		return
	}
	msg := constructErr.Error()
	if strings.Contains(msg, "not found") ||
		strings.Contains(msg, "executable") ||
		strings.Contains(msg, "install") ||
		strings.Contains(msg, "Executable") {
		t.Skipf("playwright/camoufox not installed, skipping: %v", constructErr)
	}
	// Any other error is a real failure — let the test fail.
}

// ---------------------------------------------------------------------------
// Construction tests
// ---------------------------------------------------------------------------

// TestPlaywrightCamoufox_NewCamoufoxConstructsSuccessfully verifies that
// NewCamoufox initialises playwright and returns a non-nil CamoufoxFetcher
// when the binary is available.
func TestPlaywrightCamoufox_NewCamoufoxConstructsSuccessfully(t *testing.T) {
	profile := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
	)

	f, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(profile),
		fetch.WithHeadless("true"),
		fetch.WithBlockImages(false),
	)
	skipIfPlaywrightUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewCamoufox returned unexpected error: %v", err)
	}
	if f == nil {
		t.Fatal("NewCamoufox returned nil fetcher")
	}
	defer f.Close()
}

// TestPlaywrightCamoufox_ImplementsFetcherInterface is a compile-time check
// that the playwright-built CamoufoxFetcher satisfies foxhound.Fetcher.
func TestPlaywrightCamoufox_ImplementsFetcherInterface(t *testing.T) {
	f, err := fetch.NewCamoufox(fetch.WithHeadless("true"))
	skipIfPlaywrightUnavailable(t, err)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer f.Close()
	var _ foxhound.Fetcher = f
}

// ---------------------------------------------------------------------------
// Fetch behaviour tests
// ---------------------------------------------------------------------------

// TestPlaywrightCamoufox_FetchReturnsHTMLContent verifies that Fetch navigates
// to a local test server and returns the HTML body with FetchMode=FetchBrowser.
func TestPlaywrightCamoufox_FetchReturnsHTMLContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<html><body><p id="target">foxhound-browser</p></body></html>`)
	}))
	defer srv.Close()

	profile := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSLinux),
	)

	f, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(profile),
		fetch.WithHeadless("true"),
		fetch.WithBlockImages(true),
	)
	skipIfPlaywrightUnavailable(t, err)
	if err != nil {
		t.Fatalf("unexpected error creating fetcher: %v", err)
	}
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	job := &foxhound.Job{
		ID:        "pw-test-1",
		URL:       srv.URL,
		Method:    http.MethodGet,
		FetchMode: foxhound.FetchBrowser,
		CreatedAt: time.Now(),
	}

	resp, err := f.Fetch(ctx, job)
	if err != nil {
		t.Fatalf("Fetch returned unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("Fetch returned nil response")
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if resp.FetchMode != foxhound.FetchBrowser {
		t.Errorf("expected FetchMode=FetchBrowser, got %v", resp.FetchMode)
	}
	if !strings.Contains(string(resp.Body), "foxhound-browser") {
		t.Errorf("expected body to contain 'foxhound-browser', got: %s", string(resp.Body))
	}
	if resp.URL == "" {
		t.Error("expected resp.URL to be non-empty")
	}
	if resp.Job == nil {
		t.Error("expected resp.Job to reference the original job")
	}
	if resp.Duration <= 0 {
		t.Error("expected positive Duration")
	}
}

// TestPlaywrightCamoufox_FetchSetsResponseURL verifies that resp.URL is the
// final URL seen after navigation (respects redirects).
func TestPlaywrightCamoufox_FetchSetsResponseURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "<html><body>ok</body></html>")
	}))
	defer srv.Close()

	f, err := fetch.NewCamoufox(fetch.WithHeadless("true"))
	skipIfPlaywrightUnavailable(t, err)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := f.Fetch(ctx, &foxhound.Job{
		ID:  "pw-url-test",
		URL: srv.URL,
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if resp.URL == "" {
		t.Error("expected resp.URL to be populated")
	}
}

// TestPlaywrightCamoufox_FetchMeasuresDuration verifies that Fetch populates
// a positive Duration on the response.
func TestPlaywrightCamoufox_FetchMeasuresDuration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "<html><body>timing</body></html>")
	}))
	defer srv.Close()

	f, err := fetch.NewCamoufox(fetch.WithHeadless("true"))
	skipIfPlaywrightUnavailable(t, err)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := f.Fetch(ctx, &foxhound.Job{ID: "pw-timing", URL: srv.URL})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if resp.Duration <= 0 {
		t.Errorf("expected Duration > 0, got %v", resp.Duration)
	}
}

// TestPlaywrightCamoufox_FetchRejectsNilJob verifies that Fetch returns an
// error immediately when given a nil job rather than panicking.
func TestPlaywrightCamoufox_FetchRejectsNilJob(t *testing.T) {
	f, err := fetch.NewCamoufox(fetch.WithHeadless("true"))
	skipIfPlaywrightUnavailable(t, err)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer f.Close()

	_, fetchErr := f.Fetch(context.Background(), nil)
	if fetchErr == nil {
		t.Error("expected error when job is nil, got nil")
	}
}

// TestPlaywrightCamoufox_FetchRejectsEmptyURL verifies that Fetch returns an
// error when the job URL is empty.
func TestPlaywrightCamoufox_FetchRejectsEmptyURL(t *testing.T) {
	f, err := fetch.NewCamoufox(fetch.WithHeadless("true"))
	skipIfPlaywrightUnavailable(t, err)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer f.Close()

	_, fetchErr := f.Fetch(context.Background(), &foxhound.Job{ID: "empty-url", URL: ""})
	if fetchErr == nil {
		t.Error("expected error when URL is empty, got nil")
	}
}

// TestPlaywrightCamoufox_FetchContextCancellation verifies that a cancelled
// context causes Fetch to return an error instead of hanging.
func TestPlaywrightCamoufox_FetchContextCancellation(t *testing.T) {
	// Server blocks forever; the context cancellation must abort navigation.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	f, err := fetch.NewCamoufox(fetch.WithHeadless("true"))
	skipIfPlaywrightUnavailable(t, err)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before issuing request

	_, fetchErr := f.Fetch(ctx, &foxhound.Job{ID: "cancel-test", URL: srv.URL})
	if fetchErr == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

// TestPlaywrightCamoufox_MultipleSequentialFetches verifies that the same
// CamoufoxFetcher instance can handle multiple Fetch calls sequentially
// without leaking resources or panicking.
func TestPlaywrightCamoufox_MultipleSequentialFetches(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "<html><body>call-%d</body></html>", callCount)
	}))
	defer srv.Close()

	f, err := fetch.NewCamoufox(
		fetch.WithHeadless("true"),
		fetch.WithBlockImages(true),
	)
	skipIfPlaywrightUnavailable(t, err)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer f.Close()

	for i := range 3 {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		resp, fetchErr := f.Fetch(ctx, &foxhound.Job{
			ID:  fmt.Sprintf("seq-%d", i),
			URL: srv.URL,
		})
		cancel()

		if fetchErr != nil {
			t.Errorf("call %d: unexpected error: %v", i, fetchErr)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("call %d: expected 200, got %d", i, resp.StatusCode)
		}
	}
}

// TestPlaywrightCamoufox_CloseReleasesResources verifies that Close() returns
// nil and repeated calls to Close do not panic.
func TestPlaywrightCamoufox_CloseReleasesResources(t *testing.T) {
	f, err := fetch.NewCamoufox(fetch.WithHeadless("true"))
	skipIfPlaywrightUnavailable(t, err)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if closeErr := f.Close(); closeErr != nil {
		t.Errorf("first Close() returned error: %v", closeErr)
	}
	// Second Close should not panic; may return an error (browser already closed).
	// We allow an error here but guard against panics.
	_ = f.Close()
}

// TestPlaywrightCamoufox_WithIdentityAppliesProfile verifies that providing an
// identity profile via WithBrowserIdentity does not cause NewCamoufox to error.
func TestPlaywrightCamoufox_WithIdentityAppliesProfile(t *testing.T) {
	profile := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSMacOS),
		identity.WithTimezone("America/New_York"),
		identity.WithLocale("en-US", "en-US", "en"),
		identity.WithGeo(40.7128, -74.0060),
	)

	f, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(profile),
		fetch.WithHeadless("true"),
	)
	skipIfPlaywrightUnavailable(t, err)
	if err != nil {
		t.Fatalf("NewCamoufox with full identity returned error: %v", err)
	}
	defer f.Close()
}
