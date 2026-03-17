package proxy

import (
	"context"
	"errors"
	"log/slog"
	"sync"
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
}

// NewPool creates a Pool pre-loaded with proxies from the given providers.
// Providers are queried once during construction using a background context.
// If a provider fails it is logged and skipped.
func NewPool(providers ...Provider) *Pool {
	p := &Pool{
		rotation:    PerRequest,
		cooldown:    5 * time.Minute,
		maxRequests: 0, // unlimited
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

// Get returns the proxy with the highest score that is not on cooldown.
// It blocks until a proxy is available or the context is cancelled.
func (p *Pool) Get(ctx context.Context) (*Proxy, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		p.mu.Lock()
		best := p.selectBest(time.Now())
		p.mu.Unlock()

		if best != nil {
			slog.Debug("proxy: selected", "proxy", best.proxy.URL, "score", best.health.Score)
			return best.proxy, nil
		}

		// No proxy available; back off briefly and retry.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// selectBest picks the available entry with the highest health score.
// Caller must hold p.mu.
func (p *Pool) selectBest(now time.Time) *proxyEntry {
	var best *proxyEntry
	for _, e := range p.entries {
		if !e.isAvailable(now) {
			continue
		}
		if best == nil || e.health.Score > best.health.Score {
			best = e
		}
	}
	return best
}

// Release returns a proxy to the pool after use, updating its health score.
// success=true increments the success rate; success=false decreases the score.
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
