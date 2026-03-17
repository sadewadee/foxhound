package behavior

import (
	"testing"
)

func TestGetProfileCareful(t *testing.T) {
	p := GetProfile(ProfileCareful)
	if p == nil {
		t.Fatal("GetProfile(ProfileCareful) returned nil")
	}
	if p.Name != ProfileCareful {
		t.Errorf("profile name = %q, want %q", p.Name, ProfileCareful)
	}
}

func TestGetProfileModerate(t *testing.T) {
	p := GetProfile(ProfileModerate)
	if p == nil {
		t.Fatal("GetProfile(ProfileModerate) returned nil")
	}
	if p.Name != ProfileModerate {
		t.Errorf("profile name = %q, want %q", p.Name, ProfileModerate)
	}
}

func TestGetProfileAggressive(t *testing.T) {
	p := GetProfile(ProfileAggressive)
	if p == nil {
		t.Fatal("GetProfile(ProfileAggressive) returned nil")
	}
	if p.Name != ProfileAggressive {
		t.Errorf("profile name = %q, want %q", p.Name, ProfileAggressive)
	}
}

func TestGetProfileUnknownReturnsModerate(t *testing.T) {
	p := GetProfile("nonexistent")
	if p == nil {
		t.Fatal("GetProfile(unknown) returned nil")
	}
	if p.Name != ProfileModerate {
		t.Errorf("GetProfile(unknown) returned %q, want %q (moderate fallback)", p.Name, ProfileModerate)
	}
}

// TestProfilesHaveDifferentTimingMu verifies the three profiles are distinct.
// Architecture specifies: careful mu=1.5, moderate mu=1.0, aggressive mu=0.5.
func TestProfilesHaveDifferentTimingMu(t *testing.T) {
	careful := CarefulProfile()
	moderate := ModerateProfile()
	aggressive := AggressiveProfile()

	if careful.Timing.Mu <= moderate.Timing.Mu {
		t.Errorf("careful mu (%.1f) should be > moderate mu (%.1f)", careful.Timing.Mu, moderate.Timing.Mu)
	}
	if moderate.Timing.Mu <= aggressive.Timing.Mu {
		t.Errorf("moderate mu (%.1f) should be > aggressive mu (%.1f)", moderate.Timing.Mu, aggressive.Timing.Mu)
	}
}

func TestCarefulProfileTimingValues(t *testing.T) {
	p := CarefulProfile()
	if p.Timing.Mu != 1.5 {
		t.Errorf("CarefulProfile Timing.Mu = %.1f, want 1.5", p.Timing.Mu)
	}
	if p.Timing.Sigma != 0.5 {
		t.Errorf("CarefulProfile Timing.Sigma = %.1f, want 0.5", p.Timing.Sigma)
	}
}

func TestModerateProfileTimingValues(t *testing.T) {
	p := ModerateProfile()
	if p.Timing.Mu != 1.0 {
		t.Errorf("ModerateProfile Timing.Mu = %.1f, want 1.0", p.Timing.Mu)
	}
	if p.Timing.Sigma != 0.8 {
		t.Errorf("ModerateProfile Timing.Sigma = %.1f, want 0.8", p.Timing.Sigma)
	}
}

func TestAggressiveProfileTimingValues(t *testing.T) {
	p := AggressiveProfile()
	if p.Timing.Mu != 0.5 {
		t.Errorf("AggressiveProfile Timing.Mu = %.1f, want 0.5", p.Timing.Mu)
	}
	if p.Timing.Sigma != 0.6 {
		t.Errorf("AggressiveProfile Timing.Sigma = %.1f, want 0.6", p.Timing.Sigma)
	}
}

func TestProfileScrollConfigsDiffer(t *testing.T) {
	careful := CarefulProfile()
	aggressive := AggressiveProfile()

	// Careful should use reading scroll (smaller distances) vs aggressive scan.
	if careful.Scroll.ReadPause <= aggressive.Scroll.ScanPause {
		t.Errorf("careful ReadPause (%v) should be > aggressive ScanPause (%v)",
			careful.Scroll.ReadPause, aggressive.Scroll.ScanPause)
	}
}

func TestProfileNavigationBackButtonProbDiffers(t *testing.T) {
	careful := CarefulProfile()
	moderate := ModerateProfile()
	aggressive := AggressiveProfile()

	// Careful has higher back-button probability (more human-like exploration).
	if careful.Navigation.BackButtonProb < moderate.Navigation.BackButtonProb {
		t.Errorf("careful back-button prob %.2f should be >= moderate %.2f",
			careful.Navigation.BackButtonProb, moderate.Navigation.BackButtonProb)
	}
	if moderate.Navigation.BackButtonProb < aggressive.Navigation.BackButtonProb {
		t.Errorf("moderate back-button prob %.2f should be >= aggressive %.2f",
			moderate.Navigation.BackButtonProb, aggressive.Navigation.BackButtonProb)
	}
}

func TestAllProfilesHaveValidTimingBounds(t *testing.T) {
	for _, name := range []ProfileName{ProfileCareful, ProfileModerate, ProfileAggressive} {
		p := GetProfile(name)
		if p.Timing.Min <= 0 {
			t.Errorf("%s: Timing.Min must be positive, got %v", name, p.Timing.Min)
		}
		if p.Timing.Max <= p.Timing.Min {
			t.Errorf("%s: Timing.Max must be > Min", name)
		}
	}
}

func TestAllProfilesHaveValidScrollConfig(t *testing.T) {
	for _, name := range []ProfileName{ProfileCareful, ProfileModerate, ProfileAggressive} {
		p := GetProfile(name)
		if p.Scroll.ReadMinPx <= 0 {
			t.Errorf("%s: Scroll.ReadMinPx must be positive", name)
		}
		if p.Scroll.ReadMaxPx < p.Scroll.ReadMinPx {
			t.Errorf("%s: Scroll.ReadMaxPx must be >= ReadMinPx", name)
		}
		if p.Scroll.ScrollUpProb < 0 || p.Scroll.ScrollUpProb > 1 {
			t.Errorf("%s: Scroll.ScrollUpProb %.2f outside [0,1]", name, p.Scroll.ScrollUpProb)
		}
	}
}

func TestAllProfilesHaveValidKeyboardConfig(t *testing.T) {
	for _, name := range []ProfileName{ProfileCareful, ProfileModerate, ProfileAggressive} {
		p := GetProfile(name)
		if p.Keyboard.MinDelay <= 0 {
			t.Errorf("%s: Keyboard.MinDelay must be positive", name)
		}
		if p.Keyboard.MaxDelay <= p.Keyboard.MinDelay {
			t.Errorf("%s: Keyboard.MaxDelay must be > MinDelay", name)
		}
		if p.Keyboard.TypoProb < 0 || p.Keyboard.TypoProb > 1 {
			t.Errorf("%s: Keyboard.TypoProb %.2f outside [0,1]", name, p.Keyboard.TypoProb)
		}
	}
}

func TestCarefulAndAggressiveProfilesProduceDifferentDelays(t *testing.T) {
	careful := CarefulProfile()
	aggressive := AggressiveProfile()

	tc := NewTiming(careful.Timing)
	ta := NewTiming(aggressive.Timing)

	// Average 100 samples each and confirm careful is significantly slower.
	var sumC, sumA float64
	n := 100
	for i := 0; i < n; i++ {
		sumC += tc.Delay().Seconds()
		sumA += ta.Delay().Seconds()
	}
	avgC := sumC / float64(n)
	avgA := sumA / float64(n)

	if avgC <= avgA {
		t.Errorf("careful average delay (%.2fs) should exceed aggressive (%.2fs)", avgC, avgA)
	}
}
