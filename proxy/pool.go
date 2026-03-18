package proxy

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ErrNoProxy is returned when the pool has no usable proxy available.
var ErrNoProxy = errors.New("proxy: no proxy available")

// proxyEntry tracks runtime state for a single proxy inside the pool.
type proxyEntry struct {
	proxy       *Proxy
	health      ProxyHealth
	requestCount int
	bannedDomains map[string]struct{}
}

// isAvailable returns true when the entry is not on cooldown and alive.
func (e *proxyEntry) isAvailable(now time.Time) bool {
	if e.health.CooldownUntil.After(now) {
		return false
	}
	return true
}

// Pool manages a set of proxies sourced from one or more Provider instances.
// It tracks health, enforces cooldowns, and selects the best proxy on each Get.
type Pool struct {
	mu          sync.Mutex
	entries     []*proxyEntry
	rotation    RotationStrategy
	cooldown    time.Duration
	maxRequests int

	// nextIdx is used by PerRequest round-robin selection.
	nextIdx atomic.Int64

	// sessionMap and domainMap implement sticky assignment for PerSession and
	// PerDomain strategies.  Both are protected by mu.
	sessionMap map[string]*Proxy
	domainMap  map[string]*Proxy
}

// NewPool creates a Pool pre-loaded with proxies from the given providers.
// Providers are queried once during construction using a background context.
// If a provider fails it is logged and skipped.
func NewPool(providers ...Provider) *Pool {
	p := &Pool{
		rotation:    PerRequest,
		cooldown:    5 * time.Minute,
		maxRequests: 0, // unlimited
		sessionMap:  make(map[string]*Proxy),
		domainMap:   make(map[string]*Proxy),
	}

	ctx := context.Background()
	for _, prov := range providers {
		proxies, err := prov.Proxies(ctx)
		if err != nil {
			slog.Warn("proxy: provider returned error, skipping", "err", err)
			continue
		}
		for _, px := range proxies {
			p.entries = append(p.entries, &proxyEntry{
				proxy:         px,
				health:        ProxyHealth{Alive: true, Score: 1.0},
				bannedDomains: make(map[string]struct{}),
			})
		}
	}
	slog.Info("proxy: pool initialised", "count", len(p.entries))
	return p
}

// Get returns a proxy according to the configured rotation strategy.
// It blocks until a proxy is available or the context is cancelled.
//
// When all proxies are on cooldown it sleeps until the earliest cooldown
// expiry rather than busy-waiting.  If the pool is empty it returns
// ErrNoProxy immediately.
func (p *Pool) Get(ctx context.Context) (*Proxy, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		p.mu.Lock()
		now := time.Now()
		entry := p.selectEntry(now)
		if entry != nil {
			slog.Debug("proxy: selected", "proxy", entry.proxy.URL, "score", entry.health.Score)
			p.mu.Unlock()
			return entry.proxy, nil
		}

		// No proxy is available right now.  Find the earliest cooldown expiry
		// so we can sleep precisely until then instead of busy-waiting.
		earliest := p.earliestCooldownExpiry()
		p.mu.Unlock()

		if earliest.IsZero() {
			// Pool is empty — no point waiting.
			return nil, ErrNoProxy
		}

		waitDur := time.Until(earliest)
		if waitDur <= 0 {
			// Expiry is in the past; retry immediately.
			continue
		}

		timer := time.NewTimer(waitDur)
		select {
		case <-timer.C:
			// Cooldown expired; retry the selection loop.
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}
}

// GetForSession returns a sticky proxy for the given sessionID.  The same
// proxy is returned on every subsequent call with the same sessionID as long
// as the proxy remains available.  If the sticky proxy is no longer available
// a new one is assigned.
func (p *Pool) GetForSession(ctx context.Context, sessionID string) (*Proxy, error) {
	p.mu.Lock()
	if px, ok := p.sessionMap[sessionID]; ok {
		// Verify the sticky proxy is still available.
		e := p.findEntry(px)
		if e != nil && e.isAvailable(time.Now()) {
			p.mu.Unlock()
			return px, nil
		}
		// Sticky proxy gone — fall through to pick a new one.
		delete(p.sessionMap, sessionID)
	}
	p.mu.Unlock()

	px, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.sessionMap[sessionID] = px
	p.mu.Unlock()
	return px, nil
}

// GetForDomain returns a sticky proxy for the given domain.  The same proxy
// is returned on every subsequent call with the same domain as long as the
// proxy remains available.  If the sticky proxy is no longer available a new
// one is assigned.
func (p *Pool) GetForDomain(ctx context.Context, domain string) (*Proxy, error) {
	p.mu.Lock()
	if px, ok := p.domainMap[domain]; ok {
		e := p.findEntry(px)
		if e != nil && e.isAvailable(time.Now()) {
			p.mu.Unlock()
			return px, nil
		}
		delete(p.domainMap, domain)
	}
	p.mu.Unlock()

	px, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.domainMap[domain] = px
	p.mu.Unlock()
	return px, nil
}

// selectEntry picks an entry according to the configured rotation strategy.
// Caller must hold p.mu.
func (p *Pool) selectEntry(now time.Time) *proxyEntry {
	switch p.rotation {
	case PerRequest:
		return p.selectRoundRobin(now)
	case OnBlock:
		return p.selectBest(now)
	default:
		// PerSession and PerDomain sticky selection is handled by their
		// dedicated methods; fall back to round-robin for the base Get path.
		return p.selectRoundRobin(now)
	}
}

// selectRoundRobin cycles through available proxies in order.
// Caller must hold p.mu.
func (p *Pool) selectRoundRobin(now time.Time) *proxyEntry {
	n := len(p.entries)
	if n == 0 {
		return nil
	}
	// Collect available entries in stable index order.
	var available []*proxyEntry
	for _, e := range p.entries {
		if e.isAvailable(now) && !(p.maxRequests > 0 && e.requestCount >= p.maxRequests) {
			available = append(available, e)
		}
	}
	if len(available) == 0 {
		return nil
	}
	idx := int(p.nextIdx.Add(1)-1) % len(available)
	return available[idx]
}

// selectBest picks the available entry with the highest health score.
// Caller must hold p.mu.
func (p *Pool) selectBest(now time.Time) *proxyEntry {
	var best *proxyEntry
	for _, e := range p.entries {
		if !e.isAvailable(now) {
			continue
		}
		// M1: skip proxies that have reached the request limit.
		if p.maxRequests > 0 && e.requestCount >= p.maxRequests {
			continue
		}
		if best == nil || e.health.Score > best.health.Score {
			best = e
		}
	}
	return best
}

// earliestCooldownExpiry returns the soonest CooldownUntil among all entries
// that are currently on cooldown.  Returns zero time if the pool is empty or
// all proxies are available.
// Caller must hold p.mu.
func (p *Pool) earliestCooldownExpiry() time.Time {
	var earliest time.Time
	for _, e := range p.entries {
		cu := e.health.CooldownUntil
		if cu.IsZero() {
			continue
		}
		if earliest.IsZero() || cu.Before(earliest) {
			earliest = cu
		}
	}
	return earliest
}

// Release returns a proxy to the pool after use, updating its health score.
// success=true increments the success rate; success=false decreases the score.
// When maxRequests > 0 and the proxy has now reached that limit it is placed
// on cooldown automatically.
func (p *Pool) Release(px *Proxy, success bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	e := p.findEntry(px)
	if e == nil {
		return
	}
	e.requestCount++

	if success {
		// Nudge score upward toward 1.0.
		e.health.Score = min1(e.health.Score*0.9 + 0.1)
		slog.Debug("proxy: released (success)", "proxy", px.URL, "score", e.health.Score)
	} else {
		// Penalise score.
		e.health.Score = max0(e.health.Score * 0.7)
		slog.Debug("proxy: released (failure)", "proxy", px.URL, "score", e.health.Score)
	}

	// M1: auto-cooldown when the request limit is reached.
	if p.maxRequests > 0 && e.requestCount >= p.maxRequests {
		e.health.CooldownUntil = time.Now().Add(p.cooldown)
		slog.Debug("proxy: max requests reached, entering cooldown",
			"proxy", px.URL,
			"request_count", e.requestCount,
			"cooldown_until", e.health.CooldownUntil,
		)
	}
}

// Ban marks a proxy as banned for a specific domain and puts it on cooldown.
func (p *Pool) Ban(px *Proxy, domain string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	e := p.findEntry(px)
	if e == nil {
		return
	}
	e.bannedDomains[domain] = struct{}{}
	e.health.BanCount++
	e.health.CooldownUntil = time.Now().Add(p.cooldown)
	e.health.Score = max0(e.health.Score * 0.5)
	slog.Info("proxy: banned", "proxy", px.URL, "domain", domain, "cooldown_until", e.health.CooldownUntil)
}

// Health returns a snapshot of the ProxyHealth for the given proxy.
func (p *Pool) Health(px *Proxy) ProxyHealth {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.findEntry(px)
	if e == nil {
		return ProxyHealth{}
	}
	return e.health
}

// SetRotation changes the rotation strategy. Safe to call at any time.
func (p *Pool) SetRotation(strategy RotationStrategy) {
	p.mu.Lock()
	p.rotation = strategy
	p.mu.Unlock()
}

// SetCooldown changes the duration proxies remain on cooldown after being banned.
func (p *Pool) SetCooldown(d time.Duration) {
	p.mu.Lock()
	p.cooldown = d
	p.mu.Unlock()
}

// SetMaxRequests sets the maximum number of requests a proxy handles before
// automatic cooldown. 0 means unlimited.
func (p *Pool) SetMaxRequests(n int) {
	p.mu.Lock()
	p.maxRequests = n
	p.mu.Unlock()
}

// Len returns the total number of proxies in the pool (including on cooldown).
func (p *Pool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

// Close shuts down the pool and releases resources.
func (p *Pool) Close() error {
	slog.Debug("proxy: pool closed")
	return nil
}

// findEntry locates the pool entry for the given proxy by URL.
// Caller must hold p.mu.
func (p *Pool) findEntry(px *Proxy) *proxyEntry {
	for _, e := range p.entries {
		if e.proxy.URL == px.URL {
			return e
		}
	}
	return nil
}

func min1(v float64) float64 {
	if v > 1.0 {
		return 1.0
	}
	return v
}

func max0(v float64) float64 {
	if v < 0.0 {
		return 0.0
	}
	return v
}
