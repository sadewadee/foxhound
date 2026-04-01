package behavior

import (
	"math"
	"testing"
	"time"
)

func TestNewMouseReturnsNonNil(t *testing.T) {
	m := NewMouse(DefaultMouseConfig())
	if m == nil {
		t.Fatal("NewMouse returned nil")
	}
}

func TestMoveToPathStartsAtStart(t *testing.T) {
	m := NewMouse(DefaultMouseConfig())
	start := Point{X: 100, Y: 200}
	end := Point{X: 800, Y: 600}

	path := m.MoveTo(start, end)
	if len(path) == 0 {
		t.Fatal("MoveTo returned empty path")
	}

	first := path[0]
	if math.Abs(first.X-start.X) > 5 || math.Abs(first.Y-start.Y) > 5 {
		t.Errorf("path starts at (%v,%v), expected near (%v,%v)", first.X, first.Y, start.X, start.Y)
	}
}

func TestMoveToPathEndsNearEnd(t *testing.T) {
	m := NewMouse(DefaultMouseConfig())
	start := Point{X: 0, Y: 0}
	end := Point{X: 500, Y: 400}

	path := m.MoveTo(start, end)
	if len(path) == 0 {
		t.Fatal("MoveTo returned empty path")
	}

	last := path[len(path)-1]
	// Allow up to OvershootPx + jitter tolerance.
	tolerance := DefaultMouseConfig().OvershootPx + DefaultMouseConfig().Jitter + 5
	dist := math.Sqrt(math.Pow(last.X-end.X, 2) + math.Pow(last.Y-end.Y, 2))
	if dist > tolerance {
		t.Errorf("path ends at (%v,%v), distance %.1f from end (%v,%v) exceeds tolerance %.1f",
			last.X, last.Y, dist, end.X, end.Y, tolerance)
	}
}

func TestMoveToPathHasIntermediatePoints(t *testing.T) {
	m := NewMouse(DefaultMouseConfig())
	start := Point{X: 0, Y: 0}
	end := Point{X: 1000, Y: 800}

	path := m.MoveTo(start, end)
	// Expect at least 10 intermediate points for a bezier curve sampled at ~20-50 pts.
	if len(path) < 10 {
		t.Errorf("expected at least 10 path points, got %d", len(path))
	}
}

func TestMoveToSamePoint(t *testing.T) {
	m := NewMouse(DefaultMouseConfig())
	pt := Point{X: 300, Y: 300}

	// Should not panic for zero-length movement.
	path := m.MoveTo(pt, pt)
	if len(path) == 0 {
		t.Error("MoveTo same point returned empty path")
	}
}

func TestClickOffsetIsSmall(t *testing.T) {
	m := NewMouse(DefaultMouseConfig())
	for i := 0; i < 200; i++ {
		off := m.ClickOffset()
		// Architecture says "random offset 0-5px" — applied per axis, so each
		// component must be in (-5, +5).  Euclidean distance can reach ~7px on
		// the diagonal; we check each component independently instead.
		if math.Abs(off.X) > 5.0 {
			t.Errorf("ClickOffset.X %.2f exceeds ±5px", off.X)
		}
		if math.Abs(off.Y) > 5.0 {
			t.Errorf("ClickOffset.Y %.2f exceeds ±5px", off.Y)
		}
	}
}

func TestClickDurationIsInRange(t *testing.T) {
	m := NewMouse(DefaultMouseConfig())
	for i := 0; i < 100; i++ {
		d := m.ClickDuration()
		if d < 40*time.Millisecond || d > 250*time.Millisecond {
			t.Errorf("ClickDuration %v outside [40ms, 250ms]", d)
		}
	}
}

func TestIdleDriftIsSmall(t *testing.T) {
	m := NewMouse(DefaultMouseConfig())
	for i := 0; i < 200; i++ {
		drift := m.IdleDrift()
		dist := math.Sqrt(drift.X*drift.X + drift.Y*drift.Y)
		if dist > 3 { // 1-2px + headroom
			t.Errorf("IdleDrift (%v,%v) distance %.2f exceeds 2px", drift.X, drift.Y, dist)
		}
	}
}

func TestMoveToPathJitterIsPresent(t *testing.T) {
	cfg := DefaultMouseConfig()
	cfg.Jitter = 3.0
	m := NewMouse(cfg)

	start := Point{X: 0, Y: 0}
	end := Point{X: 200, Y: 0} // horizontal only — jitter must deviate Y

	path := m.MoveTo(start, end)
	maxYDeviation := 0.0
	for _, p := range path {
		if math.Abs(p.Y) > maxYDeviation {
			maxYDeviation = math.Abs(p.Y)
		}
	}
	// With Jitter=3, we expect at least one point to deviate in Y.
	if maxYDeviation == 0 {
		t.Error("no Y jitter observed on a horizontal path — jitter not applied")
	}
}
