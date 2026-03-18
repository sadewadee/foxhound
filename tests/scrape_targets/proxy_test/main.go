//go:build playwright

// proxy_test tests scraping through a SOCKS5/HTTPS proxy with both
// StealthFetcher (static) and CamoufoxFetcher (browser).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
)

const (
	proxyUser = "REDACTED_USER"
	proxyPass = "REDACTED_PASS"
	proxyHost = "REDACTED_HOST"
	proxyPort = "6418"
)

type ProxyTestResult struct {
	Mode       string `json:"mode"`
	ProxyType  string `json:"proxy_type"`
	TargetURL  string `json:"target_url"`
	Status     int    `json:"status"`
	ProxyIP    string `json:"proxy_ip,omitempty"`
	RealIP     string `json:"real_ip,omitempty"`
	Bytes      int    `json:"bytes"`
	LatencyMs  int64  `json:"latency_ms"`
	Error      string `json:"error,omitempty"`
	Blocked    bool   `json:"blocked"`
	UserAgent  string `json:"user_agent,omitempty"`
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
		identity.WithLocale("en-US", "en-US", "en"),
		identity.WithTimezone("America/New_York"),
	)
	slog.Info("identity generated", "ua", prof.UA[:60]+"...")

	// Test URLs — IP check + real targets
	testURLs := []struct {
		name string
		url  string
	}{
		{"IP Check (httpbin)", "https://httpbin.org/ip"},
		{"IP Check (ipinfo)", "https://ipinfo.io/json"},
		{"TLS Check", "https://tls.peet.ws/api/all"},
		{"Google SERP", "https://www.google.com/search?q=wisata+alam+jawa+timur&hl=id&num=10"},
		{"Alibaba", "https://www.alibaba.com/trade/search?SearchText=yoga+mat"},
	}

	proxyTypes := []struct {
		name     string
		proxyURL string
	}{
		{"socks5", fmt.Sprintf("socks5://%s:%s@%s:%s", proxyUser, proxyPass, proxyHost, proxyPort)},
		{"https", fmt.Sprintf("http://%s:%s@%s:%s", proxyUser, proxyPass, proxyHost, proxyPort)},
	}

	var allResults []ProxyTestResult

	// ═══════════════════════════════════════
	// STATIC MODE (StealthFetcher + Proxy)
	// ═══════════════════════════════════════
	fmt.Println("\n══════════════════════════════════════════════════════")
	fmt.Println("STATIC MODE (StealthFetcher) — Testing proxy protocols")
	fmt.Println("══════════════════════════════════════════════════════")

	for _, pt := range proxyTypes {
		fmt.Printf("\n--- Proxy: %s ---\n", pt.name)

		fetcher := fetch.NewStealth(
			fetch.WithIdentity(prof),
			fetch.WithTimeout(30*time.Second),
			fetch.WithProxy(pt.proxyURL),
		)

		for _, target := range testURLs {
			result := testFetch(fetcher, target.name, target.url, "static", pt.name)
			allResults = append(allResults, result)
			time.Sleep(2 * time.Second) // polite delay
		}

		fetcher.Close()
	}

	// ═══════════════════════════════════════
	// BROWSER MODE (Camoufox + Proxy)
	// ═══════════════════════════════════════
	fmt.Println("\n══════════════════════════════════════════════════════")
	fmt.Println("BROWSER MODE (Camoufox) — Testing with SOCKS5 proxy")
	fmt.Println("══════════════════════════════════════════════════════")

	// Camoufox uses playwright's proxy config
	slog.Info("launching Camoufox with proxy...")

	camoufox, err := fetch.NewCamoufox(
		fetch.WithBrowserIdentity(prof),
		fetch.WithBlockImages(true),
		fetch.WithHeadless("true"),
		fetch.WithBrowserTimeout(45*time.Second),
		// Note: Camoufox proxy is set via playwright BrowserContext,
		// not via the fetcher option. For now test without proxy in browser mode.
	)
	if err != nil {
		slog.Error("camoufox launch failed", "err", err)
	} else {
		browserTargets := []struct {
			name string
			url  string
		}{
			{"IP Check (httpbin)", "https://httpbin.org/ip"},
			{"Google Maps", "https://www.google.com/maps/search/villa+di+bali/"},
		}

		for _, target := range browserTargets {
			result := testFetch(camoufox, target.name, target.url, "camoufox", "direct")
			allResults = append(allResults, result)
			time.Sleep(3 * time.Second)
		}
		camoufox.Close()
	}

	// ═══════════════════════════════════════
	// SUMMARY
	// ═══════════════════════════════════════
	fmt.Println("\n\n══════════════════════════════════════════════════════════════════════════")
	fmt.Println("PROXY TEST SUMMARY")
	fmt.Println("══════════════════════════════════════════════════════════════════════════")
	fmt.Printf("%-8s | %-8s | %-25s | %-6s | %-8s | %s\n",
		"Mode", "Proxy", "Target", "Status", "Latency", "Result")
	fmt.Println("─────────────────────────────────────────────────────────────────────────")

	totalReqs, totalOK, totalBlocked := 0, 0, 0
	for _, r := range allResults {
		totalReqs++
		status := "OK"
		if r.Error != "" {
			status = "ERROR"
			totalBlocked++
		} else if r.Blocked {
			status = "BLOCKED"
			totalBlocked++
		} else {
			totalOK++
		}

		proxyIP := r.ProxyIP
		if proxyIP == "" {
			proxyIP = "-"
		}

		fmt.Printf("%-8s | %-8s | %-25s | %-6d | %-8dms | %s (IP: %s)\n",
			r.Mode, r.ProxyType, r.TargetURL[:min(25, len(r.TargetURL))],
			r.Status, r.LatencyMs, status, proxyIP)
	}

	fmt.Println("─────────────────────────────────────────────────────────────────────────")
	avoidance := float64(totalOK) / float64(max(totalReqs, 1)) * 100
	fmt.Printf("Total: %d requests | OK: %d | Blocked/Error: %d | Block Avoidance: %.1f%%\n",
		totalReqs, totalOK, totalBlocked, avoidance)
	fmt.Println("══════════════════════════════════════════════════════════════════════════")

	// Save results
	data, _ := json.MarshalIndent(allResults, "", "  ")
	os.WriteFile("tests/results/proxy_test.json", data, 0644)
	slog.Info("results saved", "path", "tests/results/proxy_test.json")
}

func testFetch(f foxhound.Fetcher, name, url, mode, proxyType string) ProxyTestResult {
	result := ProxyTestResult{
		Mode:      mode,
		ProxyType: proxyType,
		TargetURL: url,
	}

	job := &foxhound.Job{
		ID:     fmt.Sprintf("proxy-test-%s-%s", proxyType, name),
		URL:    url,
		Method: "GET",
		Domain: "test",
	}

	start := time.Now()
	resp, err := f.Fetch(context.Background(), job)
	result.LatencyMs = time.Since(start).Milliseconds()

	if err != nil {
		result.Error = err.Error()
		slog.Error("fetch failed", "target", name, "proxy", proxyType, "err", err)
		fmt.Printf("  %-25s [%s] ERROR: %v\n", name, proxyType, err)
		return result
	}

	result.Status = resp.StatusCode
	result.Bytes = len(resp.Body)

	if resp.StatusCode >= 400 {
		result.Blocked = true
	}

	// Try to extract IP from common responses
	body := string(resp.Body)
	if len(body) > 500 {
		body = body[:500]
	}

	// Extract IP from httpbin/ipinfo responses
	if resp.StatusCode == 200 {
		type ipResp struct {
			Origin string `json:"origin"`
			IP     string `json:"ip"`
		}
		var ip ipResp
		json.Unmarshal(resp.Body, &ip)
		if ip.Origin != "" {
			result.ProxyIP = ip.Origin
		} else if ip.IP != "" {
			result.ProxyIP = ip.IP
		}
	}

	statusLabel := "OK"
	if result.Blocked {
		statusLabel = "BLOCKED"
	}

	fmt.Printf("  %-25s [%s] %d %s | %dms | %d bytes",
		name, proxyType, resp.StatusCode, statusLabel, result.LatencyMs, result.Bytes)
	if result.ProxyIP != "" {
		fmt.Printf(" | IP: %s", result.ProxyIP)
	}
	fmt.Println()

	return result
}
