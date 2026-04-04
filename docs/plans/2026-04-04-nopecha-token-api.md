# NopeCHA Token API Solver — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add NopeCHA Token API as a captcha solver provider, with automatic fallback to the NopeCHA browser addon when the API key is absent or invalid.

**Architecture:** New `NopeCHA` struct in `captcha/` implements the existing `Solver` interface (same pattern as CapSolver and TwoCaptcha). Token API: submit sitekey+URL → poll job ID → get token. When `captcha.provider: "nopecha"` is configured with a valid API key, the API solver is used and the browser addon is NOT loaded. When the key is absent/invalid, the addon is loaded as fallback. API is default OFF.

**Tech Stack:** Go standard library (`net/http`, `encoding/json`), NopeCHA Token API (`https://api.nopecha.com/token/`), existing `captcha.Solver` interface.

---

### Task 1: NopeCHA Solver — Tests

**Files:**
- Create: `captcha/nopecha_test.go`

- [ ] **Step 1: Write test file with mock server and 7 test cases**

Follow the exact pattern from `captcha/capsolver_test.go`. Mock server handles `/token/` POST (submit) and `/token/` GET (poll).

```go
package captcha_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sadewadee/foxhound/captcha"
)

// nopechaServer creates a mock NopeCHA Token API server.
// submitResp is returned for POST /token/ (job submission).
// pollResp is returned for GET /token/ (result polling).
func nopechaServer(t *testing.T, submitResp, pollResp string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			w.Write([]byte(submitResp))
			return
		}
		// GET = poll
		w.Write([]byte(pollResp))
	}))
}

func TestNopeCHASolvesTurnstile(t *testing.T) {
	submitResp := `{"data":"job-001"}`
	pollResp := `{"data":"turnstile-token-xyz"}`
	srv := nopechaServer(t, submitResp, pollResp)
	defer srv.Close()

	solver := captcha.NewNopeCHAWithEndpoint("test-key", srv.URL)
	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaCloudflare,
		SiteKey: "0x4AAAAAAATest",
		PageURL: "https://example.com",
	}
	sol, err := solver.Solve(context.Background(), challenge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sol.Token != "turnstile-token-xyz" {
		t.Errorf("expected token %q, got %q", "turnstile-token-xyz", sol.Token)
	}
	if sol.Type != captcha.CaptchaCloudflare {
		t.Errorf("expected type %q, got %q", captcha.CaptchaCloudflare, sol.Type)
	}
}

func TestNopeCHASolvesRecaptcha(t *testing.T) {
	submitResp := `{"data":"job-002"}`
	pollResp := `{"data":"recaptcha-token-abc"}`
	srv := nopechaServer(t, submitResp, pollResp)
	defer srv.Close()

	solver := captcha.NewNopeCHAWithEndpoint("test-key", srv.URL)
	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaRecaptcha,
		SiteKey: "6LcRecaptchaKey",
		PageURL: "https://example.com/verify",
	}
	sol, err := solver.Solve(context.Background(), challenge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sol.Token == "" {
		t.Error("expected non-empty token")
	}
}

func TestNopeCHASolvesHCaptcha(t *testing.T) {
	submitResp := `{"data":"job-003"}`
	pollResp := `{"data":"hcaptcha-token-def"}`
	srv := nopechaServer(t, submitResp, pollResp)
	defer srv.Close()

	solver := captcha.NewNopeCHAWithEndpoint("test-key", srv.URL)
	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaHCaptcha,
		SiteKey: "hcap-key-abc",
		PageURL: "https://example.com",
	}
	sol, err := solver.Solve(context.Background(), challenge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sol.Token == "" {
		t.Error("expected non-empty token")
	}
}

func TestNopeCHASubmitSendsCorrectPayload(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&capturedBody)
			w.Write([]byte(`{"data":"job-payload"}`))
			return
		}
		w.Write([]byte(`{"data":"tok"}`))
	}))
	defer srv.Close()

	solver := captcha.NewNopeCHAWithEndpoint("my-api-key", srv.URL)
	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaCloudflare,
		SiteKey: "0x4AAAAAAATest",
		PageURL: "https://test.com",
	}
	solver.Solve(context.Background(), challenge)

	if capturedBody["key"] != "my-api-key" {
		t.Errorf("expected key %q, got %v", "my-api-key", capturedBody["key"])
	}
	if capturedBody["type"] != "turnstile" {
		t.Errorf("expected type %q, got %v", "turnstile", capturedBody["type"])
	}
	if capturedBody["sitekey"] != "0x4AAAAAAATest" {
		t.Errorf("expected sitekey %q, got %v", "0x4AAAAAAATest", capturedBody["sitekey"])
	}
	if capturedBody["url"] != "https://test.com" {
		t.Errorf("expected url %q, got %v", "https://test.com", capturedBody["url"])
	}
}

func TestNopeCHABalanceReturnsCredits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"plan":"Basic","credit":15000,"quota":20000}`))
	}))
	defer srv.Close()

	solver := captcha.NewNopeCHAWithEndpoint("test-key", srv.URL)
	bal, err := solver.Balance(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bal != 15000 {
		t.Errorf("expected balance 15000, got %v", bal)
	}
}

func TestNopeCHAAPIErrorReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":401,"message":"Invalid API Key"}`))
	}))
	defer srv.Close()

	solver := captcha.NewNopeCHAWithEndpoint("bad-key", srv.URL)
	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaCloudflare,
		SiteKey: "0x4AAAAAAATest",
		PageURL: "https://example.com",
	}
	_, err := solver.Solve(context.Background(), challenge)
	if err == nil {
		t.Fatal("expected error for invalid API key, got nil")
	}
}

func TestNopeCHAContextCancelledReturnsError(t *testing.T) {
	// Server always returns "incomplete" so Solve polls until cancelled.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			w.Write([]byte(`{"data":"job-loop"}`))
			return
		}
		w.WriteHeader(http.StatusConflict) // 409 = incomplete
		w.Write([]byte(`{"error":14,"message":"Incomplete job"}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	solver := captcha.NewNopeCHAWithEndpoint("test-key", srv.URL)
	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaCloudflare,
		SiteKey: "0x4AAAAAAATest",
		PageURL: "https://example.com",
	}
	_, err := solver.Solve(ctx, challenge)
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestNopeCHA -v ./captcha/...`
Expected: FAIL — `NewNopeCHAWithEndpoint` undefined.

---

### Task 2: NopeCHA Solver — Implementation

**Files:**
- Create: `captcha/nopecha.go`

- [ ] **Step 1: Implement NopeCHA solver**

```go
package captcha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

const (
	nopechaDefaultEndpoint = "https://api.nopecha.com"
	nopechaPollInterval    = 3 * time.Second
)

// NopeCHA implements Solver using the NopeCHA Token API.
type NopeCHA struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

// NewNopeCHA creates a NopeCHA solver that calls the production API.
func NewNopeCHA(apiKey string) *NopeCHA {
	return &NopeCHA{
		apiKey:   apiKey,
		endpoint: nopechaDefaultEndpoint,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// NewNopeCHAWithEndpoint creates a NopeCHA solver with a custom base URL (for tests).
func NewNopeCHAWithEndpoint(apiKey, endpoint string) *NopeCHA {
	return &NopeCHA{
		apiKey:   apiKey,
		endpoint: endpoint,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// nopechaTokenType maps CaptchaType to NopeCHA Token API type strings.
func nopechaTokenType(ct CaptchaType) string {
	switch ct {
	case CaptchaCloudflare:
		return "turnstile"
	case CaptchaRecaptcha:
		return "recaptcha2"
	case CaptchaHCaptcha:
		return "hcaptcha"
	default:
		return "recaptcha2"
	}
}

// nopechaSubmitRequest is the JSON body for submitting a token job.
type nopechaSubmitRequest struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	SiteKey string `json:"sitekey"`
	URL     string `json:"url"`
}

// nopechaResponse is the generic NopeCHA API response.
type nopechaResponse struct {
	Data    any    `json:"data"`    // string (job ID or token)
	Error   int    `json:"error"`   // error code (0 = none)
	Message string `json:"message"` // error message
}

// nopechaStatusResponse is the response from /v1/status.
type nopechaStatusResponse struct {
	Plan   string  `json:"plan"`
	Credit float64 `json:"credit"`
	Quota  float64 `json:"quota"`
}

// Solve submits a token challenge and polls until resolved.
func (s *NopeCHA) Solve(ctx context.Context, challenge *DetectResult) (*Solution, error) {
	jobID, err := s.submit(ctx, challenge)
	if err != nil {
		return nil, fmt.Errorf("nopecha: submit: %w", err)
	}
	slog.Debug("nopecha: job submitted", "job_id", jobID, "type", challenge.Type)

	token, err := s.poll(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("nopecha: poll: %w", err)
	}
	return &Solution{Token: token, Type: challenge.Type}, nil
}

// submit posts a token job and returns the job ID.
func (s *NopeCHA) submit(ctx context.Context, challenge *DetectResult) (string, error) {
	body := nopechaSubmitRequest{
		Key:     s.apiKey,
		Type:    nopechaTokenType(challenge.Type),
		SiteKey: challenge.SiteKey,
		URL:     challenge.PageURL,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.endpoint+"/token/", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result nopechaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}
	if result.Error != 0 {
		return "", fmt.Errorf("API error %d: %s", result.Error, result.Message)
	}
	jobID, ok := result.Data.(string)
	if !ok || jobID == "" {
		return "", fmt.Errorf("unexpected data field: %v", result.Data)
	}
	return jobID, nil
}

// poll queries for the job result until complete or context is cancelled.
func (s *NopeCHA) poll(ctx context.Context, jobID string) (string, error) {
	params := url.Values{}
	params.Set("key", s.apiKey)
	params.Set("id", jobID)
	pollURL := s.endpoint + "/token/?" + params.Encode()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			return "", err
		}

		resp, err := s.client.Do(req)
		if err != nil {
			return "", err
		}
		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", err
		}

		var result nopechaResponse
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return "", fmt.Errorf("decoding poll response: %w", err)
		}

		// 409 with error 14 = "Incomplete job" — keep polling.
		if resp.StatusCode == http.StatusConflict && result.Error == 14 {
			slog.Debug("nopecha: waiting for solution", "job_id", jobID)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(nopechaPollInterval):
			}
			continue
		}

		if result.Error != 0 {
			return "", fmt.Errorf("API error %d: %s", result.Error, result.Message)
		}

		token, ok := result.Data.(string)
		if !ok || token == "" {
			return "", fmt.Errorf("unexpected token data: %v", result.Data)
		}
		return token, nil
	}
}

// Balance returns the remaining credits from the NopeCHA account.
func (s *NopeCHA) Balance(ctx context.Context) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.endpoint+"/v1/status", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Basic "+s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("nopecha: status request: %w", err)
	}
	defer resp.Body.Close()

	var result nopechaStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("nopecha: decoding status: %w", err)
	}
	return result.Credit, nil
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test -run TestNopeCHA -v ./captcha/...`
Expected: all 7 tests PASS.

- [ ] **Step 3: Run with race detector**

Run: `go test -race -run TestNopeCHA ./captcha/...`
Expected: PASS, no races.

- [ ] **Step 4: Commit**

```bash
git add captcha/nopecha.go captcha/nopecha_test.go
git commit -m "feat(captcha): add NopeCHA Token API solver"
```

---

### Task 3: Wire NopeCHA into Config + CLI

**Files:**
- Modify: `config.go:171-176` (CaptchaConfig)
- Modify: `cmd/foxhound/run.go:598-611` (solver instantiation)
- Modify: `cmd/foxhound/init.go` (env var scaffold)

- [ ] **Step 1: Update CaptchaConfig provider comment**

In `config.go:174`, update the comment to include nopecha:

```go
Provider string `yaml:"provider"` // "capsolver" | "twocaptcha" | "nopecha"
```

- [ ] **Step 2: Add nopecha case in `cmd/foxhound/run.go:598-611`**

After the `"twocaptcha", "2captcha"` case, add:

```go
case "nopecha":
    captchaSolver = captcha.NewNopeCHA(cfg.Captcha.APIKey)
    slog.Info("captcha: nopecha token API enabled")
```

- [ ] **Step 3: Add env var scaffold in `cmd/foxhound/init.go`**

Find the existing `# CAPSOLVER_API_KEY=` line and add `# NOPECHA_API_KEY=` after `# TWOCAPTCHA_API_KEY=`.

- [ ] **Step 4: Build and verify**

Run: `go build ./cmd/foxhound/...`
Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add config.go cmd/foxhound/run.go cmd/foxhound/init.go
git commit -m "feat(config): wire NopeCHA provider into captcha config and CLI"
```

---

### Task 4: Conditional Addon Loading — Skip Addon When NopeCHA API Active

**Files:**
- Modify: `fetch/camoufox_playwright.go:367-416` (addon loading block)

The addon loading logic at line 370 currently auto-downloads NopeCHA when `extensionPath` is empty. We need to add a new option `WithSkipExtension()` that the CLI sets when NopeCHA API solver is active.

- [ ] **Step 1: Add `skipExtension` field and option**

Around line 203 (in the CamoufoxFetcher struct), add:
```go
skipExtension bool // skip auto-loading NopeCHA addon (API solver active)
```

Add option function after `WithExtensionPath`:
```go
// WithSkipExtension prevents the NopeCHA addon from being auto-loaded.
// Use when the NopeCHA Token API solver is active — the API and addon
// should not run simultaneously.
func WithSkipExtension() CamoufoxOption {
    return func(f *CamoufoxFetcher) {
        f.skipExtension = true
    }
}
```

- [ ] **Step 2: Guard the addon loading block**

In `NewCamoufox()` at line 370, change the condition from:
```go
if f.extensionPath == "" {
```
to:
```go
if f.extensionPath == "" && !f.skipExtension {
```

And at line 385, also add the guard so that if `skipExtension` is true and no custom extension is set, we skip entirely:
```go
if f.skipExtension && f.extensionPath == "" {
    f.extensionPath = "none"
    slog.Info("fetch/camoufox: NopeCHA addon skipped — API solver active")
}
```

Insert this block BEFORE the existing `if f.extensionPath == ""` check (between line 367 and 370).

- [ ] **Step 3: Wire in `cmd/foxhound/run.go`**

In the nopecha case block (from Task 3), after creating the solver, when building the CamoufoxFetcher options, pass `fetch.WithSkipExtension()`:

Find where `WithBrowserProxy` and other CamoufoxOptions are assembled and add `fetch.WithSkipExtension()` when `cfg.Captcha.Provider == "nopecha" && cfg.Captcha.APIKey != ""`.

- [ ] **Step 4: Build and verify**

Run: `go build -tags playwright ./...` (excluding examples)
Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add fetch/camoufox_playwright.go cmd/foxhound/run.go
git commit -m "feat(fetch): skip NopeCHA addon when API solver is active"
```

---

### Task 5: Full Integration Verification

- [ ] **Step 1: Run all captcha tests**

Run: `go test -v ./captcha/...`
Expected: all tests PASS (existing CapSolver, TwoCaptcha, Turnstile, detect + new NopeCHA).

- [ ] **Step 2: Run all fetch tests (playwright)**

Run: `go test -tags playwright ./fetch/...`
Expected: PASS.

- [ ] **Step 3: Run full test suite**

Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 4: Run race detector on captcha package**

Run: `go test -race ./captcha/...`
Expected: PASS, no races.

- [ ] **Step 5: Build full project**

Run: `go build ./... && go build -tags playwright ./fetch/... ./cmd/...`
Expected: clean build.
