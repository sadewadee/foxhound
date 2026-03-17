package behavior

import (
	"testing"
	"time"
)

func TestNewScrollReturnsNonNil(t *testing.T) {
	s := NewScroll(DefaultScrollConfig())
	if s == nil {
		t.Fatal("NewScroll returned nil")
	}
}

func TestScrollGestureReadingModeDistanceInRange(t *testing.T) {
	s := NewScroll(DefaultScrollConfig())
	cfg := DefaultScrollConfig()
	for i := 0; i < 200; i++ {
		dist, _, _ := s.ScrollGesture(ScrollReading)
		if dist < cfg.ReadMinPx || dist > cfg.ReadMaxPx {
			t.Errorf("reading distance %d outside [%d, %d]", dist, cfg.ReadMinPx, cfg.ReadMaxPx)
		}
	}
}

func TestScrollGestureScanModeDistanceInRange(t *testing.T) {
	s := NewScroll(DefaultScrollConfig())
	cfg := DefaultScrollConfig()
	for i := 0; i < 200; i++ {
		dist, _, _ := s.ScrollGesture(ScrollScan)
		if dist < cfg.ScanMinPx || dist > cfg.ScanMaxPx {
			t.Errorf("scan distance %d outside [%d, %d]", dist, cfg.ScanMinPx, cfg.ScanMaxPx)
		}
	}
}

func TestScrollGestureReadingPauseIsPositive(t *testing.T) {
	s := NewScroll(DefaultScrollConfig())
	for i := 0; i < 100; i++ {
		_, pause, _ := s.ScrollGesture(ScrollReading)
		if pause <= 0 {
			t.Errorf("reading pause %v must be positive", pause)
		}
	}
}

func TestScrollGestureScanPauseIsPositive(t *testing.T) {
	s := NewScroll(DefaultScrollConfig())
	for i := 0; i < 100; i++ {
		_, pause, _ := s.ScrollGesture(ScrollScan)
		if pause <= 0 {
			t.Errorf("scan pause %v must be positive", pause)
		}
	}
}

// TestScrollGestureReadingPauseRange verifies pauses stay in the documented
// 1-5s window for reading mode.
func TestScrollGestureReadingPauseRange(t *testing.T) {
	s := NewScroll(DefaultScrollConfig())
	for i := 0; i < 200; i++ {
		_, pause, _ := s.ScrollGesture(ScrollReading)
		if pause < 1*time.Second || pause > 5*time.Second {
			t.Errorf("reading pause %v outside [1s, 5s]", pause)
		}
	}
}

// TestScrollGestureScanPauseRange verifies pauses stay in the documented
// 0.3-1s window for scan mode.
func TestScrollGestureScanPauseRange(t *testing.T) {
	s := NewScroll(DefaultScrollConfig())
	for i := 0; i < 200; i++ {
		_, pause, _ := s.ScrollGesture(ScrollScan)
		if pause < 300*time.Millisecond || pause > 1*time.Second {
			t.Errorf("scan pause %v outside [300ms, 1s]", pause)
		}
	}
}

// TestScrollGestureScrollUpProbability checks that the up flag is set with
// roughly the configured probability (ScrollUpProb = 0.15).
// Uses a wide tolerance band to avoid flakiness.
func TestScrollGestureScrollUpProbability(t *testing.T) {
	cfg := DefaultScrollConfig()
	s := NewScroll(cfg)

	upCount := 0
	n := 2000
	for i := 0; i < n; i++ {
		_, _, up := s.ScrollGesture(ScrollReading)
		if up {
			upCount++
		}
	}

	ratio := float64(upCount) / float64(n)
	expected := cfg.ScrollUpProb
	// Allow ±6% variance (roughly 3 sigma for binomial with p=0.15, n=2000).
	if ratio < expected-0.06 || ratio > expected+0.06 {
		t.Errorf("scroll-up ratio %.3f outside expected [%.3f, %.3f]",
			ratio, expected-0.06, expected+0.06)
	}
}

func TestScrollSequenceCoversPageHeight(t *testing.T) {
	s := NewScroll(DefaultScrollConfig())
	pageHeight := 5000

	actions := s.ScrollSequence(pageHeight, ScrollReading)
	if len(actions) == 0 {
		t.Fatal("ScrollSequence returned empty slice")
	}

	// Every action must have a positive distance and pause.
	for i, a := range actions {
		if a.Distance <= 0 {
			t.Errorf("action[%d] has non-positive distance %d", i, a.Distance)
		}
		if a.Pause <= 0 {
			t.Errorf("action[%d] has non-positive pause %v", i, a.Pause)
		}
	}
}

func TestScrollSequenceShortPageHasAtLeastOneAction(t *testing.T) {
	s := NewScroll(DefaultScrollConfig())
	actions := s.ScrollSequence(100, ScrollReading)
	if len(actions) == 0 {
		t.Error("ScrollSequence for short page returned no actions")
	}
}

func TestScrollActionFields(t *testing.T) {
	s := NewScroll(DefaultScrollConfig())
	actions := s.ScrollSequence(3000, ScrollScan)
	for i, a := range actions {
		if a.Distance < 0 {
			t.Errorf("action[%d].Distance %d is negative", i, a.Distance)
		}
	}
}
