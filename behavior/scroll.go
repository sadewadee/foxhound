package behavior

import (
	"math"
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

// ScrollAxis determines the scroll direction.
type ScrollAxis int

const (
	// ScrollVertical scrolls the viewport up/down (default).
	ScrollVertical ScrollAxis = iota
	// ScrollHorizontal scrolls the viewport left/right.
	ScrollHorizontal
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
	// ReadPause is the midpoint pause for reading mode. Actual pauses are
	// sampled from [ReadPause*0.5, ReadPause*2.5] to simulate natural variance.
	ReadPause time.Duration
	// ScanPause is the midpoint pause for scan mode. Actual pauses are
	// sampled from [ScanPause*0.6, ScanPause*2.0].
	ScanPause time.Duration
	// ScrollUpProb is the probability that any given gesture scrolls upward
	// (re-reading behaviour).
	ScrollUpProb float64
	// HorizMinPx is the minimum horizontal scroll distance per gesture in reading mode.
	HorizMinPx int
	// HorizMaxPx is the maximum horizontal scroll distance per gesture in reading mode.
	HorizMaxPx int
	// HorizScanMinPx is the minimum horizontal scroll distance per gesture in scan mode.
	HorizScanMinPx int
	// HorizScanMaxPx is the maximum horizontal scroll distance per gesture in scan mode.
	HorizScanMaxPx int
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
		ScrollUpProb:    0.15,
		HorizMinPx:     200,
		HorizMaxPx:     600,
		HorizScanMinPx: 400,
		HorizScanMaxPx: 1200,
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
	// Axis is the scroll direction (vertical or horizontal).
	Axis ScrollAxis
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
		// Gamma(alpha=2.5, rate=0.0012): mean ~2083px, mode ~1250px
		distance = int(math.Round(GammaClamped(2.5, 0.0012, float64(s.config.ScanMinPx), float64(s.config.ScanMaxPx))))
		pause = weibullPause(s.config.ScanPause, 0.6, 2.0, 1.8)
	default: // ScrollReading
		// Gamma(alpha=3.0, rate=0.006): mean ~500px, mode ~333px
		distance = int(math.Round(GammaClamped(3.0, 0.006, float64(s.config.ReadMinPx), float64(s.config.ReadMaxPx))))
		pause = weibullPause(s.config.ReadPause, 0.5, 2.5, 2.0)
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

// ScrollGestureAxis returns the attributes of a single scroll action for the
// given axis. For vertical, this delegates to ScrollGesture. For horizontal,
// distances are drawn from the horizontal config ranges.
func (s *Scroll) ScrollGestureAxis(mode ScrollMode, axis ScrollAxis) (distance int, pause time.Duration, reverse bool) {
	if axis == ScrollVertical {
		return s.ScrollGesture(mode)
	}

	reverse = rand.Float64() < s.config.ScrollUpProb

	switch mode {
	case ScrollScan:
		distance = int(math.Round(GammaClamped(2.5, 0.0012, float64(s.config.HorizScanMinPx), float64(s.config.HorizScanMaxPx))))
		pause = weibullPause(s.config.ScanPause, 0.6, 2.0, 1.8)
	default: // ScrollReading
		distance = int(math.Round(GammaClamped(3.0, 0.006, float64(s.config.HorizMinPx), float64(s.config.HorizMaxPx))))
		pause = weibullPause(s.config.ReadPause, 0.5, 2.5, 2.0)
	}
	return
}

// ScrollSequenceAxis generates a complete scroll sequence for a page with the
// given pixel extent along the specified axis.
func (s *Scroll) ScrollSequenceAxis(extent int, mode ScrollMode, axis ScrollAxis) []ScrollAction {
	if extent <= 0 {
		d, p, r := s.ScrollGestureAxis(mode, axis)
		return []ScrollAction{{Distance: d, Pause: p, Up: r, Axis: axis}}
	}

	var actions []ScrollAction
	netForward := 0

	for netForward < extent {
		dist, pause, reverse := s.ScrollGestureAxis(mode, axis)
		actions = append(actions, ScrollAction{Distance: dist, Pause: pause, Up: reverse, Axis: axis})
		if reverse {
			netForward -= dist
			if netForward < 0 {
				netForward = 0
			}
		} else {
			netForward += dist
		}
	}

	return actions
}

// weibullPause returns a Weibull-distributed pause centered around midpoint.
// loFactor and hiFactor define the bounds as multiples of midpoint.
// k is the Weibull shape parameter.
func weibullPause(midpoint time.Duration, loFactor, hiFactor, k float64) time.Duration {
	lo := float64(midpoint) * loFactor
	hi := float64(midpoint) * hiFactor
	lambda := (hi - lo) * 0.4
	sample := WeibullClamped(k, lambda, 0, hi-lo)
	return time.Duration(lo + sample)
}
