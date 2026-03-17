package captcha_test

import (
	"context"
	"errors"
	"testing"

	"github.com/foxhound-scraper/foxhound/captcha"
)

// mockSolver is a controllable Solver for testing TurnstileHandler.
type mockSolver struct {
	token   string
	err     error
	balance float64
	called  bool
}

func (m *mockSolver) Solve(_ context.Context, challenge *captcha.DetectResult) (*captcha.Solution, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}
	return &captcha.Solution{Token: m.token, Type: challenge.Type}, nil
}

func (m *mockSolver) Balance(_ context.Context) (float64, error) {
	return m.balance, nil
}

func TestTurnstileHandlerReturnsSolution(t *testing.T) {
	fallback := &mockSolver{token: "cf-turnstile-response-abc"}
	handler := captcha.NewTurnstile(fallback)

	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaCloudflare,
		SiteKey: "0x4AAAAAAATest",
		PageURL: "https://example.com",
	}

	sol, err := handler.Handle(context.Background(), challenge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sol.Token != "cf-turnstile-response-abc" {
		t.Errorf("expected token %q, got %q", "cf-turnstile-response-abc", sol.Token)
	}
}

func TestTurnstileHandlerUseFallbackSolver(t *testing.T) {
	fallback := &mockSolver{token: "fallback-token-xyz"}
	handler := captcha.NewTurnstile(fallback)

	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaCloudflare,
		SiteKey: "0x4AAAAAAATest",
		PageURL: "https://example.com",
	}

	sol, err := handler.Handle(context.Background(), challenge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fallback.called {
		t.Error("expected fallback solver to be called")
	}
	if sol.Token == "" {
		t.Error("expected non-empty token from fallback")
	}
}

func TestTurnstileHandlerPropagatesFallbackError(t *testing.T) {
	fallback := &mockSolver{err: errors.New("quota exceeded")}
	handler := captcha.NewTurnstile(fallback)

	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaCloudflare,
		SiteKey: "0x4AAAAAAATest",
		PageURL: "https://example.com",
	}

	_, err := handler.Handle(context.Background(), challenge)
	if err == nil {
		t.Fatal("expected error when fallback fails")
	}
	if !errors.Is(err, fallback.err) && !containsMsg(err.Error(), "quota exceeded") {
		t.Errorf("expected error to contain fallback error, got %v", err)
	}
}

func TestTurnstileHandlerSolutionTypeIsTurnstile(t *testing.T) {
	fallback := &mockSolver{token: "any-token"}
	handler := captcha.NewTurnstile(fallback)

	challenge := &captcha.DetectResult{
		Type:    captcha.CaptchaCloudflare,
		SiteKey: "0x4AAAAAAATest",
		PageURL: "https://example.com",
	}

	sol, err := handler.Handle(context.Background(), challenge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sol.Type != captcha.CaptchaCloudflare {
		t.Errorf("expected solution type %q, got %q", captcha.CaptchaCloudflare, sol.Type)
	}
}

func containsMsg(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
