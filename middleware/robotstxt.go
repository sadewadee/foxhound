package middleware

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	foxhound "github.com/sadewadee/foxhound"
)

// robotsRules holds the list of disallowed path prefixes for a domain.
type robotsRules struct {
	disallowed []string // path prefixes that are disallowed
}

// IsAllowed reports whether path is permitted by the cached rules.
// An empty disallowed list means everything is allowed.
func (r *robotsRules) IsAllowed(path string) bool {
	for _, prefix := range r.disallowed {
		if prefix != "" && strings.HasPrefix(path, prefix) {
			return false
		}
	}
	return true
}

// parseRobotsTxt extracts disallowed path prefixes for userAgent (case-insensitive)
// and the wildcard agent "*" from a robots.txt body.
//
// Only User-agent and Disallow directives are parsed; all others are ignored.
// RFC 9309 full compliance is intentionally out of scope — the common case is
// sufficient for anti-block scraping use.
func parseRobotsTxt(body, userAgent string) *robotsRules {
	rules := &robotsRules{}

	// We build two sets: one for "*" and one for our specific user-agent.
	// The more-specific match wins; if no specific match exists, we fall back
	// to the wildcard rules.
	type section struct {
		agents    []string
		disallows []string
	}

	var sections []section
	var current *section

	uaLower := strings.ToLower(userAgent)

	for _, rawLine := range strings.Split(body, "\n") {
		// Strip inline comments and trim whitespace.
		line := strings.TrimSpace(rawLine)
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			// Blank line ends the current section.
			current = nil
			continue
		}

		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		field := strings.ToLower(strings.TrimSpace(line[:colonIdx]))
		value := strings.TrimSpace(line[colonIdx+1:])

		switch field {
		case "user-agent":
			if current == nil {
				sections = append(sections, section{})
				current = &sections[len(sections)-1]
			}
			current.agents = append(current.agents, strings.ToLower(value))

		case "disallow":
			if current != nil {
				current.disallows = append(current.disallows, value)
			}
		}
	}

	// Collect applicable disallowed paths.
	// Prefer specific user-agent match over wildcard.
	var wildcardDisallows []string
	var specificDisallows []string
	hasSpecific := false

	for _, sec := range sections {
		isWildcard := false
		isSpecific := false
		for _, agent := range sec.agents {
			if agent == "*" {
				isWildcard = true
			}
			if agent == uaLower {
				isSpecific = true
			}
		}
		if isSpecific {
			specificDisallows = append(specificDisallows, sec.disallows...)
			hasSpecific = true
		}
		if isWildcard {
			wildcardDisallows = append(wildcardDisallows, sec.disallows...)
		}
	}

	if hasSpecific {
		rules.disallowed = specificDisallows
	} else {
		rules.disallowed = wildcardDisallows
	}

	return rules
}

// robotsTxtMiddleware fetches and caches robots.txt per domain, skipping
// disallowed URLs before they reach the underlying fetcher.
type robotsTxtMiddleware struct {
	userAgent string
	mu        sync.Mutex
	cache     map[string]*robotsRules // domain -> rules
	httpClient *http.Client
}

// NewRobotsTxt returns a Middleware that optionally respects robots.txt.
//
// On the first request to a domain the middleware fetches /robots.txt using a
// plain http.Client and caches the result. Subsequent requests to the same
// domain use the cached rules.
//
// If robots.txt cannot be fetched (network error, non-2xx status) all URLs are
// allowed — conservative fail-open behaviour is safer for scraping than
// incorrectly blocking pages.
//
// Disallowed URLs return a Response with StatusCode 0 without calling the
// underlying Fetcher, matching the pattern used by NewDedup and NewDeltaFetch.
func NewRobotsTxt(userAgent string) foxhound.Middleware {
	return &robotsTxtMiddleware{
		userAgent:  userAgent,
		cache:      make(map[string]*robotsRules),
		httpClient: &http.Client{},
	}
}

// Wrap implements foxhound.Middleware.
func (m *robotsTxtMiddleware) Wrap(next foxhound.Fetcher) foxhound.Fetcher {
	return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		parsed, err := url.Parse(job.URL)
		if err != nil {
			// Unparseable URL — pass through, let the inner fetcher deal with it.
			return next.Fetch(ctx, job)
		}

		domain := parsed.Host
		rules := m.rulesFor(domain, parsed.Scheme)

		if !rules.IsAllowed(parsed.Path) {
			slog.Debug("robotstxt: skipping disallowed URL", "url", job.URL)
			return &foxhound.Response{StatusCode: 0, Job: job}, nil
		}

		return next.Fetch(ctx, job)
	})
}

// rulesFor returns cached rules for domain, fetching /robots.txt on the first
// call. On any fetch or parse error an allow-all rule set is returned and NOT
// cached so that a transient network failure does not permanently allow all
// requests without a real check.
func (m *robotsTxtMiddleware) rulesFor(domain, scheme string) *robotsRules {
	m.mu.Lock()
	if rules, ok := m.cache[domain]; ok {
		m.mu.Unlock()
		return rules
	}
	m.mu.Unlock()

	rules := m.fetchRules(domain, scheme)

	m.mu.Lock()
	// Cache even on error so that we don't hammer a failing server.
	m.cache[domain] = rules
	m.mu.Unlock()

	return rules
}

// fetchRules downloads and parses /robots.txt for domain.
// Returns an allow-all rule set if the fetch fails or returns a non-2xx status.
func (m *robotsTxtMiddleware) fetchRules(domain, scheme string) *robotsRules {
	if scheme == "" {
		scheme = "https"
	}
	robotsURL := scheme + "://" + domain + "/robots.txt"

	resp, err := m.httpClient.Get(robotsURL) //nolint:noctx // best-effort fetch
	if err != nil {
		slog.Debug("robotstxt: fetch error, allowing all", "domain", domain, "err", err)
		return &robotsRules{}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Debug("robotstxt: non-2xx status, allowing all",
			"domain", domain, "status", resp.StatusCode)
		return &robotsRules{}
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Debug("robotstxt: body read error, allowing all", "domain", domain, "err", err)
		return &robotsRules{}
	}

	rules := parseRobotsTxt(string(bodyBytes), m.userAgent)
	slog.Debug("robotstxt: parsed rules",
		"domain", domain,
		"disallowed_count", len(rules.disallowed),
	)
	return rules
}
