package behavior_test

import (
	"testing"
	"time"

	"github.com/foxhound-scraper/foxhound/behavior"
)

// ---------------------------------------------------------------------------
// DefaultRhythmConfig
// ---------------------------------------------------------------------------

func TestDefaultRhythmConfigHasReasonableValues(t *testing.T) {
	cfg := behavior.DefaultRhythmConfig()

	if cfg.BurstMin <= 0 {
		t.Errorf("BurstMin must be > 0, got %d", cfg.BurstMin)
	}
	if cfg.BurstMax <= cfg.BurstMin {
		t.Errorf("BurstMax (%d) must be > BurstMin (%d)", cfg.BurstMax, cfg.BurstMin)
	}
	if cfg.PauseMin <= 0 {
		t.Errorf("PauseMin must be > 0, got %v", cfg.PauseMin)
	}
	if cfg.PauseMax <= cfg.PauseMin {
		t.Errorf("PauseMax (%v) must be > PauseMin (%v)", cfg.PauseMax, cfg.PauseMin)
	}
	if cfg.LongPauseMin <= 0 {
		t.Errorf("LongPauseMin must be > 0, got %v", cfg.LongPauseMin)
	}
	if cfg.LongPauseMax <= cfg.LongPauseMin {
		t.Errorf("LongPauseMax (%v) must be > LongPauseMin (%v)", cfg.LongPauseMax, cfg.LongPauseMin)
	}
	if cfg.LongPauseProb < 0 || cfg.LongPauseProb > 1 {
		t.Errorf("LongPauseProb must be in [0,1], got %v", cfg.LongPauseProb)
	}
}

func TestDefaultRhythmConfigSpecValues(t *testing.T) {
	// Architecture spec: burst 5-15, pause 10-60s, long pause 1-5min, prob 0.15.
	cfg := behavior.DefaultRhythmConfig()
	if cfg.BurstMin != 5 {
		t.Errorf("BurstMin: got %d, want 5", cfg.BurstMin)
	}
	if cfg.BurstMax != 15 {
		t.Errorf("BurstMax: got %d, want 15", cfg.BurstMax)
	}
	if cfg.PauseMin != 10*time.Second {
		t.Errorf("PauseMin: got %v, want 10s", cfg.PauseMin)
	}
	if cfg.PauseMax != 60*time.Second {
		t.Errorf("PauseMax: got %v, want 60s", cfg.PauseMax)
	}
	if cfg.LongPauseMin != 1*time.Minute {
		t.Errorf("LongPauseMin: got %v, want 1m", cfg.LongPauseMin)
	}
	if cfg.LongPauseMax != 5*time.Minute {
		t.Errorf("LongPauseMax: got %v, want 5m", cfg.LongPauseMax)
	}
	if cfg.LongPauseProb != 0.15 {
		t.Errorf("LongPauseProb: got %v, want 0.15", cfg.LongPauseProb)
	}
}

// ---------------------------------------------------------------------------
// NewRhythm
// ---------------------------------------------------------------------------

func TestNewRhythmReturnsNonNil(t *testing.T) {
	r := behavior.NewRhythm(behavior.DefaultRhythmConfig())
	if r == nil {
		t.Fatal("NewRhythm returned nil")
	}
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

func TestInitialStateIsBurst(t *testing.T) {
	r := behavior.NewRhythm(behavior.DefaultRhythmConfig())
	if r.State() != behavior.RhythmBurst {
		t.Errorf("initial state: got %v, want RhythmBurst", r.State())
	}
}

// ---------------------------------------------------------------------------
// Next — burst phase returns short delays
// ---------------------------------------------------------------------------

func TestNextDuringBurstReturnsBurstRangeDelay(t *testing.T) {
	cfg := behavior.DefaultRhythmConfig()
	// Use a very tight burst window so the first N calls are all in-burst.
	cfg.BurstMin = 5
	cfg.BurstMax = 5 // exactly 5 burst actions guaranteed
	// Make pause very long so we know any short delay is from burst, not pause.
	cfg.PauseMin = 30 * time.Second
	cfg.PauseMax = 60 * time.Second
	cfg.LongPauseProb = 0 // no long pauses to simplify test

	r := behavior.NewRhythm(cfg)

	// First 4 calls are still within the burst window — delays should be < PauseMin.
	for i := 0; i < 4; i++ {
		d := r.Next()
		if d >= cfg.PauseMin {
			t.Errorf("call %d during burst: delay %v >= PauseMin %v", i+1, d, cfg.PauseMin)
		}
		if r.State() != behavior.RhythmBurst {
			t.Errorf("call %d: expected RhythmBurst state, got %v", i+1, r.State())
		}
	}
}

// ---------------------------------------------------------------------------
// Next — state transitions after burst exhausted
// ---------------------------------------------------------------------------

func TestNextAfterBurstTransitionsToPauseOrLongPause(t *testing.T) {
	cfg := behavior.DefaultRhythmConfig()
	cfg.BurstMin = 3
	cfg.BurstMax = 3 // exhausted after exactly 3 Next() calls
	cfg.LongPauseProb = 0 // force normal pause

	r := behavior.NewRhythm(cfg)

	// Exhaust the burst.
	for i := 0; i < 3; i++ {
		r.Next()
	}

	// 4th call triggers end-of-burst pause.
	d := r.Next()
	state := r.State()

	if state != behavior.RhythmPause && state != behavior.RhythmLongPause {
		t.Errorf("after burst exhausted: got state %v, want Pause or LongPause", state)
	}
	if d < cfg.PauseMin {
		t.Errorf("post-burst delay %v < PauseMin %v", d, cfg.PauseMin)
	}
}

func TestNextLongPauseReturnedWhenProbabilityIsOne(t *testing.T) {
	cfg := behavior.DefaultRhythmConfig()
	cfg.BurstMin = 1
	cfg.BurstMax = 1 // burst = 1 action, then pause immediately
	cfg.LongPauseProb = 1.0 // always long pause
	cfg.LongPauseMin = 2 * time.Minute
	cfg.LongPauseMax = 2 * time.Minute // deterministic

	r := behavior.NewRhythm(cfg)
	r.Next() // consume the burst action
	d := r.Next()

	if r.State() != behavior.RhythmLongPause {
		t.Errorf("state: got %v, want RhythmLongPause", r.State())
	}
	if d < cfg.LongPauseMin {
		t.Errorf("long pause delay %v < LongPauseMin %v", d, cfg.LongPauseMin)
	}
}

// ---------------------------------------------------------------------------
// Next — after pause a new burst begins
// ---------------------------------------------------------------------------

func TestAfterPauseNewBurstBegins(t *testing.T) {
	cfg := behavior.DefaultRhythmConfig()
	cfg.BurstMin = 1
	cfg.BurstMax = 1
	cfg.LongPauseProb = 0
	cfg.PauseMin = 100 * time.Millisecond
	cfg.PauseMax = 100 * time.Millisecond

	r := behavior.NewRhythm(cfg)
	r.Next() // burst action
	r.Next() // pause
	// After pause, next call starts a new burst.
	r.Next()
	if r.State() != behavior.RhythmBurst {
		t.Errorf("after pause, expected RhythmBurst, got %v", r.State())
	}
}

// ---------------------------------------------------------------------------
// Next — returned delays are always non-negative
// ---------------------------------------------------------------------------

func TestNextAlwaysReturnsNonNegative(t *testing.T) {
	r := behavior.NewRhythm(behavior.DefaultRhythmConfig())
	for i := 0; i < 100; i++ {
		d := r.Next()
		if d < 0 {
			t.Errorf("iteration %d: Next() returned negative delay %v", i, d)
		}
	}
}

// ---------------------------------------------------------------------------
// BehaviorProfile integration
// ---------------------------------------------------------------------------

func TestBehaviorProfileContainsRhythm(t *testing.T) {
	p := behavior.ModerateProfile()
	// The Rhythm field must be populated (non-zero config).
	if p.Rhythm == nil {
		t.Fatal("BehaviorProfile.Rhythm must not be nil in ModerateProfile")
	}
}

func TestCarefulProfileHasRhythm(t *testing.T) {
	p := behavior.CarefulProfile()
	if p.Rhythm == nil {
		t.Fatal("BehaviorProfile.Rhythm must not be nil in CarefulProfile")
	}
}

func TestAggressiveProfileHasRhythm(t *testing.T) {
	p := behavior.AggressiveProfile()
	if p.Rhythm == nil {
		t.Fatal("BehaviorProfile.Rhythm must not be nil in AggressiveProfile")
	}
}
