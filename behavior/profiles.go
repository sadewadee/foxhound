package behavior

import "time"

// ProfileName identifies a preset behaviour configuration.
type ProfileName string

const (
	// ProfileCareful is slow and maximally human-like — best for heavily
	// protected sites where throughput is less important than stealth.
	ProfileCareful ProfileName = "careful"
	// ProfileModerate is the default balanced profile.
	ProfileModerate ProfileName = "moderate"
	// ProfileAggressive trades stealth for speed — suitable for lightly
	// protected sites.
	ProfileAggressive ProfileName = "aggressive"
)

// BehaviorProfile bundles all per-subsystem configurations into a named
// preset.  Pass individual Config structs to the subsystem constructors as
// needed.
type BehaviorProfile struct {
	Name       ProfileName
	Timing     TimingConfig
	Mouse      MouseConfig
	Scroll     ScrollConfig
	Keyboard   KeyboardConfig
	Navigation NavigationConfig
	// Rhythm controls the burst/pause cadence of a virtual user session.
	// It is initialised from DefaultRhythmConfig and tuned per preset.
	Rhythm *Rhythm
}

// GetProfile returns the named profile.  Falls back to ModerateProfile for
// unknown names so callers never receive a nil pointer.
func GetProfile(name ProfileName) *BehaviorProfile {
	switch name {
	case ProfileCareful:
		return CarefulProfile()
	case ProfileAggressive:
		return AggressiveProfile()
	default:
		return ModerateProfile()
	}
}

// CarefulProfile returns a very human-like, slow preset.
//
// Designed for Cloudflare Enterprise / Akamai Bot Manager targets where any
// timing anomaly triggers a challenge.  Throughput is intentionally low.
func CarefulProfile() *BehaviorProfile {
	return &BehaviorProfile{
		Name: ProfileCareful,

		Timing: TimingConfig{
			Mu:    1.5,  // median ≈ 4.5 s
			Sigma: 0.5,  // narrower spread → more predictably slow
			Min:   1 * time.Second,
			Max:   60 * time.Second,
		},

		Mouse: MouseConfig{
			Jitter:        3.0,
			OvershootProb: 0.30,
			OvershootPx:   5.0,
		},

		Scroll: ScrollConfig{
			ReadMinPx:      300,
			ReadMaxPx:      600,
			ScanMinPx:      800,
			ScanMaxPx:      1500,
			ReadPause:      3 * time.Second,
			ScanPause:      800 * time.Millisecond,
			ScrollUpProb:   0.25, // re-reads more often
			HorizMinPx:     150,
			HorizMaxPx:     400,
			HorizScanMinPx: 300,
			HorizScanMaxPx: 800,
		},

		Keyboard: KeyboardConfig{
			MinDelay: 80 * time.Millisecond,
			MaxDelay: 250 * time.Millisecond,
			TypoProb: 0.04, // more typos = more human
		},

		Navigation: NavigationConfig{
			PagesPerSession: Range{Min: 10, Max: 20},
			SessionDuration: DurationRange{Min: 15 * time.Minute, Max: 60 * time.Minute},
			SessionGap:      DurationRange{Min: 10 * time.Minute, Max: 30 * time.Minute},
			BackButtonProb:  0.40, // high back-button usage
			UselessPageProb: 0.20,
			SearchProb:      0.25,
		},

		Rhythm: NewRhythm(RhythmConfig{
			BurstMin:      5,
			BurstMax:      10,
			PauseMin:      30 * time.Second,
			PauseMax:      90 * time.Second,
			LongPauseMin:  3 * time.Minute,
			LongPauseMax:  8 * time.Minute,
			LongPauseProb: 0.25, // more frequent long breaks — very human
		}),
	}
}

// ModerateProfile returns the default balanced preset.
func ModerateProfile() *BehaviorProfile {
	return &BehaviorProfile{
		Name: ProfileModerate,

		Timing: TimingConfig{
			Mu:    1.0,  // median ≈ 2.7 s
			Sigma: 0.8,
			Min:   500 * time.Millisecond,
			Max:   30 * time.Second,
		},

		Mouse: DefaultMouseConfig(),

		Scroll: DefaultScrollConfig(),

		Keyboard: DefaultKeyboardConfig(),

		Navigation: DefaultNavigationConfig(),

		Rhythm: NewRhythm(DefaultRhythmConfig()),
	}
}

// AggressiveProfile returns a faster, higher-risk preset.
//
// Suitable for sites with minimal or no behavioural analysis.  Block rate
// will increase on heavily-protected targets.
func AggressiveProfile() *BehaviorProfile {
	return &BehaviorProfile{
		Name: ProfileAggressive,

		Timing: TimingConfig{
			Mu:    0.5,  // median ≈ 1.6 s
			Sigma: 0.6,
			Min:   200 * time.Millisecond,
			Max:   10 * time.Second,
		},

		Mouse: MouseConfig{
			Jitter:        1.0,
			OvershootProb: 0.10,
			OvershootPx:   2.0,
		},

		Scroll: ScrollConfig{
			ReadMinPx:      500,
			ReadMaxPx:      1000,
			ScanMinPx:      1500,
			ScanMaxPx:      4000,
			ReadPause:      500 * time.Millisecond,
			ScanPause:      200 * time.Millisecond,
			ScrollUpProb:   0.08,
			HorizMinPx:     300,
			HorizMaxPx:     800,
			HorizScanMinPx: 600,
			HorizScanMaxPx: 1600,
		},

		Keyboard: KeyboardConfig{
			MinDelay: 30 * time.Millisecond,
			MaxDelay: 100 * time.Millisecond,
			TypoProb: 0.01,
		},

		Navigation: NavigationConfig{
			PagesPerSession: Range{Min: 15, Max: 30},
			SessionDuration: DurationRange{Min: 5 * time.Minute, Max: 20 * time.Minute},
			SessionGap:      DurationRange{Min: 2 * time.Minute, Max: 10 * time.Minute},
			BackButtonProb:  0.15,
			UselessPageProb: 0.05,
			SearchProb:      0.10,
		},

		Rhythm: NewRhythm(RhythmConfig{
			BurstMin:      8,
			BurstMax:      20,
			PauseMin:      5 * time.Second,
			PauseMax:      20 * time.Second,
			LongPauseMin:  30 * time.Second,
			LongPauseMax:  2 * time.Minute,
			LongPauseProb: 0.08, // fewer long pauses — faster throughput
		}),
	}
}
