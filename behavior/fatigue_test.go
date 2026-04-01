package behavior

import (
	"testing"
	"time"
)

func TestFatigueWarmupSlowsEarlyActions(t *testing.T) {
	f := NewSessionFatigue(DefaultFatigueConfig())
	f.StartAt(time.Now())

	factor := f.FactorAt(0)
	if factor < 1.3 {
		t.Fatalf("Expected warmup factor > 1.3 at t=0, got %f", factor)
	}
}

func TestFatigueLateSessionSlows(t *testing.T) {
	f := NewSessionFatigue(DefaultFatigueConfig())
	f.StartAt(time.Now())

	factor := f.FactorAt(30 * time.Minute)
	if factor < 1.1 {
		t.Fatalf("Expected fatigue factor > 1.1 at t=30min, got %f", factor)
	}
}

func TestFatigueMiddleSessionIsFastest(t *testing.T) {
	f := NewSessionFatigue(DefaultFatigueConfig())
	f.StartAt(time.Now())

	early := f.FactorAt(0)
	cruise := f.FactorAt(5 * time.Minute)
	late := f.FactorAt(30 * time.Minute)

	if cruise >= early {
		t.Fatalf("Cruise factor (%f) should be less than early (%f)", cruise, early)
	}
	if cruise >= late {
		t.Fatalf("Cruise factor (%f) should be less than late (%f)", cruise, late)
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
	adjusted := f.AdjustDelay(base)
	// At t=0 warmup factor ~1.4, so adjusted should be > base
	if adjusted <= base {
		t.Fatalf("Expected adjusted delay > base at session start, got %v vs %v", adjusted, base)
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

	factor := f.FactorAt(0)
	if factor < 1.4 {
		t.Fatalf("Careful profile should have higher warmup, got %f", factor)
	}
}
