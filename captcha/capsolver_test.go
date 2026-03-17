package captcha_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/foxhound-scraper/foxhound/captcha"
)

// capsolverServer sets up a mock CapSolver API server.
// createTaskResp is the JSON body for the createTask endpoint.
// getTaskResp is the JSON body for the getTaskResult endpoint.
func capsolverServer(t *testing.T, createTaskResp, getTaskResp string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "createTask") {
			w.Write([]byte(createTaskResp))
			return
		}
		if strings.Contains(r.URL.Path, "getTaskResult") {
			w.Write([]byte(getTaskResp))
			return
		}
		http.NotFound(w, r)
	}))
}

func TestCapSolverSolvesTurnstile(t *testing.T) {
	createResp := `{"errorId":0,"taskId":"task-uuid-001"}`
	getResp := `{"errorId":0,"status":"ready","solution":{"token":"turnstile-token-abc"}}`

	srv := capsolverServer(t, createResp, getResp)
	defer srv.Close()

	solver := captcha.NewCapSolverWithEndpoint("test-api-key", srv.URL)
	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaCloudflare,
		SiteKey: "0x4AAAAAAATest",
		PageURL: "https://example.com",
	}

	sol, err := solver.Solve(context.Background(), challenge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sol.Token != "turnstile-token-abc" {
		t.Errorf("expected token %q, got %q", "turnstile-token-abc", sol.Token)
	}
	if sol.Type != captcha.CaptchaCloudflare {
		t.Errorf("expected type %q, got %q", captcha.CaptchaCloudflare, sol.Type)
	}
}

func TestCapSolverSolvesRecaptcha(t *testing.T) {
	createResp := `{"errorId":0,"taskId":"task-uuid-002"}`
	getResp := `{"errorId":0,"status":"ready","solution":{"gRecaptchaResponse":"recaptcha-token-xyz"}}`

	srv := capsolverServer(t, createResp, getResp)
	defer srv.Close()

	solver := captcha.NewCapSolverWithEndpoint("test-api-key", srv.URL)
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
		t.Error("expected non-empty token for recaptcha solve")
	}
}

func TestCapSolverSolvesHCaptcha(t *testing.T) {
	createResp := `{"errorId":0,"taskId":"task-uuid-003"}`
	getResp := `{"errorId":0,"status":"ready","solution":{"gRecaptchaResponse":"hcaptcha-token-def"}}`

	srv := capsolverServer(t, createResp, getResp)
	defer srv.Close()

	solver := captcha.NewCapSolverWithEndpoint("test-api-key", srv.URL)
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
		t.Error("expected non-empty token for hcaptcha solve")
	}
}

func TestCapSolverCreateTaskSendsCorrectPayload(t *testing.T) {
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "createTask") {
			json.NewDecoder(r.Body).Decode(&capturedBody)
			w.Write([]byte(`{"errorId":0,"taskId":"task-payload-test"}`))
			return
		}
		// getTaskResult
		w.Write([]byte(`{"errorId":0,"status":"ready","solution":{"token":"tok"}}`))
	}))
	defer srv.Close()

	solver := captcha.NewCapSolverWithEndpoint("my-api-key", srv.URL)
	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaCloudflare,
		SiteKey: "0x4AAAAAAATest",
		PageURL: "https://test.com",
	}
	solver.Solve(context.Background(), challenge)

	if capturedBody["clientKey"] != "my-api-key" {
		t.Errorf("expected clientKey %q, got %v", "my-api-key", capturedBody["clientKey"])
	}
	task, _ := capturedBody["task"].(map[string]any)
	if task == nil {
		t.Fatal("expected task object in request body")
	}
	if task["websiteURL"] != "https://test.com" {
		t.Errorf("expected websiteURL %q, got %v", "https://test.com", task["websiteURL"])
	}
}

func TestCapSolverBalanceReturnsFloat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"errorId":0,"balance":12.50}`))
	}))
	defer srv.Close()

	solver := captcha.NewCapSolverWithEndpoint("test-api-key", srv.URL)
	bal, err := solver.Balance(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bal != 12.50 {
		t.Errorf("expected balance 12.50, got %v", bal)
	}
}

func TestCapSolverAPIErrorReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "createTask") {
			w.Write([]byte(`{"errorId":1,"errorCode":"ERROR_KEY_DENIED_ACCESS","errorDescription":"Invalid API key"}`))
			return
		}
	}))
	defer srv.Close()

	solver := captcha.NewCapSolverWithEndpoint("bad-key", srv.URL)
	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaCloudflare,
		SiteKey: "0x4AAAAAAATest",
		PageURL: "https://example.com",
	}
	_, err := solver.Solve(context.Background(), challenge)
	if err == nil {
		t.Fatal("expected error for API key denial, got nil")
	}
}

func TestCapSolverContextCancelledReturnsError(t *testing.T) {
	// Server that always returns "processing" so Solve loops until cancelled.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "createTask") {
			w.Write([]byte(`{"errorId":0,"taskId":"task-loop"}`))
			return
		}
		w.Write([]byte(`{"errorId":0,"status":"processing"}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	solver := captcha.NewCapSolverWithEndpoint("test-api-key", srv.URL)
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
