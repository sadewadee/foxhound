//go:build playwright

package captcha

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/sadewadee/foxhound/behavior"
)

// pxButtonSelectors lists known PerimeterX press-and-hold button selectors in
// priority order. The first one that exists on the page is used.
var pxButtonSelectors = []string{
	"#px-captcha-wrapper",
	"#px-captcha",
	"div.px-captcha-error-button",
	"div[id^='px-captcha']",
}

// SolvePerimeterX attempts to solve a PerimeterX "Robot or human?" challenge
// by performing a human-realistic press-and-hold gesture on the challenge button.
//
// Behaviour:
//  1. Locates the challenge button using known PX selectors.
//  2. Moves the cursor to the button centre via a Bézier trajectory.
//  3. Holds the mouse button down for a Weibull-sampled duration (0.8–4.0s).
//  4. Releases the mouse button.
//
// Returns nil on success. Returns an error if the button is not found or if
// the playwright interaction fails.
//
// IMPORTANT: This function runs BEFORE any NopeCHA or token-based solver. It
// does NOT modify the NopeCHA extension or its configuration in any way.
func SolvePerimeterX(ctx context.Context, page playwright.Page) error {
	slog.Info("behavior: PressAndHold initiated", "challenge", "perimeter_x")

	// Find the challenge button.
	var loc playwright.Locator
	var foundSelector string
	for _, sel := range pxButtonSelectors {
		candidate := page.Locator(sel)
		count, err := candidate.Count()
		if err == nil && count > 0 {
			loc = candidate
			foundSelector = sel
			break
		}
	}
	if loc == nil {
		return fmt.Errorf("perimeter_x: press-hold button not found (tried %v)", pxButtonSelectors)
	}

	slog.Debug("behavior: PressAndHold found button", "selector", foundSelector)

	// Get the bounding box of the button for pointer positioning.
	box, err := loc.BoundingBox()
	if err != nil || box == nil {
		return fmt.Errorf("perimeter_x: could not get button bounding box: %w", err)
	}

	// Target: centre of the button.
	targetX := box.X + box.Width/2
	targetY := box.Y + box.Height/2

	// Current cursor position — assume centre of viewport as a safe default.
	viewportSize := page.ViewportSize()
	startX := float64(viewportSize.Width) / 2
	startY := float64(viewportSize.Height) / 2

	// Generate a human-like mouse approach trajectory.
	approach := behavior.PressHoldApproach(
		behavior.Point{X: startX, Y: startY},
		behavior.Point{X: targetX, Y: targetY},
	)

	mouse := page.Mouse()

	// Replay the Bézier trajectory.
	for _, pt := range approach {
		if err := mouse.Move(pt.X, pt.Y); err != nil {
			return fmt.Errorf("perimeter_x: mouse move error: %w", err)
		}
	}

	// Sample hold duration from human-realistic distribution.
	holdDur := behavior.PressHoldDuration()
	slog.Debug("behavior: PressAndHold holding",
		"selector", foundSelector,
		"hold_ms", holdDur.Milliseconds(),
	)

	// Press the button.
	if err := mouse.Down(); err != nil {
		return fmt.Errorf("perimeter_x: mouse down error: %w", err)
	}

	// Hold for the sampled duration, respecting context cancellation.
	select {
	case <-ctx.Done():
		_ = mouse.Up()
		return fmt.Errorf("perimeter_x: context cancelled during hold: %w", ctx.Err())
	case <-time.After(holdDur):
	}

	// Release.
	if err := mouse.Up(); err != nil {
		return fmt.Errorf("perimeter_x: mouse up error: %w", err)
	}

	slog.Info("behavior: PressAndHold complete", "challenge", "perimeter_x", "held_ms", holdDur.Milliseconds())
	return nil
}
