package captcha_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sadewadee/foxhound/captcha"
)

// twocaptchaServer builds a mock 2captcha API server.
// inResp is returned for the /in.php endpoint, resResp for /res.php.
func twocaptchaServer(t *testing.T, inResp, resResp string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/in.php") {
			w.Write([]byte(inResp))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/res.php") {
			w.Write([]byte(resResp))
			return
		}
		http.NotFound(w, r)
	}))
}

func TestTwoCaptchaSolvesRecaptcha(t *testing.T) {
	srv := twocaptchaServer(t, "OK|captcha-id-001", "OK|solved-recaptcha-token")
	defer srv.Close()

	solver := captcha.NewTwoCaptchaWithEndpoint("test-api-key", srv.URL)
	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaRecaptcha,
		SiteKey: "6LcRecaptchaKey",
		PageURL: "https://example.com",
	}

	sol, err := solver.Solve(context.Background(), challenge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sol.Token != "solved-recaptcha-token" {
		t.Errorf("expected token %q, got %q", "solved-recaptcha-token", sol.Token)
	}
	if sol.Type != captcha.CaptchaRecaptcha {
		t.Errorf("expected type %q, got %q", captcha.CaptchaRecaptcha, sol.Type)
	}
}

func TestTwoCaptchaSolvesHCaptcha(t *testing.T) {
	srv := twocaptchaServer(t, "OK|captcha-id-002", "OK|hcaptcha-response-token")
	defer srv.Close()

	solver := captcha.NewTwoCaptchaWithEndpoint("test-api-key", srv.URL)
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

func TestTwoCaptchaSolvesTurnstile(t *testing.T) {
	srv := twocaptchaServer(t, "OK|captcha-id-003", "OK|turnstile-response-token")
	defer srv.Close()

	solver := captcha.NewTwoCaptchaWithEndpoint("test-api-key", srv.URL)
	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaCloudflare,
		SiteKey: "0x4AAAAAAATest",
		PageURL: "https://example.com",
	}

	sol, err := solver.Solve(context.Background(), challenge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sol.Token == "" {
		t.Error("expected non-empty token for turnstile solve")
	}
}

func TestTwoCaptchaSubmitRequestIncludesRequiredFields(t *testing.T) {
	var capturedQuery url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/in.php") {
			capturedQuery = r.URL.Query()
			w.Write([]byte("OK|captcha-id-fields"))
			return
		}
		w.Write([]byte("OK|result-token"))
	}))
	defer srv.Close()

	solver := captcha.NewTwoCaptchaWithEndpoint("my-secret-key", srv.URL)
	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaRecaptcha,
		SiteKey: "6LcFieldTestKey",
		PageURL: "https://fieldtest.com",
	}
	solver.Solve(context.Background(), challenge)

	if capturedQuery.Get("key") != "my-secret-key" {
		t.Errorf("expected key %q in request, got %q", "my-secret-key", capturedQuery.Get("key"))
	}
	if capturedQuery.Get("googlekey") != "6LcFieldTestKey" {
		t.Errorf("expected googlekey %q, got %q", "6LcFieldTestKey", capturedQuery.Get("googlekey"))
	}
	if capturedQuery.Get("pageurl") != "https://fieldtest.com" {
		t.Errorf("expected pageurl %q, got %q", "https://fieldtest.com", capturedQuery.Get("pageurl"))
	}
}

func TestTwoCaptchaBalanceReturnsFloat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK|7.35"))
	}))
	defer srv.Close()

	solver := captcha.NewTwoCaptchaWithEndpoint("test-api-key", srv.URL)
	bal, err := solver.Balance(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bal != 7.35 {
		t.Errorf("expected balance 7.35, got %v", bal)
	}
}

func TestTwoCaptchaErrorResponseReturnsError(t *testing.T) {
	srv := twocaptchaServer(t, "ERROR_WRONG_USER_KEY", "")
	defer srv.Close()

	solver := captcha.NewTwoCaptchaWithEndpoint("bad-key", srv.URL)
	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaRecaptcha,
		SiteKey: "6LcTest",
		PageURL: "https://example.com",
	}
	_, err := solver.Solve(context.Background(), challenge)
	if err == nil {
		t.Fatal("expected error for wrong user key, got nil")
	}
}

func TestTwoCaptchaContextCancelledReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/in.php") {
			w.Write([]byte("OK|captcha-id-loop"))
			return
		}
		// Always return CAPCHA_NOT_READY to force looping.
		w.Write([]byte("CAPCHA_NOT_READY"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	solver := captcha.NewTwoCaptchaWithEndpoint("test-api-key", srv.URL)
	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaRecaptcha,
		SiteKey: "6LcLoopKey",
		PageURL: "https://example.com",
	}
	_, err := solver.Solve(ctx, challenge)
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}
