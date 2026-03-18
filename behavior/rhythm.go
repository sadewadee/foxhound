package behavior

import (
	"math/rand/v2"
	"time"
)

// RhythmState describes the current phase of a scraping session's rhythm.
type RhythmState int

const (
	// RhythmBurst is the active phase: rapid sequential actions (page loads,
	// clicks, form submissions). Delays between actions are short.
	RhythmBurst RhythmState = iota
	// RhythmPause is the rest phase after a burst: simulates reading time or
	// light distraction. Duration is drawn from [PauseMin, PauseMax].
	RhythmPause
	// RhythmLongPause is a deeper rest phase: simulates a break or context
	// switch away from the browser. Duration is drawn from [LongPauseMin, LongPauseMax].
	RhythmLongPause
)

// RhythmConfig controls the burst/pause pattern for a scraping session.
//
// Architecture spec:
//
//	Burst:      5-15 rapid actions
//	Pause:      10-60 s
//	Burst:      3-8 actions
//	Long pause: 1-5 min (probability 0.15 after each burst)
type RhythmConfig struct {
	// BurstMin is the minimum number of actions in a single burst.
	BurstMin int
	// BurstMax is the maximum number of actions in a single burst.
	BurstMax int
	// PauseMin is the lower bound for a normal inter-burst pause.
	PauseMin time.Duration
	// PauseMax is the upper bound for a normal inter-burst pause.
	PauseMax time.Duration
	// LongPauseMin is the lower bound for a long pause.
	LongPauseMin time.Duration
	// LongPauseMax is the upper bound for a long pause.
	LongPauseMax time.Duration
	// LongPauseProb is the probability (in [0, 1]) that a long pause is
	// chosen instead of a normal pause at the end of each burst.
	LongPauseProb float64
}

// DefaultRhythmConfig returns the rhythm configuration that matches the
// architecture specification:
//
//	BurstMin/Max:       5 – 15 actions
//	PauseMin/Max:       10 s – 60 s
//	LongPauseMin/Max:   1 min – 5 min
//	LongPauseProb:      0.15
func DefaultRhythmConfig() RhythmConfig {
	return RhythmConfig{
		BurstMin:      5,
		BurstMax:      15,
		PauseMin:      10 * time.Second,
		PauseMax:      60 * time.Second,
		LongPauseMin:  1 * time.Minute,
		LongPauseMax:  5 * time.Minute,
		LongPauseProb: 0.15,
	}
}

// Rhythm manages the burst/pause state machine for a single virtual user.
// It is not safe for concurrent use — each Walker owns its own Rhythm.
type Rhythm struct {
	config      RhythmConfig
	state       RhythmState
	actionsLeft int // remaining burst actions before transitioning to a pause
}

// NewRhythm creates a Rhythm initialised at the beginning of the first burst.
func NewRhythm(cfg RhythmConfig) *Rhythm {
	r := &Rhythm{config: cfg}
	r.startNewBurst()
	return r
}

// State returns the current rhythm state.
func (r *Rhythm) State() RhythmState {
	return r.state
}

// Next returns the delay that the caller should wait before performing the next
// action and advances the internal state machine.
//
// State transitions:
//
//	RhythmBurst (actionsLeft > 0):
//	    Decrement actionsLeft, return a short burst delay. State stays Burst.
//
//	RhythmBurst (actionsLeft == 0):
//	    Burst exhausted. Transition to RhythmPause or RhythmLongPause and
//	    return the pause duration so the caller sleeps before the next burst.
//
//	RhythmPause / RhythmLongPause:
//	    Pause finished. Start a new burst and return 0 so the caller acts
//	    immediately (the caller already slept the pause duration on the
//	    previous call).
func (r *Rhythm) Next() time.Duration {
	switch r.state {
	case RhythmBurst:
		if r.actionsLeft > 0 {
			r.actionsLeft--
			return r.burstDelay()
		}
		// All burst actions consumed — transition to a pause.
		return r.transitionToPause()

	case RhythmPause, RhythmLongPause:
		// The caller has finished sleeping; begin a fresh burst.
		r.startNewBurst()
		return 0

	default:
		r.startNewBurst()
		return 0
	}
}

// burstDelay returns a short, human-like delay appropriate within a burst.
// Using a small uniform range keeps individual actions feeling rapid while
// still avoiding perfectly-uniform timing signatures.
func (r *Rhythm) burstDelay() time.Duration {
	// 200 ms – 1.5 s: fast but not robotic.
	const minMs, maxMs = 200.0, 1500.0
	ms := minMs + rand.Float64()*(maxMs-minMs)
	return time.Duration(ms * float64(time.Millisecond))
}

// transitionToPause chooses between a normal pause and a long pause based on
// LongPauseProb, updates the state, and returns the pause duration.
func (r *Rhythm) transitionToPause() time.Duration {
	if r.config.LongPauseProb > 0 && rand.Float64() < r.config.LongPauseProb {
		r.state = RhythmLongPause
		return uniformDuration(r.config.LongPauseMin, r.config.LongPauseMax)
	}
	r.state = RhythmPause
	return uniformDuration(r.config.PauseMin, r.config.PauseMax)
}

// startNewBurst selects a random burst length in [BurstMin, BurstMax] and
// resets the state to RhythmBurst. actionsLeft is the number of burst-delay
// calls that Next() will serve before switching to a pause.
func (r *Rhythm) startNewBurst() {
	span := r.config.BurstMax - r.config.BurstMin
	if span <= 0 {
		r.actionsLeft = r.config.BurstMin
	} else {
		r.actionsLeft = r.config.BurstMin + rand.IntN(span+1)
	}
	r.state = RhythmBurst
}

// uniformDuration returns a uniformly-distributed duration in [lo, hi].
func uniformDuration(lo, hi time.Duration) time.Duration {
	if hi <= lo {
		return lo
	}
	span := int64(hi - lo)
	return lo + time.Duration(rand.Int64N(span))
}
