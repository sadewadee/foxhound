package behavior

import (
	"strings"
	"testing"
	"time"
)

func TestNewNavigationReturnsNonNil(t *testing.T) {
	n := NewNavigation(DefaultNavigationConfig())
	if n == nil {
		t.Fatal("NewNavigation returned nil")
	}
}

func TestSessionPagesInRange(t *testing.T) {
	n := NewNavigation(DefaultNavigationConfig())
	cfg := DefaultNavigationConfig()
	for i := 0; i < 200; i++ {
		pages := n.SessionPages()
		if pages < cfg.PagesPerSession.Min || pages > cfg.PagesPerSession.Max {
			t.Errorf("SessionPages() = %d, outside [%d, %d]",
				pages, cfg.PagesPerSession.Min, cfg.PagesPerSession.Max)
		}
	}
}

func TestSessionDurationInRange(t *testing.T) {
	n := NewNavigation(DefaultNavigationConfig())
	cfg := DefaultNavigationConfig()
	for i := 0; i < 200; i++ {
		dur := n.SessionDuration()
		if dur < cfg.SessionDuration.Min || dur > cfg.SessionDuration.Max {
			t.Errorf("SessionDuration() = %v, outside [%v, %v]",
				dur, cfg.SessionDuration.Min, cfg.SessionDuration.Max)
		}
	}
}

func TestSessionGapInRange(t *testing.T) {
	n := NewNavigation(DefaultNavigationConfig())
	cfg := DefaultNavigationConfig()
	for i := 0; i < 200; i++ {
		gap := n.SessionGap()
		if gap < cfg.SessionGap.Min || gap > cfg.SessionGap.Max {
			t.Errorf("SessionGap() = %v, outside [%v, %v]",
				gap, cfg.SessionGap.Min, cfg.SessionGap.Max)
		}
	}
}

// TestShouldGoBackProbability checks that back navigation occurs with
// roughly the configured probability (BackButtonProb=0.3).
func TestShouldGoBackProbability(t *testing.T) {
	cfg := DefaultNavigationConfig()
	n := NewNavigation(cfg)

	trueCount := 0
	trials := 3000
	for i := 0; i < trials; i++ {
		if n.ShouldGoBack() {
			trueCount++
		}
	}

	ratio := float64(trueCount) / float64(trials)
	expected := cfg.BackButtonProb
	if ratio < expected-0.06 || ratio > expected+0.06 {
		t.Errorf("ShouldGoBack ratio %.3f outside [%.3f, %.3f]",
			ratio, expected-0.06, expected+0.06)
	}
}

func TestShouldVisitUselessProbability(t *testing.T) {
	cfg := DefaultNavigationConfig()
	n := NewNavigation(cfg)

	trueCount := 0
	trials := 3000
	for i := 0; i < trials; i++ {
		if n.ShouldVisitUseless() {
			trueCount++
		}
	}

	ratio := float64(trueCount) / float64(trials)
	expected := cfg.UselessPageProb
	if ratio < expected-0.04 || ratio > expected+0.04 {
		t.Errorf("ShouldVisitUseless ratio %.3f outside [%.3f, %.3f]",
			ratio, expected-0.04, expected+0.04)
	}
}

func TestShouldSearchProbability(t *testing.T) {
	cfg := DefaultNavigationConfig()
	n := NewNavigation(cfg)

	trueCount := 0
	trials := 3000
	for i := 0; i < trials; i++ {
		if n.ShouldSearch() {
			trueCount++
		}
	}

	ratio := float64(trueCount) / float64(trials)
	expected := cfg.SearchProb
	if ratio < expected-0.05 || ratio > expected+0.05 {
		t.Errorf("ShouldSearch ratio %.3f outside [%.3f, %.3f]",
			ratio, expected-0.05, expected+0.05)
	}
}

func TestRefererIsNonEmpty(t *testing.T) {
	n := NewNavigation(DefaultNavigationConfig())
	ref := n.Referer("example.com")
	if ref == "" {
		t.Error("Referer returned empty string")
	}
}

func TestRefererIsValidURL(t *testing.T) {
	n := NewNavigation(DefaultNavigationConfig())
	ref := n.Referer("example.com")
	if !strings.HasPrefix(ref, "http://") && !strings.HasPrefix(ref, "https://") {
		t.Errorf("Referer %q does not start with http:// or https://", ref)
	}
}

func TestDefaultNavigationConfigValues(t *testing.T) {
	cfg := DefaultNavigationConfig()

	if cfg.PagesPerSession.Min <= 0 {
		t.Error("PagesPerSession.Min must be positive")
	}
	if cfg.PagesPerSession.Max < cfg.PagesPerSession.Min {
		t.Error("PagesPerSession.Max must be >= Min")
	}
	if cfg.SessionDuration.Min <= 0 {
		t.Error("SessionDuration.Min must be positive")
	}
	if cfg.SessionGap.Min <= 0 {
		t.Error("SessionGap.Min must be positive")
	}
	if cfg.BackButtonProb < 0 || cfg.BackButtonProb > 1 {
		t.Errorf("BackButtonProb %.2f outside [0,1]", cfg.BackButtonProb)
	}
	if cfg.UselessPageProb < 0 || cfg.UselessPageProb > 1 {
		t.Errorf("UselessPageProb %.2f outside [0,1]", cfg.UselessPageProb)
	}
	if cfg.SearchProb < 0 || cfg.SearchProb > 1 {
		t.Errorf("SearchProb %.2f outside [0,1]", cfg.SearchProb)
	}
}

func TestDurationRangeSanity(t *testing.T) {
	cfg := DefaultNavigationConfig()
	if cfg.SessionDuration.Max < cfg.SessionDuration.Min {
		t.Error("SessionDuration Max < Min")
	}
	if cfg.SessionGap.Max < cfg.SessionGap.Min {
		t.Error("SessionGap Max < Min")
	}
}

// Verify documented default ranges match expected values from architecture.
func TestDefaultNavigationDocumentedRanges(t *testing.T) {
	cfg := DefaultNavigationConfig()

	// Architecture: 10-30 pages per session.
	if cfg.PagesPerSession.Min != 10 || cfg.PagesPerSession.Max != 30 {
		t.Errorf("PagesPerSession = [%d,%d], want [10,30]",
			cfg.PagesPerSession.Min, cfg.PagesPerSession.Max)
	}

	// Architecture: session 10-60 min.
	if cfg.SessionDuration.Min != 10*time.Minute || cfg.SessionDuration.Max != 60*time.Minute {
		t.Errorf("SessionDuration = [%v,%v], want [10m,60m]",
			cfg.SessionDuration.Min, cfg.SessionDuration.Max)
	}

	// Architecture: gap 5-30 min.
	if cfg.SessionGap.Min != 5*time.Minute || cfg.SessionGap.Max != 30*time.Minute {
		t.Errorf("SessionGap = [%v,%v], want [5m,30m]",
			cfg.SessionGap.Min, cfg.SessionGap.Max)
	}
}
