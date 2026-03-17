package behavior

import (
	"fmt"
	"math/rand/v2"
	"time"
)

// Range specifies an inclusive integer interval.
type Range struct {
	Min, Max int
}

// DurationRange specifies an inclusive duration interval.
type DurationRange struct {
	Min, Max time.Duration
}

// NavigationConfig configures browsing-pattern generation.
type NavigationConfig struct {
	// PagesPerSession is how many pages a single session visits.
	PagesPerSession Range
	// SessionDuration is how long a session lasts.
	SessionDuration DurationRange
	// SessionGap is the idle gap between consecutive sessions.
	SessionGap DurationRange
	// BackButtonProb is the probability that the walker hits back.
	BackButtonProb float64
	// UselessPageProb is the probability of visiting a low-value page
	// (about, contact, FAQ) to add realism.
	UselessPageProb float64
	// SearchProb is the probability of using the site's search feature.
	SearchProb float64
}

// DefaultNavigationConfig returns the architecture-documented defaults.
func DefaultNavigationConfig() NavigationConfig {
	return NavigationConfig{
		PagesPerSession: Range{Min: 10, Max: 30},
		SessionDuration: DurationRange{Min: 10 * time.Minute, Max: 60 * time.Minute},
		SessionGap:      DurationRange{Min: 5 * time.Minute, Max: 30 * time.Minute},
		BackButtonProb:  0.30,
		UselessPageProb: 0.10,
		SearchProb:      0.20,
	}
}

// Navigation generates human-like browsing patterns.
type Navigation struct {
	config NavigationConfig
}

// NewNavigation creates a Navigation with the supplied configuration.
func NewNavigation(cfg NavigationConfig) *Navigation {
	return &Navigation{config: cfg}
}

// ShouldGoBack returns true when the walker should navigate backward.
func (n *Navigation) ShouldGoBack() bool {
	return rand.Float64() < n.config.BackButtonProb
}

// ShouldVisitUseless returns true when the walker should visit a low-value
// page to simulate realistic browsing patterns.
func (n *Navigation) ShouldVisitUseless() bool {
	return rand.Float64() < n.config.UselessPageProb
}

// ShouldSearch returns true when the walker should use the site search.
func (n *Navigation) ShouldSearch() bool {
	return rand.Float64() < n.config.SearchProb
}

// SessionPages returns how many pages the current session should visit,
// drawn uniformly from [PagesPerSession.Min, PagesPerSession.Max].
func (n *Navigation) SessionPages() int {
	span := n.config.PagesPerSession.Max - n.config.PagesPerSession.Min
	if span <= 0 {
		return n.config.PagesPerSession.Min
	}
	return n.config.PagesPerSession.Min + rand.IntN(span+1)
}

// SessionDuration returns how long the current session should last,
// drawn uniformly from [SessionDuration.Min, SessionDuration.Max].
func (n *Navigation) SessionDuration() time.Duration {
	return randDurationRange(n.config.SessionDuration)
}

// SessionGap returns the idle time before the next session begins,
// drawn uniformly from [SessionGap.Min, SessionGap.Max].
func (n *Navigation) SessionGap() time.Duration {
	return randDurationRange(n.config.SessionGap)
}

// Referer returns a plausible HTTP Referer header value for a walker arriving
// at targetDomain from a search engine or direct navigation.
//
// The returned string is always an absolute HTTP/HTTPS URL.
func (n *Navigation) Referer(targetDomain string) string {
	entries := []string{
		"https://www.google.com/search?q=" + targetDomain,
		"https://www.bing.com/search?q=" + targetDomain,
		"https://duckduckgo.com/?q=" + targetDomain,
		fmt.Sprintf("https://%s/", targetDomain),
	}
	return entries[rand.IntN(len(entries))]
}

// randDurationRange returns a uniform sample from [r.Min, r.Max].
func randDurationRange(r DurationRange) time.Duration {
	span := r.Max - r.Min
	if span <= 0 {
		return r.Min
	}
	return r.Min + time.Duration(rand.Int64N(int64(span+1)))
}
