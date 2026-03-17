package captcha

import (
	"context"
	"fmt"
	"log/slog"
)

// TurnstileHandler is a specialised handler for Cloudflare Turnstile challenges.
// Turnstile is often invisible/behavioural so it delegates directly to the
// configured fallback solver (CapSolver or 2captcha).
type TurnstileHandler struct {
	fallback Solver
}

// NewTurnstile creates a TurnstileHandler that uses fallback to solve challenges
// that cannot be bypassed behaviourally.
func NewTurnstile(fallback Solver) *TurnstileHandler {
	return &TurnstileHandler{fallback: fallback}
}

// Handle attempts to solve a Turnstile challenge.
// It returns the cf-turnstile-response token on success.
func (t *TurnstileHandler) Handle(ctx context.Context, challenge *DetectResult) (*Solution, error) {
	slog.Info("captcha: solving Cloudflare Turnstile via fallback solver",
		"page_url", challenge.PageURL,
		"site_key", challenge.SiteKey,
	)

	sol, err := t.fallback.Solve(ctx, challenge)
	if err != nil {
		return nil, fmt.Errorf("turnstile: fallback solver failed: %w", err)
	}

	slog.Info("captcha: Turnstile solved", "page_url", challenge.PageURL)
	return sol, nil
}
