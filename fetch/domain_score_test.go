package fetch

import (
	"testing"
	"time"
)

func TestDomainScoreConvergesOnBlocked(t *testing.T) {
	scorer := NewDomainScorer(DefaultDomainScoreConfig())

	// Simulate 20 blocked static attempts
	for i := 0; i < 20; i++ {
		scorer.RecordStatic("blocked.com", true)
	}

	risk := scorer.Risk("blocked.com")
	if risk < 0.8 {
		t.Fatalf("Expected risk > 0.8 after 20 blocks, got %f", risk)
	}
}

func TestDomainScoreConvergesOnSuccess(t *testing.T) {
	scorer := NewDomainScorer(DefaultDomainScoreConfig())

	// Simulate 20 successful static attempts
	for i := 0; i < 20; i++ {
		scorer.RecordStatic("easy.com", false)
	}

	risk := scorer.Risk("easy.com")
	if risk > 0.15 {
		t.Fatalf("Expected risk < 0.15 after 20 successes, got %f", risk)
	}
}

func TestDomainScoreUnknownDomainReturnsPrior(t *testing.T) {
	scorer := NewDomainScorer(DefaultDomainScoreConfig())

	risk := scorer.Risk("unknown.com")
	// Prior mean = 1/(1+3) = 0.25
	expected := 0.25
	if risk < expected-0.01 || risk > expected+0.01 {
		t.Fatalf("Expected prior mean ~%f for unknown domain, got %f", expected, risk)
	}
}

func TestDomainScoreDecays(t *testing.T) {
	cfg := DefaultDomainScoreConfig()
	cfg.DecayHalflife = 100 * time.Millisecond // fast decay for testing
	scorer := NewDomainScorer(cfg)

	// Block heavily
	for i := 0; i < 20; i++ {
		scorer.RecordStatic("decay.com", true)
	}

	riskBefore := scorer.Risk("decay.com")

	// Wait for ~2 half-lives
	time.Sleep(200 * time.Millisecond)

	riskAfter := scorer.Risk("decay.com")

	if riskAfter >= riskBefore {
		t.Fatalf("Expected risk to decrease after decay, got before=%f after=%f", riskBefore, riskAfter)
	}
}

func TestDomainScoreRecommendEscalation(t *testing.T) {
	scorer := NewDomainScorer(DefaultDomainScoreConfig())

	// Block enough to trigger escalation (>0.6 threshold)
	for i := 0; i < 30; i++ {
		scorer.RecordStatic("hard.com", true)
	}

	action := scorer.Recommend("hard.com")
	if action != ActionBrowserDirect {
		t.Fatalf("Expected ActionBrowserDirect, got %d", action)
	}
}

func TestDomainScoreRecommendCautious(t *testing.T) {
	scorer := NewDomainScorer(DefaultDomainScoreConfig())

	// Mix of blocked and success to land in cautious range (0.3-0.6)
	for i := 0; i < 5; i++ {
		scorer.RecordStatic("mixed.com", true)
	}
	for i := 0; i < 5; i++ {
		scorer.RecordStatic("mixed.com", false)
	}

	action := scorer.Recommend("mixed.com")
	if action != ActionStaticCautious {
		t.Fatalf("Expected ActionStaticCautious, got %d (risk=%f)", action, scorer.Risk("mixed.com"))
	}
}

func TestSocialMediaConfigEscalatesFast(t *testing.T) {
	scorer := NewDomainScorer(SocialMediaScoreConfig())

	// Just 1 blocked attempt should push risk above caution threshold
	scorer.RecordStatic("instagram.com", true)
	risk := scorer.Risk("instagram.com")
	if risk < 0.6 {
		t.Fatalf("Expected risk > 0.6 after 1 block with social media prior, got %f", risk)
	}
}

func TestAsymmetricDecay(t *testing.T) {
	cfg := DefaultDomainScoreConfig()
	cfg.DecayHalflife = 100 * time.Millisecond
	scorer := NewDomainScorer(cfg)

	// Record equal blocks and successes
	for i := 0; i < 10; i++ {
		scorer.RecordStatic("asym.com", true)
		scorer.RecordStatic("asym.com", false)
	}
	riskBefore := scorer.Risk("asym.com")

	// Wait for ~2 success half-lives (but only ~0.5 block half-lives)
	time.Sleep(200 * time.Millisecond)

	riskAfter := scorer.Risk("asym.com")
	// Risk should INCREASE because successes decay faster than blocks
	if riskAfter <= riskBefore {
		t.Fatalf("Expected risk to increase with asymmetric decay, got before=%f after=%f", riskBefore, riskAfter)
	}
}

func TestDomainScoreNegativeAgeDoesNotAmplify(t *testing.T) {
	scorer := NewDomainScorer(DefaultDomainScoreConfig())
	scorer.RecordStatic("test.com", true)

	risk := scorer.Risk("test.com")
	// Risk should be between 0 and 1
	if risk < 0 || risk > 1.0 {
		t.Fatalf("risk out of bounds: %f", risk)
	}
}

func TestDomainScoreGetOrCreateReusesExisting(t *testing.T) {
	scorer := NewDomainScorer(DefaultDomainScoreConfig())

	// Record twice — should reuse the same DomainScore, not create new ones
	scorer.RecordStatic("test.com", true)
	scorer.RecordStatic("test.com", false)

	risk := scorer.Risk("test.com")
	// With 1 block + 1 success + prior(1,3): risk = (1+1)/(1+1+1+3) = 0.333
	if risk < 0.25 || risk > 0.45 {
		t.Fatalf("unexpected risk %f — getOrCreate may be creating duplicates", risk)
	}
}

func TestDomainScoreRecommendNormal(t *testing.T) {
	scorer := NewDomainScorer(DefaultDomainScoreConfig())

	// Mostly success
	for i := 0; i < 20; i++ {
		scorer.RecordStatic("easy.com", false)
	}

	action := scorer.Recommend("easy.com")
	if action != ActionStaticNormal {
		t.Fatalf("Expected ActionStaticNormal, got %d", action)
	}
}
