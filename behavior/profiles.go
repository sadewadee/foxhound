package behavior

import (
	"math/rand/v2"
	"time"
)

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
	// Fatigue controls the session warmup and fatigue model.
	Fatigue FatigueConfig
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
			Mu:    1.5, // median ≈ 4.5 s
			Sigma: 0.5, // narrower spread → more predictably slow
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
			MinDelay:    80 * time.Millisecond,
			MaxDelay:    250 * time.Millisecond,
			TypoProb:    0.04, // more typos = more human
			BigramModel: true,
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

		Fatigue: FatigueConfig{
			WarmupAmplitude:  0.5,
			WarmupTau:        2 * time.Minute,
			FatigueAmplitude: 0.3,
			FatigueTau:       30 * time.Minute,
		},
	}
}

// ModerateProfile returns the default balanced preset.
func ModerateProfile() *BehaviorProfile {
	return &BehaviorProfile{
		Name: ProfileModerate,

		Timing: TimingConfig{
			Mu:    1.0, // median ≈ 2.7 s
			Sigma: 0.8,
			Min:   500 * time.Millisecond,
			Max:   30 * time.Second,
		},

		Mouse: DefaultMouseConfig(),

		Scroll: DefaultScrollConfig(),

		Keyboard: KeyboardConfig{
			MinDelay:    50 * time.Millisecond,
			MaxDelay:    200 * time.Millisecond,
			TypoProb:    0.02,
			BigramModel: true,
		},

		Navigation: DefaultNavigationConfig(),

		Rhythm: NewRhythm(DefaultRhythmConfig()),

		Fatigue: DefaultFatigueConfig(),
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
			Mu:    0.5, // median ≈ 1.6 s
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
			MinDelay:    30 * time.Millisecond,
			MaxDelay:    100 * time.Millisecond,
			TypoProb:    0.01,
			BigramModel: true,
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

		Fatigue: FatigueConfig{
			WarmupAmplitude:  0.2,
			WarmupTau:        2 * time.Minute,
			FatigueAmplitude: 0.15,
			FatigueTau:       30 * time.Minute,
		},
	}
}

// Jitter returns a copy of this profile with per-session random perturbation
// applied to all numeric parameters. This prevents clustering attacks that
// would otherwise identify all sessions using the same named profile.
//
// Each parameter is multiplied by (1 + U(-jitterFrac, +jitterFrac)) where
// jitterFrac defaults to 0.15 (±15%).
func (p *BehaviorProfile) Jitter() *BehaviorProfile {
	return p.JitterBy(0.15)
}

// JitterBy returns a copy with the specified jitter fraction applied.
func (p *BehaviorProfile) JitterBy(frac float64) *BehaviorProfile {
	jf := func(v float64) float64 {
		return v * (1.0 + (rand.Float64()*2-1)*frac)
	}
	jd := func(d time.Duration) time.Duration {
		return time.Duration(float64(d) * (1.0 + (rand.Float64()*2-1)*frac))
	}
	ji := func(v int) int {
		result := int(float64(v) * (1.0 + (rand.Float64()*2-1)*frac))
		if result < 1 {
			result = 1
		}
		return result
	}

	// clampInt ensures min <= max after independent jitter.
	clampI := func(lo, hi int) (int, int) {
		if lo > hi {
			lo, hi = hi, lo
		}
		return lo, hi
	}
	// clampDuration ensures min <= max after independent jitter.
	clampD := func(lo, hi time.Duration) (time.Duration, time.Duration) {
		if lo > hi {
			lo, hi = hi, lo
		}
		return lo, hi
	}

	timingMin, timingMax := clampD(jd(p.Timing.Min), jd(p.Timing.Max))
	kbMin, kbMax := clampD(jd(p.Keyboard.MinDelay), jd(p.Keyboard.MaxDelay))
	readMinPx, readMaxPx := clampI(ji(p.Scroll.ReadMinPx), ji(p.Scroll.ReadMaxPx))
	scanMinPx, scanMaxPx := clampI(ji(p.Scroll.ScanMinPx), ji(p.Scroll.ScanMaxPx))
	horizMinPx, horizMaxPx := clampI(ji(p.Scroll.HorizMinPx), ji(p.Scroll.HorizMaxPx))
	horizScanMinPx, horizScanMaxPx := clampI(ji(p.Scroll.HorizScanMinPx), ji(p.Scroll.HorizScanMaxPx))
	ppsMin, ppsMax := clampI(ji(p.Navigation.PagesPerSession.Min), ji(p.Navigation.PagesPerSession.Max))
	sdMin, sdMax := clampD(jd(p.Navigation.SessionDuration.Min), jd(p.Navigation.SessionDuration.Max))
	sgMin, sgMax := clampD(jd(p.Navigation.SessionGap.Min), jd(p.Navigation.SessionGap.Max))
	burstMin, burstMax := clampI(ji(p.Rhythm.config.BurstMin), ji(p.Rhythm.config.BurstMax))
	pauseMin, pauseMax := clampD(jd(p.Rhythm.config.PauseMin), jd(p.Rhythm.config.PauseMax))
	longPauseMin, longPauseMax := clampD(jd(p.Rhythm.config.LongPauseMin), jd(p.Rhythm.config.LongPauseMax))

	return &BehaviorProfile{
		Name: p.Name,
		Timing: TimingConfig{
			Mu:    jf(p.Timing.Mu),
			Sigma: jf(p.Timing.Sigma),
			Min:   timingMin,
			Max:   timingMax,
		},
		Mouse: MouseConfig{
			Jitter:        jf(p.Mouse.Jitter),
			OvershootProb: jf(p.Mouse.OvershootProb),
			OvershootPx:   jf(p.Mouse.OvershootPx),
		},
		Scroll: ScrollConfig{
			ReadMinPx:      readMinPx,
			ReadMaxPx:      readMaxPx,
			ScanMinPx:      scanMinPx,
			ScanMaxPx:      scanMaxPx,
			ReadPause:      jd(p.Scroll.ReadPause),
			ScanPause:      jd(p.Scroll.ScanPause),
			ScrollUpProb:   jf(p.Scroll.ScrollUpProb),
			HorizMinPx:     horizMinPx,
			HorizMaxPx:     horizMaxPx,
			HorizScanMinPx: horizScanMinPx,
			HorizScanMaxPx: horizScanMaxPx,
		},
		Keyboard: KeyboardConfig{
			MinDelay:    kbMin,
			MaxDelay:    kbMax,
			TypoProb:    jf(p.Keyboard.TypoProb),
			BigramModel: p.Keyboard.BigramModel,
		},
		Navigation: NavigationConfig{
			PagesPerSession: Range{Min: ppsMin, Max: ppsMax},
			SessionDuration: DurationRange{Min: sdMin, Max: sdMax},
			SessionGap:      DurationRange{Min: sgMin, Max: sgMax},
			BackButtonProb:  jf(p.Navigation.BackButtonProb),
			UselessPageProb: jf(p.Navigation.UselessPageProb),
			SearchProb:      jf(p.Navigation.SearchProb),
		},
		Rhythm: NewRhythm(RhythmConfig{
			BurstMin:      burstMin,
			BurstMax:      burstMax,
			PauseMin:      pauseMin,
			PauseMax:      pauseMax,
			LongPauseMin:  longPauseMin,
			LongPauseMax:  longPauseMax,
			LongPauseProb: jf(p.Rhythm.config.LongPauseProb),
		}),
		Fatigue: FatigueConfig{
			WarmupAmplitude:  jf(p.Fatigue.WarmupAmplitude),
			WarmupTau:        jd(p.Fatigue.WarmupTau),
			FatigueAmplitude: jf(p.Fatigue.FatigueAmplitude),
			FatigueTau:       jd(p.Fatigue.FatigueTau),
		},
	}
}
