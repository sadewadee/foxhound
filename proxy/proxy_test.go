package proxy_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sadewadee/foxhound/proxy"
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

// TestParse_AllFormats exercises every supported proxy string format.
func TestParse_AllFormats(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantHost  string
		wantPort  string
		wantUser  string
		wantPass  string
		wantProto string
	}{
		// Standard URL format
		{
			name: "standard http URL",
			input: "http://user:pass@1.2.3.4:8080",
			wantHost: "1.2.3.4", wantPort: "8080",
			wantUser: "user", wantPass: "pass", wantProto: "http",
		},
		{
			name: "standard socks5 URL",
			input: "socks5://user:pass@1.2.3.4:1080",
			wantHost: "1.2.3.4", wantPort: "1080",
			wantUser: "user", wantPass: "pass", wantProto: "socks5",
		},
		// host:port only
		{
			name: "bare host:port",
			input: "1.2.3.4:8080",
			wantHost: "1.2.3.4", wantPort: "8080",
			wantUser: "", wantPass: "", wantProto: "http",
		},
		// host:port:user:pass
		{
			name: "host:port:user:pass",
			input: "1.2.3.4:8080:myuser:mypass",
			wantHost: "1.2.3.4", wantPort: "8080",
			wantUser: "myuser", wantPass: "mypass", wantProto: "http",
		},
		// user:pass:host:port
		{
			name: "user:pass:host:port",
			input: "myuser:mypass:1.2.3.4:8080",
			wantHost: "1.2.3.4", wantPort: "8080",
			wantUser: "myuser", wantPass: "mypass", wantProto: "http",
		},
		// protocol:host:port:user:pass
		{
			name: "socks5:host:port:user:pass",
			input: "socks5:1.2.3.4:1080:myuser:mypass",
			wantHost: "1.2.3.4", wantPort: "1080",
			wantUser: "myuser", wantPass: "mypass", wantProto: "socks5",
		},
		{
			name: "http:host:port:user:pass",
			input: "http:1.2.3.4:8080:user:pass",
			wantHost: "1.2.3.4", wantPort: "8080",
			wantUser: "user", wantPass: "pass", wantProto: "http",
		},
		// user:pass@host:port (no scheme)
		{
			name: "user:pass@host:port no scheme",
			input: "user:pass@1.2.3.4:8080",
			wantHost: "1.2.3.4", wantPort: "8080",
			wantUser: "user", wantPass: "pass", wantProto: "http",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := proxy.Parse(tc.input)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tc.input, err)
			}
			if p.Host != tc.wantHost {
				t.Errorf("host: got %q, want %q", p.Host, tc.wantHost)
			}
			if p.Port != tc.wantPort {
				t.Errorf("port: got %q, want %q", p.Port, tc.wantPort)
			}
			if p.Username != tc.wantUser {
				t.Errorf("username: got %q, want %q", p.Username, tc.wantUser)
			}
			if p.Password != tc.wantPass {
				t.Errorf("password: got %q, want %q", p.Password, tc.wantPass)
			}
			if p.Protocol != tc.wantProto {
				t.Errorf("protocol: got %q, want %q", p.Protocol, tc.wantProto)
			}
		})
	}
}

// TestParse_EmptyString verifies that an empty input returns an error.
func TestParse_EmptyString(t *testing.T) {
	_, err := proxy.Parse("")
	if err == nil {
		t.Fatal("expected error for empty string, got nil")
	}
}

// TestParse_InvalidFormat verifies that unrecognised colon counts return errors.
func TestParse_InvalidFormat(t *testing.T) {
	badInputs := []string{
		"a:b:c",         // 3 parts — ambiguous
		"a:b:c:d:e:f",  // 6 parts — too many
	}
	for _, input := range badInputs {
		t.Run(input, func(t *testing.T) {
			_, err := proxy.Parse(input)
			if err == nil {
				t.Fatalf("Parse(%q): expected error, got nil", input)
			}
		})
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

// ---------------------------------------------------------------------------
// H4: HealthChecker must route traffic through the proxy under test
// ---------------------------------------------------------------------------

// TestHealthChecker_RoutesRequestThroughProxy verifies that Check uses the
// proxy URL when making the health-check request.  We spin up a local proxy
// server that records whether it received a CONNECT/GET and assert it was
// actually called.
func TestHealthChecker_RoutesRequestThroughProxy(t *testing.T) {
	var proxyHit atomic.Bool

	// Minimal proxy server: accepts any connection, records the hit, then
	// responds 200 so the check reads a valid HTTP response.
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHit.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer proxySrv.Close()

	// Target server that should be reached via the proxy above.
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer targetSrv.Close()

	checker := proxy.NewHealthChecker(targetSrv.URL, 500*time.Millisecond)
	p := &proxy.Proxy{
		URL:      proxySrv.URL,
		Protocol: "http",
		Host:     "127.0.0.1",
		Port:     "0",
	}
	checker.Check(context.Background(), p)

	if !proxyHit.Load() {
		t.Error("HealthChecker did not route the health-check request through the proxy")
	}
}

// ---------------------------------------------------------------------------
// M1: Pool.selectBest must respect maxRequests and auto-cooldown
// ---------------------------------------------------------------------------

// TestPool_MaxRequests_SkipsExhaustedProxy verifies that when a proxy has
// handled maxRequests it is no longer returned by Get, and that after Release
// pushes it onto cooldown it is unavailable within a short timeout.
func TestPool_MaxRequests_SkipsExhaustedProxy(t *testing.T) {
	provider := proxy.Static([]string{"http://1.2.3.4:8080"})
	pool := proxy.NewPool(provider)
	pool.SetMaxRequests(2)
	pool.SetCooldown(10 * time.Second)
	defer pool.Close()

	// Consume the 2 allowed requests.
	p1, err := pool.Get(context.Background())
	if err != nil {
		t.Fatalf("Get #1: %v", err)
	}
	pool.Release(p1, true)

	p2, err := pool.Get(context.Background())
	if err != nil {
		t.Fatalf("Get #2: %v", err)
	}
	pool.Release(p2, true) // this release hits maxRequests — should trigger cooldown

	// Now the proxy should be on cooldown; Get with short deadline must fail.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = pool.Get(ctx)
	if err == nil {
		t.Fatal("expected Get to fail after proxy reached maxRequests, but it succeeded")
	}
}

// ---------------------------------------------------------------------------
// M2: Pool rotation strategies
// ---------------------------------------------------------------------------

// TestPool_PerRequest_RoundRobin verifies that successive Get calls with
// PerRequest rotation cycle through different proxies rather than always
// returning the highest-score one.
func TestPool_PerRequest_RoundRobin(t *testing.T) {
	provider := proxy.Static([]string{
		"http://1.1.1.1:8080",
		"http://2.2.2.2:8080",
		"http://3.3.3.3:8080",
	})
	pool := proxy.NewPool(provider)
	pool.SetRotation(proxy.PerRequest)
	defer pool.Close()

	seen := map[string]int{}
	for i := 0; i < 9; i++ {
		p, err := pool.Get(context.Background())
		if err != nil {
			t.Fatalf("Get #%d: %v", i, err)
		}
		seen[p.URL]++
		pool.Release(p, true)
	}

	// With 3 proxies and 9 requests each proxy should be used at least twice.
	for url, count := range seen {
		if count < 2 {
			t.Errorf("proxy %s used only %d times in 9 round-robin calls — not cycling", url, count)
		}
	}
	if len(seen) < 3 {
		t.Errorf("only %d distinct proxies seen in 9 calls — expected all 3 to be used", len(seen))
	}
}

// TestPool_GetForSession_StickyProxy verifies that two calls with the same
// sessionID return the same proxy.
func TestPool_GetForSession_StickyProxy(t *testing.T) {
	provider := proxy.Static([]string{
		"http://1.1.1.1:8080",
		"http://2.2.2.2:8080",
	})
	pool := proxy.NewPool(provider)
	defer pool.Close()

	ctx := context.Background()
	p1, err := pool.GetForSession(ctx, "session-abc")
	if err != nil {
		t.Fatalf("GetForSession #1: %v", err)
	}
	p2, err := pool.GetForSession(ctx, "session-abc")
	if err != nil {
		t.Fatalf("GetForSession #2: %v", err)
	}
	if p1.URL != p2.URL {
		t.Errorf("GetForSession returned different proxies for same session: %s vs %s", p1.URL, p2.URL)
	}
}

// TestPool_GetForDomain_StickyProxy verifies that two calls with the same
// domain return the same proxy.
func TestPool_GetForDomain_StickyProxy(t *testing.T) {
	provider := proxy.Static([]string{
		"http://1.1.1.1:8080",
		"http://2.2.2.2:8080",
	})
	pool := proxy.NewPool(provider)
	defer pool.Close()

	ctx := context.Background()
	p1, err := pool.GetForDomain(ctx, "example.com")
	if err != nil {
		t.Fatalf("GetForDomain #1: %v", err)
	}
	p2, err := pool.GetForDomain(ctx, "example.com")
	if err != nil {
		t.Fatalf("GetForDomain #2: %v", err)
	}
	if p1.URL != p2.URL {
		t.Errorf("GetForDomain returned different proxies for same domain: %s vs %s", p1.URL, p2.URL)
	}
}

// ---------------------------------------------------------------------------
// M5: Pool.Get must sleep until earliest cooldown expiry, not busy-wait
// ---------------------------------------------------------------------------

// TestPool_Get_SleepsUntilCooldownExpiry verifies that when the only proxy is
// on a short cooldown, Get blocks until the cooldown expires rather than
// returning an error or spinning in a tight loop.  We use a 100 ms cooldown
// and assert that Get returns the proxy within a 500 ms window.
func TestPool_Get_SleepsUntilCooldownExpiry(t *testing.T) {
	provider := proxy.Static([]string{"http://1.2.3.4:8080"})
	pool := proxy.NewPool(provider)
	pool.SetCooldown(100 * time.Millisecond)
	defer pool.Close()

	// Ban to trigger a 100 ms cooldown.
	p, err := pool.Get(context.Background())
	if err != nil {
		t.Fatalf("initial Get: %v", err)
	}
	pool.Ban(p, "example.com")

	// Now the only proxy is on cooldown; Get should block ~100 ms then succeed.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	p2, err := pool.Get(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Get after cooldown: %v", err)
	}
	if p2 == nil {
		t.Fatal("expected non-nil proxy after cooldown expired")
	}
	// The proxy should have been returned reasonably close to the 100 ms mark.
	if elapsed < 80*time.Millisecond {
		t.Errorf("Get returned too quickly (%v) — cooldown may not have been respected", elapsed)
	}
}

// TestPool_Get_EmptyPoolReturnsError verifies that Get returns ErrNoProxy
// immediately when the pool has no proxies at all (M5 edge case: no earliest
// expiry to compute).
func TestPool_Get_EmptyPoolReturnsError(t *testing.T) {
	pool := proxy.NewPool()
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := pool.Get(ctx)
	if err == nil {
		t.Fatal("expected error from empty pool, got nil")
	}
}
