// Package fetch_test contains tests for the fetch layer.
//
// TDD cycle: each test was written before the implementation it exercises.
// Tests cover StealthFetcher, CamoufoxFetcher (stub), and SmartFetcher.
package fetch_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newJob builds a minimal Job for testing.
func newJob(url string) *foxhound.Job {
	return &foxhound.Job{
		ID:        "test-job-1",
		URL:       url,
		Method:    http.MethodGet,
		FetchMode: foxhound.FetchAuto,
		CreatedAt: time.Now(),
	}
}

// testProfile returns a deterministic identity profile for tests.
func testProfile() *identity.Profile {
	return identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
	)
}

// ---------------------------------------------------------------------------
// StealthFetcher tests
// ---------------------------------------------------------------------------

// TestStealthFetcher_FetchReturnsResponse verifies that StealthFetcher performs
// a successful GET and returns a Response with FetchMode=FetchStatic.
func TestStealthFetcher_FetchReturnsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "hello foxhound")
	}))
	defer srv.Close()

	f := fetch.NewStealth(
		fetch.WithIdentity(testProfile()),
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
	if string(resp.Body) != "hello foxhound" {
		t.Errorf("expected body 'hello foxhound', got %q", string(resp.Body))
	}
	if resp.FetchMode != foxhound.FetchStatic {
		t.Errorf("expected FetchMode=FetchStatic, got %v", resp.FetchMode)
	}
	if resp.Job == nil {
		t.Error("expected resp.Job to be set")
	}
	if resp.Duration <= 0 {
		t.Error("expected positive Duration")
	}
}

// TestStealthFetcher_SetsUserAgentFromIdentity verifies that StealthFetcher
// sends the User-Agent from the supplied identity profile.
func TestStealthFetcher_SetsUserAgentFromIdentity(t *testing.T) {
	profile := testProfile()

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

// TestStealthFetcher_SetsAcceptLanguageFromIdentity verifies that the
// Accept-Language header reflects the profile's Languages field.
func TestStealthFetcher_SetsAcceptLanguageFromIdentity(t *testing.T) {
	profile := testProfile()

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
	// Must contain the primary language tag from the profile.
	primaryLang := profile.Languages[0]
	if !strings.Contains(receivedLang, primaryLang[:2]) {
		t.Errorf("Accept-Language %q does not include profile language %q", receivedLang, primaryLang)
	}
}

// TestStealthFetcher_JobHeadersMerged verifies that headers set on the Job are
// sent alongside the identity headers.
func TestStealthFetcher_JobHeadersMerged(t *testing.T) {
	var receivedToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("X-Custom-Token")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	job := newJob(srv.URL)
	job.Headers = http.Header{"X-Custom-Token": []string{"abc123"}}

	f := fetch.NewStealth(fetch.WithIdentity(testProfile()))
	defer f.Close()

	_, err := f.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedToken != "abc123" {
		t.Errorf("expected X-Custom-Token 'abc123', got %q", receivedToken)
	}
}

// TestStealthFetcher_ContextCancellation verifies that a cancelled context
// causes Fetch to return an error.
func TestStealthFetcher_ContextCancellation(t *testing.T) {
	// Server that blocks until the test ends — ensures the client must cancel.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before issuing request

	f := fetch.NewStealth(fetch.WithTimeout(5 * time.Second))
	defer f.Close()

	_, err := f.Fetch(ctx, newJob(srv.URL))
	if err == nil {
		t.Error("expected error on cancelled context, got nil")
	}
}

// TestStealthFetcher_ReturnsURLFromResponse verifies that resp.URL is populated.
func TestStealthFetcher_ReturnsURLFromResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := fetch.NewStealth()
	defer f.Close()

	resp, err := f.Fetch(context.Background(), newJob(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.URL == "" {
		t.Error("expected resp.URL to be non-empty")
	}
}

// TestStealthFetcher_PostWithBody verifies that a POST request sends the body.
func TestStealthFetcher_PostWithBody(t *testing.T) {
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

	f := fetch.NewStealth()
	defer f.Close()

	_, err := f.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedBody != `{"key":"value"}` {
		t.Errorf("expected body %q, got %q", `{"key":"value"}`, receivedBody)
	}
}

// TestStealthFetcher_ImplementsFetcherInterface ensures compile-time interface
// satisfaction.
func TestStealthFetcher_ImplementsFetcherInterface(t *testing.T) {
	var _ foxhound.Fetcher = fetch.NewStealth()
}

// ---------------------------------------------------------------------------
// CamoufoxFetcher (stub) tests
// ---------------------------------------------------------------------------

// TestCamoufoxFetcher_ReturnsNotConfiguredError verifies the stub returns a
// clear actionable error rather than panicking or returning nil.
func TestCamoufoxFetcher_ReturnsNotConfiguredError(t *testing.T) {
	f, err := fetch.NewCamoufox()
	if err != nil {
		t.Fatalf("NewCamoufox should not fail at construction: %v", err)
	}
	defer f.Close()

	_, fetchErr := f.Fetch(context.Background(), newJob("https://example.com"))
	if fetchErr == nil {
		t.Fatal("expected error from stub Fetch, got nil")
	}
	if !strings.Contains(fetchErr.Error(), "playwright-go") {
		t.Errorf("expected error to mention 'playwright-go', got: %v", fetchErr)
	}
}

// TestCamoufoxFetcher_CloseIsNoop verifies that Close() returns no error on
// the stub.
func TestCamoufoxFetcher_CloseIsNoop(t *testing.T) {
	f, _ := fetch.NewCamoufox()
	if err := f.Close(); err != nil {
		t.Errorf("expected Close to return nil, got: %v", err)
	}
}

// TestCamoufoxFetcher_ImplementsFetcherInterface ensures compile-time interface
// satisfaction.
func TestCamoufoxFetcher_ImplementsFetcherInterface(t *testing.T) {
	f, _ := fetch.NewCamoufox()
	var _ foxhound.Fetcher = f
}

// TestCamoufoxFetcher_WithOptions verifies the option functions compile and
// apply without panicking.
func TestCamoufoxFetcher_WithOptions(t *testing.T) {
	profile := testProfile()
	f, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(profile),
		fetch.WithBlockImages(true),
		fetch.WithHeadless("virtual"),
	)
	if err != nil {
		t.Fatalf("NewCamoufox with options should not fail: %v", err)
	}
	defer f.Close()
}

// ---------------------------------------------------------------------------
// DefaultBlockDetector tests
// ---------------------------------------------------------------------------

// TestDefaultBlockDetector_BlockedOnKnownStatusCodes verifies that the detector
// flags 401, 403, 407, 429, and 503 as blocked.
func TestDefaultBlockDetector_BlockedOnKnownStatusCodes(t *testing.T) {
	d := &fetch.DefaultBlockDetector{}

	blockedCodes := []int{401, 403, 407, 429, 503}
	for _, code := range blockedCodes {
		resp := &foxhound.Response{StatusCode: code}
		if !d.IsBlocked(resp) {
			t.Errorf("expected status %d to be detected as blocked", code)
		}
	}
}

// TestDefaultBlockDetector_NotBlockedOnSuccess verifies that 200, 201, 204,
// 301, and 404 are NOT treated as blocks.
func TestDefaultBlockDetector_NotBlockedOnSuccess(t *testing.T) {
	d := &fetch.DefaultBlockDetector{}

	passCodes := []int{200, 201, 204, 301, 302, 404}
	for _, code := range passCodes {
		resp := &foxhound.Response{StatusCode: code}
		if d.IsBlocked(resp) {
			t.Errorf("status %d should NOT be detected as blocked", code)
		}
	}
}

// ---------------------------------------------------------------------------
// SmartFetcher tests
// ---------------------------------------------------------------------------

// TestSmartFetcher_FetchAutoUsesStaticFirst verifies that in FetchAuto mode the
// static fetcher is tried first when the response is not blocked.
func TestSmartFetcher_FetchAutoUsesStaticFirst(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "static response")
	}))
	defer srv.Close()

	staticF := fetch.NewStealth(fetch.WithTimeout(5 * time.Second))
	smart := fetch.NewSmart(staticF, nil)
	defer smart.Close()

	job := newJob(srv.URL)
	job.FetchMode = foxhound.FetchAuto

	resp, err := smart.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FetchMode != foxhound.FetchStatic {
		t.Errorf("expected FetchStatic on clean response, got %v", resp.FetchMode)
	}
	if string(resp.Body) != "static response" {
		t.Errorf("expected 'static response', got %q", string(resp.Body))
	}
}

// TestSmartFetcher_FetchAutoEscalatesToBrowserOnBlock verifies that when the
// static fetcher returns a blocked status code, SmartFetcher escalates to the
// browser fetcher.
func TestSmartFetcher_FetchAutoEscalatesToBrowserOnBlock(t *testing.T) {
	// Static server returns 403 (blocked).
	staticSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer staticSrv.Close()

	// Browser server returns 200 with content.
	browserSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "browser response")
	}))
	defer browserSrv.Close()

	// Use a custom mock for the browser fetcher that always hits browserSrv.
	browserF := &mockFetcher{
		fn: func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, browserSrv.URL, nil)
			httpResp, err := http.DefaultClient.Do(req)
			if err != nil {
				return nil, err
			}
			defer httpResp.Body.Close()
			body := make([]byte, 512)
			n, _ := httpResp.Body.Read(body)
			return &foxhound.Response{
				StatusCode: httpResp.StatusCode,
				Body:       body[:n],
				URL:        browserSrv.URL,
				FetchMode:  foxhound.FetchBrowser,
				Job:        job,
			}, nil
		},
	}

	staticF := fetch.NewStealth(fetch.WithTimeout(5 * time.Second))
	smart := fetch.NewSmart(staticF, browserF)
	defer smart.Close()

	job := newJob(staticSrv.URL) // static will hit the 403 server
	job.FetchMode = foxhound.FetchAuto

	resp, err := smart.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FetchMode != foxhound.FetchBrowser {
		t.Errorf("expected FetchBrowser after escalation, got %v", resp.FetchMode)
	}
	if string(resp.Body) != "browser response" {
		t.Errorf("expected 'browser response', got %q", string(resp.Body))
	}
}

// TestSmartFetcher_FetchStaticModeSkipsBrowserEvenIfBlocked verifies that
// FetchStatic mode never escalates, even when blocked.
func TestSmartFetcher_FetchStaticModeSkipsBrowserEvenIfBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	browserCalled := false
	browserF := &mockFetcher{
		fn: func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
			browserCalled = true
			return &foxhound.Response{StatusCode: 200, FetchMode: foxhound.FetchBrowser, Job: job}, nil
		},
	}

	staticF := fetch.NewStealth(fetch.WithTimeout(5 * time.Second))
	smart := fetch.NewSmart(staticF, browserF)
	defer smart.Close()

	job := newJob(srv.URL)
	job.FetchMode = foxhound.FetchStatic

	resp, err := smart.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if browserCalled {
		t.Error("browser fetcher should NOT be called in FetchStatic mode")
	}
	if resp.FetchMode != foxhound.FetchStatic {
		t.Errorf("expected FetchStatic, got %v", resp.FetchMode)
	}
}

// TestSmartFetcher_FetchBrowserModeUsedDirectly verifies that FetchBrowser mode
// goes straight to the browser fetcher, bypassing the static fetcher.
func TestSmartFetcher_FetchBrowserModeUsedDirectly(t *testing.T) {
	staticCalled := false
	staticF := &mockFetcher{
		fn: func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
			staticCalled = true
			return &foxhound.Response{StatusCode: 200, FetchMode: foxhound.FetchStatic, Job: job}, nil
		},
	}
	browserF := &mockFetcher{
		fn: func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
			return &foxhound.Response{StatusCode: 200, FetchMode: foxhound.FetchBrowser, Job: job}, nil
		},
	}

	smart := fetch.NewSmart(staticF, browserF)
	defer smart.Close()

	job := newJob("https://example.com")
	job.FetchMode = foxhound.FetchBrowser

	resp, err := smart.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if staticCalled {
		t.Error("static fetcher should NOT be called in FetchBrowser mode")
	}
	if resp.FetchMode != foxhound.FetchBrowser {
		t.Errorf("expected FetchBrowser, got %v", resp.FetchMode)
	}
}

// TestSmartFetcher_NilBrowserAlwaysUsesStatic verifies that SmartFetcher works
// safely when no browser fetcher is provided.
func TestSmartFetcher_NilBrowserAlwaysUsesStatic(t *testing.T) {
	// Returns 403, but with nil browser there is no escalation path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	staticF := fetch.NewStealth(fetch.WithTimeout(5 * time.Second))
	smart := fetch.NewSmart(staticF, nil)
	defer smart.Close()

	job := newJob(srv.URL)
	job.FetchMode = foxhound.FetchAuto

	resp, err := smart.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 passthrough, got %d", resp.StatusCode)
	}
}

// TestSmartFetcher_CustomBlockDetector verifies that a custom BlockDetector
// can be injected via WithBlockDetector.
func TestSmartFetcher_CustomBlockDetector(t *testing.T) {
	// Custom detector considers 404 a block (unusual, but tests injection).
	alwaysBlocked := &alwaysBlockDetector{}

	browserCalled := false
	browserF := &mockFetcher{
		fn: func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
			browserCalled = true
			return &foxhound.Response{StatusCode: 200, FetchMode: foxhound.FetchBrowser, Job: job}, nil
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // 200, but custom detector will still say blocked
	}))
	defer srv.Close()

	staticF := fetch.NewStealth(fetch.WithTimeout(5 * time.Second))
	smart := fetch.NewSmart(staticF, browserF,
		fetch.WithBlockDetector(alwaysBlocked),
	)
	defer smart.Close()

	job := newJob(srv.URL)
	job.FetchMode = foxhound.FetchAuto

	_, err := smart.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !browserCalled {
		t.Error("expected browser to be called when custom detector reports block")
	}
}

// TestSmartFetcher_ImplementsFetcherInterface ensures compile-time interface
// satisfaction.
func TestSmartFetcher_ImplementsFetcherInterface(t *testing.T) {
	staticF := fetch.NewStealth()
	var _ foxhound.Fetcher = fetch.NewSmart(staticF, nil)
}

// TestSmartFetcher_WithStaticFetcherOption verifies the WithStaticFetcher
// option replaces the static fetcher.
func TestSmartFetcher_WithStaticFetcherOption(t *testing.T) {
	customCalled := false
	customStatic := &mockFetcher{
		fn: func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
			customCalled = true
			return &foxhound.Response{StatusCode: 200, FetchMode: foxhound.FetchStatic, Job: job}, nil
		},
	}

	smart := fetch.NewSmart(fetch.NewStealth(), nil,
		fetch.WithStaticFetcher(customStatic),
	)
	defer smart.Close()

	job := newJob("https://example.com")
	job.FetchMode = foxhound.FetchAuto

	_, err := smart.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !customCalled {
		t.Error("expected custom static fetcher to be used")
	}
}

// TestSmartFetcher_WithBrowserFetcherOption verifies the WithBrowserFetcher
// option replaces the browser fetcher.
func TestSmartFetcher_WithBrowserFetcherOption(t *testing.T) {
	// Static always returns 403 to trigger escalation.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	customBrowserCalled := false
	customBrowser := &mockFetcher{
		fn: func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
			customBrowserCalled = true
			return &foxhound.Response{StatusCode: 200, FetchMode: foxhound.FetchBrowser, Job: job}, nil
		},
	}

	staticF := fetch.NewStealth(fetch.WithTimeout(5 * time.Second))
	smart := fetch.NewSmart(staticF, nil,
		fetch.WithBrowserFetcher(customBrowser),
	)
	defer smart.Close()

	job := newJob(srv.URL)
	job.FetchMode = foxhound.FetchAuto

	_, err := smart.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !customBrowserCalled {
		t.Error("expected custom browser fetcher to be used after escalation")
	}
}

// ---------------------------------------------------------------------------
// ContentBlockDetector tests
// ---------------------------------------------------------------------------

// TestContentBlockDetector_CaptchaPage200_IsBlocked verifies that a 200 OK
// response containing CAPTCHA markers is detected as blocked.
func TestContentBlockDetector_CaptchaPage200_IsBlocked(t *testing.T) {
	d := &fetch.ContentBlockDetector{}

	captchaPages := []struct {
		name string
		body string
	}{
		{"cloudflare-turnstile", `<html><body><div class="cf-turnstile"></div></body></html>`},
		{"recaptcha", `<html><body><div class="g-recaptcha" data-sitekey="abc"></div></body></html>`},
		{"hcaptcha", `<html><body><script src="https://hcaptcha.com/1/api.js"></script></body></html>`},
	}

	for _, tc := range captchaPages {
		resp := &foxhound.Response{StatusCode: 200, Body: []byte(tc.body)}
		if !d.IsBlocked(resp) {
			t.Errorf("%s: expected IsBlocked=true for CAPTCHA page with 200 OK", tc.name)
		}
	}
}

// TestContentBlockDetector_SoftBlock200_IsBlocked verifies that a 200 OK
// response with "access denied" text in a small body is detected as blocked.
func TestContentBlockDetector_SoftBlock200_IsBlocked(t *testing.T) {
	d := &fetch.ContentBlockDetector{}

	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(`<body>Access Denied - please try again later</body>`),
	}
	if !d.IsBlocked(resp) {
		t.Error("expected IsBlocked=true for soft-block 200 page")
	}
}

// TestContentBlockDetector_Normal200_NotBlocked verifies that a normal 200 OK
// response with real content is not flagged as blocked.
func TestContentBlockDetector_Normal200_NotBlocked(t *testing.T) {
	d := &fetch.ContentBlockDetector{}

	resp := &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(`<html><head><title>Products</title></head><body><h1>Welcome to our store</h1><p>Browse our collection of fine products.</p></body></html>`),
	}
	if d.IsBlocked(resp) {
		t.Error("expected IsBlocked=false for normal 200 page")
	}
}

// TestContentBlockDetector_403_StillBlocked verifies that the status-code path
// still works (backward compatible with DefaultBlockDetector).
func TestContentBlockDetector_403_StillBlocked(t *testing.T) {
	d := &fetch.ContentBlockDetector{}

	for _, code := range []int{401, 403, 407, 429, 503} {
		resp := &foxhound.Response{StatusCode: code}
		if !d.IsBlocked(resp) {
			t.Errorf("expected IsBlocked=true for status %d", code)
		}
	}
}

// TestSmartFetcher_EscalatesOnCaptchaPage verifies that SmartFetcher escalates
// to the browser fetcher when the static fetcher returns a 200 page containing
// CAPTCHA markers.
func TestSmartFetcher_EscalatesOnCaptchaPage(t *testing.T) {
	// Static returns 200 but with Cloudflare challenge content.
	captchaBody := []byte(`<html><body>Checking your browser before accessing cloudflare. Just a moment...</body></html>`)
	staticF := &mockFetcher{
		fn: func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
			return &foxhound.Response{
				StatusCode: 200,
				Body:       captchaBody,
				FetchMode:  foxhound.FetchStatic,
				Job:        job,
			}, nil
		},
	}

	browserCalled := false
	browserF := &mockFetcher{
		fn: func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
			browserCalled = true
			return &foxhound.Response{
				StatusCode: 200,
				Body:       []byte(`<html><body>Real content</body></html>`),
				FetchMode:  foxhound.FetchBrowser,
				Job:        job,
			}, nil
		},
	}

	smart := fetch.NewSmart(staticF, browserF)
	defer smart.Close()

	job := newJob("https://example.com")
	job.FetchMode = foxhound.FetchAuto

	resp, err := smart.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !browserCalled {
		t.Error("expected browser fetcher to be called after CAPTCHA detected in static response")
	}
	if resp.FetchMode != foxhound.FetchBrowser {
		t.Errorf("expected FetchBrowser after escalation, got %v", resp.FetchMode)
	}
}

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// mockFetcher is a test double for foxhound.Fetcher.
type mockFetcher struct {
	fn func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error)
}

func (m *mockFetcher) Fetch(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
	return m.fn(ctx, job)
}

func (m *mockFetcher) Close() error { return nil }

// alwaysBlockDetector reports every response as blocked.
type alwaysBlockDetector struct{}

func (a *alwaysBlockDetector) IsBlocked(_ *foxhound.Response) bool { return true }
