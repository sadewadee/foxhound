package behavior

import (
	"testing"
	"time"
)

// TestPressHoldDuration verifies that the sampled hold duration stays within
// the documented human-realistic bounds [0.8s, 4.0s].
func TestPressHoldDuration(t *testing.T) {
	const samples = 10000
	lo := 0.8 * float64(time.Second)
	hi := 4.0 * float64(time.Second)

	for i := 0; i < samples; i++ {
		d := PressHoldDuration()
		if float64(d) < lo || float64(d) > hi {
			t.Fatalf("sample %d: PressHoldDuration()=%s outside [0.8s, 4.0s]", i, d)
		}
	}
}

// TestPressHoldDuration_Distribution verifies the Weibull(k=2, λ=1.5) sample
// produces a mean in a plausible range.  We only assert a loose bound to avoid
// flakiness from random variation over 10k samples.
func TestPressHoldDuration_Distribution(t *testing.T) {
	const samples = 10000
	var total float64
	for i := 0; i < samples; i++ {
		total += PressHoldDuration().Seconds()
	}
	mean := total / float64(samples)
	// Theoretical unclamped mean ≈ Γ(1+1/k)*λ = Γ(1.5)*1.5 ≈ 0.886*1.5 ≈ 1.33s.
	// With clamping at [0.8, 4.0] the empirical mean should land comfortably in [1.0, 2.0].
	if mean < 1.0 || mean > 2.0 {
		t.Errorf("mean hold duration %0.3fs outside expected range [1.0s, 2.0s]", mean)
	}
}

// TestPressHoldApproach verifies that the generated trajectory is non-empty and
// that the final point is near the requested end point (within mouse jitter).
func TestPressHoldApproach(t *testing.T) {
	start := Point{X: 960, Y: 540}
	end := Point{X: 300, Y: 400}

	pts := PressHoldApproach(start, end)
	if len(pts) == 0 {
		t.Fatal("PressHoldApproach returned empty trajectory")
	}

	// Must have at least 2 points (start + end minimum).
	if len(pts) < 2 {
		t.Fatalf("expected at least 2 waypoints, got %d", len(pts))
	}

	// Last point should be close to end (within 10px — Mouse.MoveTo micro-jitter is ≤2px
	// plus optional overshoot correction of ≤3px, giving max 5px per axis).
	last := pts[len(pts)-1]
	dx := last.X - end.X
	dy := last.Y - end.Y
	if dx < -10 || dx > 10 || dy < -10 || dy > 10 {
		t.Errorf("last waypoint (%0.1f,%0.1f) too far from target (%0.1f,%0.1f)",
			last.X, last.Y, end.X, end.Y)
	}
}

// TestPressHoldApproach_ZeroLength verifies that a zero-length move returns
// at least one point without panicking.
func TestPressHoldApproach_ZeroLength(t *testing.T) {
	pt := Point{X: 100, Y: 200}
	pts := PressHoldApproach(pt, pt)
	if len(pts) == 0 {
		t.Fatal("expected at least one point for zero-length move")
	}
}
