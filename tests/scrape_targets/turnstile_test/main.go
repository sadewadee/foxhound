//go:build playwright

// turnstile_test — test Camoufox browser with proxy + Turnstile solver
// against real-world targets.
//
// Build: go build -tags playwright ./tests/scrape_targets/turnstile_test/
// Run:   FOXHOUND_PROXY=http://user:pass@host:port ./turnstile_test
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/captcha"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
)

var proxyURL = getEnvOrDefault("FOXHOUND_PROXY", "")
var nopechaPath = getEnvOrDefault("NOPECHA_EXT", "captcha/nopecha/nopecha-signed.xpi")

type target struct {
	name string
	url  string
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	os.MkdirAll("tests/results", 0755)

	prof := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSMacOS),
		identity.WithLocale("en-GB", "en-GB", "en"),
		identity.WithTimezone("Europe/London"),
	)

	targets := []target{
		{"google-serp", "https://www.google.com/search?q=best+laptop+2025&hl=en&num=10"},
	}

	// Check if NopeCHA extension exists.
	hasNopeCHA := false
	if _, err := os.Stat(nopechaPath); err == nil {
		hasNopeCHA = true
	}

	fmt.Println("══════════════════════════════════════════════════════")
	fmt.Println("  Foxhound CAPTCHA Test — nopecha.com")
	fmt.Printf("  Targets:  %d (turnstile, recaptcha, hcaptcha, geetest)\n", len(targets))
	fmt.Printf("  Proxy:    %s\n", maskProxy(proxyURL))
	fmt.Printf("  NopeCHA:  %v (%s)\n", hasNopeCHA, nopechaPath)
	fmt.Printf("  UA:       %s\n", prof.UA)
	fmt.Println("══════════════════════════════════════════════════════")

	opts := []fetch.CamoufoxOption{
		fetch.WithBrowserIdentity(prof),
		fetch.WithBlockImages(false),
		fetch.WithHeadless("false"),
		fetch.WithBrowserTimeout(60 * time.Second),
		fetch.WithPersistSession(true),
	}
	if proxyURL != "" {
		opts = append(opts, fetch.WithBrowserProxy(proxyURL))
	}
	if hasNopeCHA {
		opts = append(opts, fetch.WithExtensionPath(nopechaPath))
	}

	cf, err := fetch.NewCamoufox(opts...)
	if err != nil {
		slog.Error("launch failed", "err", err)
		os.Exit(1)
	}
	defer cf.Close()

	for i, t := range targets {
		if i > 0 {
			fmt.Println("\nWaiting 5s between targets...")
			time.Sleep(5 * time.Second)
		}
		fmt.Printf("\n[%d/%d] %s → %s\n", i+1, len(targets), t.name, t.url)
		testTarget(cf, t)
	}

	fmt.Println("\n══════════════════════════════════════════════════════")
	fmt.Println("  All targets complete")
	fmt.Println("══════════════════════════════════════════════════════")
}

func testTarget(cf foxhound.Fetcher, t target) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	start := time.Now()
	resp, err := cf.Fetch(ctx, &foxhound.Job{
		ID:        t.name,
		URL:       t.url,
		Method:    "GET",
		FetchMode: foxhound.FetchBrowser,
	})
	duration := time.Since(start)

	if err != nil {
		fmt.Printf("  ✗ FAILED: %v (duration=%s)\n", err, duration.Round(time.Millisecond))
		return
	}

	fmt.Printf("  status=%d bytes=%d duration=%s\n",
		resp.StatusCode, len(resp.Body), duration.Round(time.Millisecond))

	// Save HTML
	htmlPath := fmt.Sprintf("tests/results/%s.html", strings.ReplaceAll(t.name, ".", "_"))
	os.WriteFile(htmlPath, resp.Body, 0644)
	fmt.Printf("  HTML → %s\n", htmlPath)

	// CAPTCHA check
	det := captcha.Detect(resp)
	if det.Type != captcha.CaptchaNone {
		fmt.Printf("  ⚠ CAPTCHA detected: type=%s sitekey=%s\n", det.Type, det.SiteKey)
	} else {
		fmt.Printf("  ✓ No CAPTCHA — page loaded clean\n")
	}

	// Quick content check
	body := strings.ToLower(string(resp.Body))
	if strings.Contains(body, "success") || strings.Contains(body, "passed") {
		fmt.Printf("  ✓ Success indicator found\n")
	}
	if resp.StatusCode >= 400 {
		fmt.Printf("  ⚠ HTTP error %d\n", resp.StatusCode)
	}
}

func maskProxy(p string) string {
	if p == "" {
		return "(none — direct connection)"
	}
	// Hide credentials
	if idx := strings.Index(p, "@"); idx != -1 {
		scheme := p[:strings.Index(p, "://")+3]
		return scheme + "***@" + p[idx+1:]
	}
	return p
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
