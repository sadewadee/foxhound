package proxy_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/foxhound-scraper/foxhound/proxy"
)

// --- proxy.Parse tests ---

func TestParseFullHTTP(t *testing.T) {
	p, err := proxy.Parse("http://alice:secret@192.168.1.1:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Protocol != "http" {
		t.Errorf("protocol: got %q, want %q", p.Protocol, "http")
	}
	if p.Host != "192.168.1.1" {
		t.Errorf("host: got %q, want %q", p.Host, "192.168.1.1")
	}
	if p.Port != "8080" {
		t.Errorf("port: got %q, want %q", p.Port, "8080")
	}
	if p.Username != "alice" {
		t.Errorf("username: got %q, want %q", p.Username, "alice")
	}
	if p.Password != "secret" {
		t.Errorf("password: got %q, want %q", p.Password, "secret")
	}
}

func TestParseSOCKS5(t *testing.T) {
	p, err := proxy.Parse("socks5://bob:pass@10.0.0.1:1080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Protocol != "socks5" {
		t.Errorf("protocol: got %q, want %q", p.Protocol, "socks5")
	}
	if p.Host != "10.0.0.1" {
		t.Errorf("host: got %q, want %q", p.Host, "10.0.0.1")
	}
}

func TestParseHostPort(t *testing.T) {
	p, err := proxy.Parse("10.0.0.2:3128")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Protocol != "http" {
		t.Errorf("bare host:port should default to http, got %q", p.Protocol)
	}
	if p.Host != "10.0.0.2" {
		t.Errorf("host: got %q, want %q", p.Host, "10.0.0.2")
	}
	if p.Port != "3128" {
		t.Errorf("port: got %q, want %q", p.Port, "3128")
	}
	if p.Username != "" || p.Password != "" {
		t.Errorf("bare host:port should have no credentials")
	}
}

func TestParseInvalid(t *testing.T) {
	_, err := proxy.Parse("not-a-proxy")
	if err == nil {
		t.Fatal("expected error for invalid proxy URL, got nil")
	}
}

// --- StaticProvider tests ---

func TestStaticProviderReturnsProxies(t *testing.T) {
	urls := []string{
		"http://user:pass@1.2.3.4:8080",
		"socks5://5.6.7.8:1080",
	}
	provider := proxy.Static(urls)
	proxies, err := provider.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proxies) != 2 {
		t.Fatalf("expected 2 proxies, got %d", len(proxies))
	}
}

func TestStaticProviderSkipsInvalidURLs(t *testing.T) {
	urls := []string{
		"http://1.2.3.4:8080",
		"not-valid",
	}
	provider := proxy.Static(urls)
	proxies, err := provider.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Invalid URL is skipped; only valid one returned.
	if len(proxies) != 1 {
		t.Fatalf("expected 1 proxy after skipping invalid, got %d", len(proxies))
	}
}

// --- Pool tests ---

func TestPoolGetReturnsProxy(t *testing.T) {
	provider := proxy.Static([]string{"http://1.2.3.4:8080", "http://5.6.7.8:8080"})
	pool := proxy.NewPool(provider)
	defer pool.Close()

	p, err := pool.Get(context.Background())
	if err != nil {
		t.Fatalf("unexpected error from Get: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
}

func TestPoolGetFailsWhenEmpty(t *testing.T) {
	pool := proxy.NewPool()
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := pool.Get(ctx)
	if err == nil {
		t.Fatal("expected error from empty pool, got nil")
	}
}

func TestPoolReleaseSuccess(t *testing.T) {
	provider := proxy.Static([]string{"http://1.2.3.4:8080"})
	pool := proxy.NewPool(provider)
	defer pool.Close()

	p, err := pool.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// Release as success — should not panic.
	pool.Release(p, true)
}

func TestPoolReleaseFailureDegradesScore(t *testing.T) {
	provider := proxy.Static([]string{"http://1.2.3.4:8080"})
	pool := proxy.NewPool(provider)
	defer pool.Close()

	p, _ := pool.Get(context.Background())
	pool.Release(p, false)

	health := pool.Health(p)
	if health.Score >= 1.0 {
		t.Errorf("expected score < 1.0 after failure, got %v", health.Score)
	}
}

func TestPoolBanRemovesProxyFromRotation(t *testing.T) {
	provider := proxy.Static([]string{"http://only-proxy:8080"})
	pool := proxy.NewPool(provider)
	defer pool.Close()

	p, _ := pool.Get(context.Background())
	pool.Ban(p, "example.com")

	// The single proxy is now on cooldown; Get with short timeout should fail.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := pool.Get(ctx)
	if err == nil {
		t.Fatal("expected error after banning only proxy")
	}
}

func TestPoolLen(t *testing.T) {
	provider := proxy.Static([]string{"http://1.1.1.1:80", "http://2.2.2.2:80"})
	pool := proxy.NewPool(provider)
	defer pool.Close()

	if got := pool.Len(); got != 2 {
		t.Errorf("Len: got %d, want 2", got)
	}
}

func TestPoolSetRotation(t *testing.T) {
	pool := proxy.NewPool()
	// Should not panic.
	pool.SetRotation(proxy.PerDomain)
	pool.SetRotation(proxy.OnBlock)
}

func TestPoolSetCooldown(t *testing.T) {
	pool := proxy.NewPool()
	pool.SetCooldown(5 * time.Minute)
}

func TestPoolSetMaxRequests(t *testing.T) {
	pool := proxy.NewPool()
	pool.SetMaxRequests(100)
}

// --- HealthChecker tests ---

func TestHealthCheckerUpdatesAliveStatus(t *testing.T) {
	// Spin up a test server that always responds 200.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// We test the HealthChecker indirectly through the pool — after a check
	// cycle a proxy that resolves should have Alive=true.
	checker := proxy.NewHealthChecker(srv.URL, 100*time.Millisecond)
	p := &proxy.Proxy{URL: srv.URL, Protocol: "http", Host: "127.0.0.1", Port: "0"}
	h := checker.Check(context.Background(), p)
	if !h.Alive {
		t.Errorf("expected proxy to be alive against test server")
	}
	if h.Latency <= 0 {
		t.Errorf("expected positive latency, got %v", h.Latency)
	}
}

func TestHealthCheckerMarksBrokenProxy(t *testing.T) {
	checker := proxy.NewHealthChecker("http://127.0.0.1:19999", 100*time.Millisecond)
	p := &proxy.Proxy{URL: "http://127.0.0.1:19999", Protocol: "http", Host: "127.0.0.1", Port: "19999"}
	h := checker.Check(context.Background(), p)
	if h.Alive {
		t.Errorf("expected proxy to be dead for unreachable address")
	}
}
