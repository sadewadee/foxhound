package fetch

import (
	"math"
	"sync"
	"time"
)

// DomainScore tracks static vs browser fetch outcomes for a single domain
// using a Bayesian Beta distribution model.
//
// Risk score formula (Beta posterior mean):
//
//	risk = (blocked + priorAlpha) / (blocked + success + priorAlpha + priorBeta)
//
// With prior Beta(1, 3): assumes static usually works (75% prior success rate).
// As blocked events accumulate, risk rises toward 1.0.
//
// Time decay ensures old outcomes fade:
//
//	effective_count = count * exp(-age / halflife)
type DomainScore struct {
	mu             sync.Mutex
	staticSuccess  float64
	staticBlocked  float64
	browserSuccess float64
	browserBlocked float64
	lastUpdate     time.Time
}

// DomainScoreConfig controls the learning behavior.
type DomainScoreConfig struct {
	// PriorAlpha is the Beta distribution prior for blocked (default 1.0).
	PriorAlpha float64
	// PriorBeta is the Beta distribution prior for success (default 3.0).
	PriorBeta float64
	// EscalationThreshold: if risk > this, skip static entirely (default 0.6).
	EscalationThreshold float64
	// CautionThreshold: if risk > this, use static with shorter timeout (default 0.3).
	CautionThreshold float64
	// DecayHalflife: old outcomes decay with this half-life (default 1h).
	DecayHalflife time.Duration
}

// DefaultDomainScoreConfig returns sensible defaults.
func DefaultDomainScoreConfig() DomainScoreConfig {
	return DomainScoreConfig{
		PriorAlpha:          1.0,
		PriorBeta:           3.0,
		EscalationThreshold: 0.6,
		CautionThreshold:    0.3,
		DecayHalflife:       1 * time.Hour,
	}
}

// SocialMediaScoreConfig returns a configuration tuned for social media targets
// where static fetches are almost always blocked.
// Prior Beta(3,1) = 75% prior block rate — escalates after just 1 blocked attempt.
func SocialMediaScoreConfig() DomainScoreConfig {
	return DomainScoreConfig{
		PriorAlpha:          3.0,  // high prior on blocked
		PriorBeta:           1.0,  // low prior on success
		EscalationThreshold: 0.6,
		CautionThreshold:    0.3,
		DecayHalflife:       24 * time.Hour, // blocks persist longer for social media
	}
}

// DomainScoreAction indicates what the SmartFetcher should do.
type DomainScoreAction int

const (
	// ActionStaticNormal means use static with normal timeout.
	ActionStaticNormal DomainScoreAction = iota
	// ActionStaticCautious means use static but with a shorter timeout (fail-fast).
	ActionStaticCautious
	// ActionBrowserDirect means skip static and go directly to browser.
	ActionBrowserDirect
)

// DomainScorer manages per-domain risk scores.
type DomainScorer struct {
	config DomainScoreConfig
	scores sync.Map // map[string]*DomainScore
}

// NewDomainScorer creates a scorer with the given configuration.
func NewDomainScorer(cfg DomainScoreConfig) *DomainScorer {
	if cfg.PriorAlpha <= 0 {
		cfg.PriorAlpha = 1.0
	}
	if cfg.PriorBeta <= 0 {
		cfg.PriorBeta = 3.0
	}
	if cfg.EscalationThreshold <= 0 {
		cfg.EscalationThreshold = 0.6
	}
	if cfg.CautionThreshold <= 0 {
		cfg.CautionThreshold = 0.3
	}
	if cfg.DecayHalflife <= 0 {
		cfg.DecayHalflife = 1 * time.Hour
	}
	return &DomainScorer{config: cfg}
}

// RecordStatic records the outcome of a static fetch for the given domain.
func (ds *DomainScorer) RecordStatic(domain string, blocked bool) {
	score := ds.getOrCreate(domain)
	score.mu.Lock()
	defer score.mu.Unlock()

	if blocked {
		score.staticBlocked++
	} else {
		score.staticSuccess++
	}
	score.lastUpdate = time.Now()
}

// RecordBrowser records the outcome of a browser fetch for the given domain.
func (ds *DomainScorer) RecordBrowser(domain string, blocked bool) {
	score := ds.getOrCreate(domain)
	score.mu.Lock()
	defer score.mu.Unlock()

	if blocked {
		score.browserBlocked++
	} else {
		score.browserSuccess++
	}
	score.lastUpdate = time.Now()
}

// Risk returns the current risk score for a domain (probability that static will be blocked).
// Uses Bayesian Beta posterior mean with time decay.
func (ds *DomainScorer) Risk(domain string) float64 {
	val, ok := ds.scores.Load(domain)
	if !ok {
		// No data — return prior mean
		return ds.config.PriorAlpha / (ds.config.PriorAlpha + ds.config.PriorBeta)
	}

	score := val.(*DomainScore)
	score.mu.Lock()
	defer score.mu.Unlock()

	age := time.Since(score.lastUpdate)
	successDecay := ds.decayFactor(age)
	// Blocks decay 4x slower — protection measures are usually persistent
	blockDecay := ds.decayFactor(age / 4)

	effectiveBlocked := score.staticBlocked * blockDecay
	effectiveSuccess := score.staticSuccess * successDecay

	// Beta posterior mean
	return (effectiveBlocked + ds.config.PriorAlpha) /
		(effectiveBlocked + effectiveSuccess + ds.config.PriorAlpha + ds.config.PriorBeta)
}

// Recommend returns the recommended fetch action for a domain.
func (ds *DomainScorer) Recommend(domain string) DomainScoreAction {
	risk := ds.Risk(domain)
	if risk > ds.config.EscalationThreshold {
		return ActionBrowserDirect
	}
	if risk > ds.config.CautionThreshold {
		return ActionStaticCautious
	}
	return ActionStaticNormal
}

// decayFactor returns the exponential decay multiplier for the given age.
// Formula: exp(-age * ln(2) / halflife)
func (ds *DomainScorer) decayFactor(age time.Duration) float64 {
	if ds.config.DecayHalflife <= 0 {
		return 1.0
	}
	return math.Exp(-float64(age) * math.Ln2 / float64(ds.config.DecayHalflife))
}

func (ds *DomainScorer) getOrCreate(domain string) *DomainScore {
	val, _ := ds.scores.LoadOrStore(domain, &DomainScore{
		lastUpdate: time.Now(),
	})
	return val.(*DomainScore)
}
