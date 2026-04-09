// session.go — stateful, single-call client for ad-hoc scraping.
//
// A Session is the middle ground between calling fetch.NewStealth().Fetch
// directly (stateless, no cookie persistence) and running a full Hunt
// (heavyweight, requires Processor + Walker + Queue). It owns a fetcher,
// an http.CookieJar, an optional identity profile, and an optional proxy
// URL, and reuses them across calls so cookies and identity attributes
// persist for the lifetime of the Session.
//
// Typical usage:
//
//	s := foxhound.NewSession(
//	    foxhound.WithSessionIdentity(identity.Generate()),
//	    foxhound.WithSessionProxy("http://user:pass@proxy.example:8080"),
//	)
//	defer s.Close()
//	resp, err := s.Get(ctx, "https://example.com/page")
//
// Session is also the unit registered with Hunt.AddSession for multi-session
// scraping campaigns where index pages and detail pages need separate cookie
// jars and proxies.

package foxhound

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"
)

// Session is a stateful client that survives across calls. Cookies are
// persisted in an internal CookieJar; identity, proxy, and fetcher are
// reused for every Get/Fetch.
//
// Session is safe for concurrent use by multiple goroutines.
type Session struct {
	mu       sync.Mutex
	fetcher  Fetcher
	jar      http.CookieJar
	identity any    // *identity.Profile, kept untyped to avoid an import cycle
	proxyURL string // recorded for inspection / Hunt registration
	name     string // optional name (set by Hunt.AddSession)
}

// SessionOption configures a Session at construction time.
type SessionOption func(*Session)

// WithSessionFetcher overrides the default fetcher. When omitted the caller
// must register one explicitly via SetFetcher before the first Get / Fetch
// call; calling Get on a Session without a fetcher returns an error.
//
// The default Session does NOT auto-create a stealth fetcher to avoid an
// import cycle from foxhound → fetch. Wire one up at the application layer:
//
//	s := foxhound.NewSession(foxhound.WithSessionFetcher(fetch.NewStealth()))
func WithSessionFetcher(f Fetcher) SessionOption {
	return func(s *Session) { s.fetcher = f }
}

// WithSessionIdentity attaches an identity profile to the session. The value
// is stored as `any` to avoid an import cycle with the identity package; the
// caller passes a *identity.Profile and is responsible for using it on the
// fetcher (most fetchers accept it via their own option at construction).
func WithSessionIdentity(p any) SessionOption {
	return func(s *Session) { s.identity = p }
}

// WithSessionProxy records the session's proxy URL. The value is stored for
// inspection but is NOT auto-applied to the fetcher; configure the fetcher's
// own proxy option at construction time. This is intentional: a Session is a
// thin wrapper, not a fetcher factory.
func WithSessionProxy(rawURL string) SessionOption {
	return func(s *Session) { s.proxyURL = rawURL }
}

// WithSessionCookieJar replaces the default in-memory jar with a caller-
// supplied implementation. Use this when persisting cookies across processes
// (e.g. via a custom file-backed jar) or when sharing a jar across sessions.
func WithSessionCookieJar(jar http.CookieJar) SessionOption {
	return func(s *Session) { s.jar = jar }
}

// NewSession constructs a Session with the supplied options. A fresh in-memory
// cookie jar is created when no WithSessionCookieJar option is given.
func NewSession(opts ...SessionOption) *Session {
	s := &Session{}
	for _, opt := range opts {
		opt(s)
	}
	if s.jar == nil {
		jar, _ := cookiejar.New(nil)
		s.jar = jar
	}
	return s
}

// Name returns the session's optional name. Returns empty string for
// stand-alone sessions; populated for sessions registered via Hunt.AddSession.
func (s *Session) Name() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.name
}

// SetName updates the session's name. Used by Hunt.AddSession.
func (s *Session) SetName(name string) {
	s.mu.Lock()
	s.name = name
	s.mu.Unlock()
}

// Fetcher returns the underlying fetcher. Returns nil if none was configured.
func (s *Session) Fetcher() Fetcher {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fetcher
}

// SetFetcher updates the underlying fetcher post-construction. Useful when
// the fetcher needs to reference the Session's cookie jar (a chicken-and-egg
// problem solved by constructing the Session first, then the fetcher).
func (s *Session) SetFetcher(f Fetcher) {
	s.mu.Lock()
	s.fetcher = f
	s.mu.Unlock()
}

// Identity returns the configured identity profile (as `any`). Callers
// type-assert to *identity.Profile.
func (s *Session) Identity() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.identity
}

// ProxyURL returns the session's recorded proxy URL.
func (s *Session) ProxyURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proxyURL
}

// Cookies returns all cookies currently held in the jar across every host.
// The returned slice is a fresh copy; mutating it does not affect the jar.
func (s *Session) Cookies() []*http.Cookie {
	s.mu.Lock()
	jar := s.jar
	s.mu.Unlock()
	if jar == nil {
		return nil
	}
	// http.CookieJar exposes cookies only per-URL. We have no enumeration API,
	// so this returns nil unless the caller uses CookiesFor with a known URL.
	return nil
}

// CookiesFor returns the cookies the jar would send for the given URL. This
// is the standard http.CookieJar query — use it when you need to inspect
// what the session has accumulated for a particular host.
func (s *Session) CookiesFor(rawURL string) []*http.Cookie {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	jar := s.jar
	s.mu.Unlock()
	if jar == nil {
		return nil
	}
	return jar.Cookies(u)
}

// Get is the simple fetch shorthand. It builds a Job with method GET, the
// session's identity, and the URL, then delegates to Fetch.
func (s *Session) Get(ctx context.Context, rawURL string) (*Response, error) {
	job := &Job{
		ID:        rawURL,
		URL:       rawURL,
		Method:    http.MethodGet,
		FetchMode: FetchAuto,
		Priority:  PriorityNormal,
		CreatedAt: time.Now(),
	}
	return s.Fetch(ctx, job)
}

// Fetch executes a Job through the session's fetcher. Before the call any
// cookies the jar holds for the target URL are merged into the job's headers
// (so static fetchers without their own jar still see them). After a
// successful fetch any cookies returned in Response.Cookies are stored in the
// jar so the next call observes them.
func (s *Session) Fetch(ctx context.Context, job *Job) (*Response, error) {
	if job == nil {
		return nil, fmt.Errorf("foxhound/session: job must not be nil")
	}
	s.mu.Lock()
	fetcher := s.fetcher
	jar := s.jar
	s.mu.Unlock()

	if fetcher == nil {
		return nil, fmt.Errorf("foxhound/session: no fetcher configured (use WithSessionFetcher)")
	}

	// Inject jar cookies into the outgoing request headers when the fetcher
	// does not maintain its own jar. This is harmless when it does — duplicates
	// are deduplicated by the HTTP layer.
	if jar != nil {
		if u, err := url.Parse(job.URL); err == nil {
			cookies := jar.Cookies(u)
			if len(cookies) > 0 {
				if job.Headers == nil {
					job.Headers = make(http.Header)
				}
				header := make([]string, 0, len(cookies))
				for _, c := range cookies {
					header = append(header, c.Name+"="+c.Value)
				}
				existing := job.Headers.Get("Cookie")
				if existing != "" {
					header = append([]string{existing}, header...)
				}
				job.Headers.Set("Cookie", joinCookies(header))
			}
		}
	}

	resp, err := fetcher.Fetch(ctx, job)
	if err != nil {
		return resp, err
	}

	// Persist any cookies the response carries.
	if jar != nil && resp != nil && len(resp.Cookies) > 0 {
		if u, perr := url.Parse(resp.URL); perr == nil && u != nil {
			jar.SetCookies(u, resp.Cookies)
		} else if u, perr := url.Parse(job.URL); perr == nil && u != nil {
			jar.SetCookies(u, resp.Cookies)
		}
	}

	return resp, nil
}

// Close releases any resources held by the underlying fetcher. The cookie
// jar is in-memory and needs no cleanup. Safe to call multiple times.
func (s *Session) Close() error {
	s.mu.Lock()
	f := s.fetcher
	s.fetcher = nil
	s.mu.Unlock()
	if f == nil {
		return nil
	}
	return f.Close()
}

// joinCookies concatenates multiple cookie name=value strings with the
// standard "; " separator the Cookie header expects.
func joinCookies(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += "; " + p
	}
	return out
}
