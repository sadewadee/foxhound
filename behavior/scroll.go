package behavior

import (
	"math/rand/v2"
	"time"
)

// ScrollMode determines which scroll-speed profile is used.
type ScrollMode int

const (
	// ScrollReading simulates a user reading content: short gestures with
	// long pauses.
	ScrollReading ScrollMode = iota
	// ScrollScan simulates a user skimming a page: long gestures, short
	// pauses.
	ScrollScan
)

// ScrollConfig configures scroll behaviour generation.
type ScrollConfig struct {
	// ReadMinPx is the minimum scroll distance per gesture in reading mode.
	ReadMinPx int
	// ReadMaxPx is the maximum scroll distance per gesture in reading mode.
	ReadMaxPx int
	// ScanMinPx is the minimum scroll distance per gesture in scan mode.
	ScanMinPx int
	// ScanMaxPx is the maximum scroll distance per gesture in scan mode.
	ScanMaxPx int
	// ReadPause is the *base* pause for reading mode (not a single value;
	// actual pauses are sampled from [1s, 5s]).
	ReadPause time.Duration
	// ScanPause is the *base* pause for scan mode (actual: [300ms, 1s]).
	ScanPause time.Duration
	// ScrollUpProb is the probability that any given gesture scrolls upward
	// (re-reading behaviour).
	ScrollUpProb float64
}

// DefaultScrollConfig returns the architecture-recommended defaults.
func DefaultScrollConfig() ScrollConfig {
	return ScrollConfig{
		ReadMinPx:    300,
		ReadMaxPx:    800,
		ScanMinPx:    1000,
		ScanMaxPx:    3000,
		ReadPause:    2 * time.Second, // midpoint reference — actual range [1s,5s]
		ScanPause:    500 * time.Millisecond,
		ScrollUpProb: 0.15,
	}
}

// ScrollAction describes a single scroll gesture and the pause that follows it.
type ScrollAction struct {
	// Distance is the scroll magnitude in pixels (always positive; Up indicates
	// direction).
	Distance int
	// Pause is the delay after this gesture before the next action.
	Pause time.Duration
	// Up is true if the scroll gesture moves the viewport upward.
	Up bool
}

// Scroll generates human-like scroll behaviour.
type Scroll struct {
	config ScrollConfig
}

// NewScroll creates a Scroll with the supplied configuration.
func NewScroll(cfg ScrollConfig) *Scroll {
	return &Scroll{config: cfg}
}

// ScrollGesture returns the attributes of a single scroll action:
//   - distance: pixels scrolled (always positive)
//   - pause:    delay to wait after the gesture
//   - up:       direction flag
func (s *Scroll) ScrollGesture(mode ScrollMode) (distance int, pause time.Duration, up bool) {
	up = rand.Float64() < s.config.ScrollUpProb

	switch mode {
	case ScrollScan:
		distance = s.config.ScanMinPx + rand.IntN(s.config.ScanMaxPx-s.config.ScanMinPx+1)
		pause = uniformDurationMs(300, 1000)
	default: // ScrollReading
		distance = s.config.ReadMinPx + rand.IntN(s.config.ReadMaxPx-s.config.ReadMinPx+1)
		pause = uniformDurationMs(1000, 5000)
	}
	return
}

// ScrollSequence generates a complete scroll sequence for a page with the
// given pixel height.  It keeps producing gestures until the cumulative net
// downward scroll roughly covers pageHeight, then stops.  Upward gestures
// count against the total to ensure the sequence is realistic in length.
// Every page gets at least one action.
func (s *Scroll) ScrollSequence(pageHeight int, mode ScrollMode) []ScrollAction {
	if pageHeight <= 0 {
		d, p, u := s.ScrollGesture(mode)
		return []ScrollAction{{Distance: d, Pause: p, Up: u}}
	}

	var actions []ScrollAction
	netDown := 0

	for netDown < pageHeight {
		dist, pause, up := s.ScrollGesture(mode)
		actions = append(actions, ScrollAction{Distance: dist, Pause: pause, Up: up})
		if up {
			netDown -= dist
			if netDown < 0 {
				netDown = 0
			}
		} else {
			netDown += dist
		}
	}

	return actions
}

// uniformDurationMs returns a uniformly-distributed duration in [minMs, maxMs].
func uniformDurationMs(minMs, maxMs float64) time.Duration {
	ms := minMs + rand.Float64()*(maxMs-minMs)
	return time.Duration(ms * float64(time.Millisecond))
}
