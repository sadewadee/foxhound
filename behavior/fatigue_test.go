package behavior

import (
	"testing"
	"time"
)

// avgFactorAt returns the average of n FactorAt calls to smooth per-call noise.
func avgFactorAt(f *SessionFatigue, d time.Duration, n int) float64 {
	var sum float64
	for i := 0; i < n; i++ {
		sum += f.FactorAt(d)
	}
	return sum / float64(n)
}

func TestFatigueWarmupSlowsEarlyActions(t *testing.T) {
	f := NewSessionFatigue(DefaultFatigueConfig())
	f.StartAt(time.Now())

	factor := avgFactorAt(f, 0, 100)
	if factor < 1.3 {
		t.Fatalf("Expected avg warmup factor > 1.3 at t=0, got %f", factor)
	}
}

func TestFatigueLateSessionSlows(t *testing.T) {
	f := NewSessionFatigue(DefaultFatigueConfig())
	f.StartAt(time.Now())

	factor := avgFactorAt(f, 30*time.Minute, 100)
	if factor < 1.1 {
		t.Fatalf("Expected avg fatigue factor > 1.1 at t=30min, got %f", factor)
	}
}

func TestFatigueMiddleSessionIsFastest(t *testing.T) {
	f := NewSessionFatigue(DefaultFatigueConfig())
	f.StartAt(time.Now())

	early := avgFactorAt(f, 0, 100)
	cruise := avgFactorAt(f, 5*time.Minute, 100)
	late := avgFactorAt(f, 30*time.Minute, 100)

	if cruise >= early {
		t.Fatalf("Avg cruise factor (%f) should be less than avg early (%f)", cruise, early)
	}
	if cruise >= late {
		t.Fatalf("Avg cruise factor (%f) should be less than avg late (%f)", cruise, late)
	}
}

func TestFatigueNotStarted(t *testing.T) {
	f := NewSessionFatigue(DefaultFatigueConfig())
	if f.Factor() != 1.0 {
		t.Fatalf("Expected factor 1.0 when not started, got %f", f.Factor())
	}
}

func TestFatigueAdjustDelay(t *testing.T) {
	f := NewSessionFatigue(DefaultFatigueConfig())
	f.StartAt(time.Now())

	base := 1 * time.Second
	// Average to smooth noise
	var sum time.Duration
	for i := 0; i < 50; i++ {
		sum += f.AdjustDelay(base)
	}
	avg := sum / 50
	if avg <= base {
		t.Fatalf("Expected avg adjusted delay > base at session start, got %v vs %v", avg, base)
	}
}

func TestFatigueCarefulProfile(t *testing.T) {
	cfg := FatigueConfig{
		WarmupAmplitude:  0.5,
		WarmupTau:        2 * time.Minute,
		FatigueAmplitude: 0.3,
		FatigueTau:       30 * time.Minute,
	}
	f := NewSessionFatigue(cfg)
	f.StartAt(time.Now())

	factor := avgFactorAt(f, 0, 100)
	if factor < 1.4 {
		t.Fatalf("Careful profile should have higher warmup, got %f", factor)
	}
}
