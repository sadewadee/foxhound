package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/proxy"
)

// cmdProxyTest loads the proxy pool from configuration and probes each proxy,
// printing a health summary.
func cmdProxyTest(args []string) {
	fs := flag.NewFlagSet("proxy-test", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: foxhound proxy-test [flags]")
		fmt.Fprintln(os.Stderr, "\nTest proxy pool health.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}

	configPath := fs.String("config", "config.yaml", "path to configuration file")
	checkURL := fs.String("check-url", "https://httpbin.org/ip", "URL used to verify proxy connectivity")
	timeout := fs.Duration("timeout", 10*time.Second, "per-proxy check timeout")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	cfg, err := foxhound.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config %q: %v\n", *configPath, err)
		os.Exit(1)
	}

	foxhound.SetupLogging(cfg.Logging, globalVerbose)

	// Build the provider list from config.
	var providers []proxy.Provider
	for _, entry := range cfg.Proxy.Providers {
		if entry.Type == "static" && len(entry.List) > 0 {
			providers = append(providers, proxy.Static(entry.List))
		}
	}

	pool := proxy.NewPool(providers...)
	defer pool.Close()

	total := pool.Len()
	if total == 0 {
		fmt.Println("Proxy pool is empty — add proxies in config.yaml under proxy.providers")
		return
	}

	fmt.Printf("\nProxy Health Check (%d proxies)\n", total)
	fmt.Println("-----------------------------------")

	checker := proxy.NewHealthChecker(*checkURL, *timeout)
	ctx := context.Background()

	healthy, slow, dead := 0, 0, 0
	slowThreshold := 2 * time.Second

	// We check by getting all proxies from the pool.
	for i := 0; i < total; i++ {
		pctx, cancel := context.WithTimeout(ctx, *timeout+time.Second)
		p, err := pool.Get(pctx)
		cancel()
		if err != nil {
			dead++
			continue
		}

		h := checker.Check(ctx, p)
		status := proxyStatus(h, slowThreshold)
		fmt.Printf("  [%-7s] %-40s latency=%s\n", status, p.URL, h.Latency.Round(time.Millisecond))

		switch status {
		case "HEALTHY":
			healthy++
		case "SLOW":
			slow++
		default:
			dead++
		}
		pool.Release(p, h.Alive)
	}

	fmt.Println("-----------------------------------")
	fmt.Printf("Summary: %d healthy, %d slow, %d dead  (score: %.0f%%)\n",
		healthy, slow, dead, float64(healthy)*100/float64(total))
}

// proxyStatus converts a ProxyHealth into a human-readable status label.
func proxyStatus(h proxy.ProxyHealth, slowThreshold time.Duration) string {
	if !h.Alive {
		return "DEAD"
	}
	if h.Latency > slowThreshold {
		return "SLOW"
	}
	return "HEALTHY"
}
