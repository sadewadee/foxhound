package captcha_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		// GET = poll for result
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			w.Write([]byte(`{"data":"job-loop"}`))
			return
		}
		// 409 = incomplete job, keeps polling
		w.WriteHeader(http.StatusConflict)
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
