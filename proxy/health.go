package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// HealthChecker probes proxies against a test endpoint to determine liveness
// and measure latency.
type HealthChecker struct {
	// checkURL is the URL used to test proxy connectivity.
	checkURL string
	// timeout is the per-check deadline.
	timeout time.Duration
}

// NewHealthChecker creates a HealthChecker that probes proxies by requesting
// checkURL.  timeout is applied to each individual probe.
func NewHealthChecker(checkURL string, timeout time.Duration) *HealthChecker {
	return &HealthChecker{checkURL: checkURL, timeout: timeout}
}

// Check probes a single proxy and returns its updated health.
// The ProxyHealth.Score is set to 1.0 for alive proxies and 0.0 for dead ones.
//
// The request is routed through the proxy under test so that the latency and
// reachability measurement reflects actual proxy connectivity.
func (hc *HealthChecker) Check(ctx context.Context, p *Proxy) ProxyHealth {
	start := time.Now()

	proxyURL, err := url.Parse(p.URL)
	if err != nil {
		slog.Warn("proxy: health check cannot parse proxy URL", "proxy", p.URL, "err", err)
		return ProxyHealth{Alive: false, Score: 0}
	}

	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   hc.timeout,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hc.checkURL, nil)
	if err != nil {
		slog.Warn("proxy: health check request creation failed", "proxy", p.URL, "err", err)
		return ProxyHealth{Alive: false, Score: 0}
	}

	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		slog.Debug("proxy: health check failed", "proxy", p.URL, "err", err, "latency", latency)
		return ProxyHealth{Alive: false, Score: 0, Latency: latency}
	}
	resp.Body.Close()

	slog.Debug("proxy: health check ok", "proxy", p.URL, "status", resp.StatusCode, "latency", latency)
	return ProxyHealth{
		Alive:       true,
		Latency:     latency,
		SuccessRate: 1.0,
		Score:       1.0,
	}
}
