package behavior

import (
	"math"
	"math/rand/v2"
	"time"
)

// FatigueConfig controls the session warmup and fatigue model.
//
// The speed factor follows an inverted-U curve:
//
//	speed_factor(t) = warmup(t) * fatigue(t)
//	warmup(t)  = 1.0 + WarmupAmplitude * exp(-t / WarmupTau)
//	fatigue(t) = 1.0 + FatigueAmplitude * (1 - exp(-t / FatigueTau))
//
// At session start (t=0): factor = 1 + WarmupAmplitude (slow start).
// At cruise speed (~5 min): factor ~ 1.07.
// At late session (~30 min): fatigue dominates, factor rises again.
type FatigueConfig struct {
	// WarmupAmplitude is the fractional slowdown at session start (default 0.4 = 40% slower).
	WarmupAmplitude float64
	// WarmupTau is the warmup time constant -- 63% of warmup effect gone after this duration.
	WarmupTau time.Duration
	// FatigueAmplitude is the maximum fractional slowdown from fatigue (default 0.25 = 25% slower).
	FatigueAmplitude float64
	// FatigueTau is the fatigue time constant -- 63% of max fatigue reached after this duration.
	FatigueTau time.Duration
}

// DefaultFatigueConfig returns the moderate fatigue preset.
func DefaultFatigueConfig() FatigueConfig {
	return FatigueConfig{
		WarmupAmplitude:  0.4,
		WarmupTau:        2 * time.Minute,
		FatigueAmplitude: 0.25,
		FatigueTau:       30 * time.Minute,
	}
}

// SessionFatigue tracks elapsed session time and computes the current speed
// multiplier. Not safe for concurrent use -- each Walker owns its own instance.
type SessionFatigue struct {
	config    FatigueConfig
	startTime time.Time
	started   bool
}

// NewSessionFatigue creates a SessionFatigue with the given configuration.
func NewSessionFatigue(cfg FatigueConfig) *SessionFatigue {
	return &SessionFatigue{config: cfg}
}

// Start records the session start time. Call once when the walker begins.
func (f *SessionFatigue) Start() {
	f.startTime = time.Now()
	f.started = true
}

// StartAt records a specific start time (useful for testing).
func (f *SessionFatigue) StartAt(t time.Time) {
	f.startTime = t
	f.started = true
}

// Factor returns the current speed multiplier. Values > 1.0 mean slower.
//
// Timeline with default config:
//
//	t=0s:    1.40 (40% slower -- session warmup)
//	t=60s:   1.24 (warming up)
//	t=120s:  1.15 (near cruise speed)
//	t=300s:  1.07 (cruising, slight fatigue)
//	t=1800s: 1.16 (fatigue building)
//	t=3600s: 1.22 (noticeable slowdown)
func (f *SessionFatigue) Factor() float64 {
	if !f.started {
		return 1.0
	}
	t := time.Since(f.startTime).Seconds()
	return f.factorAt(t)
}

// FactorAt returns the speed multiplier at a given elapsed time in seconds.
// Exposed for testing.
func (f *SessionFatigue) FactorAt(elapsed time.Duration) float64 {
	return f.factorAt(elapsed.Seconds())
}

func (f *SessionFatigue) factorAt(t float64) float64 {
	tauW := f.config.WarmupTau.Seconds()
	tauF := f.config.FatigueTau.Seconds()

	if tauW <= 0 {
		tauW = 120
	}
	if tauF <= 0 {
		tauF = 1800
	}

	warmup := 1.0 + f.config.WarmupAmplitude*math.Exp(-t/tauW)
	fatigue := 1.0 + f.config.FatigueAmplitude*(1.0-math.Exp(-t/tauF))

	base := warmup * fatigue

	// Add per-call noise to prevent smooth curve detection.
	// ±5% Gaussian noise makes the curve jagged like real human fatigue.
	noise := 1.0 + rand.NormFloat64()*0.05
	if noise < 0.85 {
		noise = 0.85
	}
	if noise > 1.15 {
		noise = 1.15
	}

	return base * noise
}

// AdjustDelay multiplies the base delay by the current fatigue factor.
func (f *SessionFatigue) AdjustDelay(base time.Duration) time.Duration {
	return time.Duration(float64(base) * f.Factor())
}
