//go:build playwright

// Social Media Scraper Template — demonstrates anti-detection algorithms.
//
// This example demonstrates all the mathematical improvements for social media
// scraping with minimal detection:
//
//   - Weibull/Gamma-distributed behavior (scroll, rhythm, pauses)
//   - Bigram-aware per-character typing speed model
//   - Gaussian mouse jitter (center-heavy, not uniform)
//   - Session fatigue/warmup (inverted-U speed curve)
//   - Bayesian domain risk scoring (adaptive static→browser routing)
//   - Circuit breaker with exponential backoff
//   - Careful behavior profile (Cloudflare Enterprise / Akamai level)
//
// Usage:
//
//	go run -tags playwright ./examples/social/ -url "https://www.instagram.com/natgeo/"
//	go run -tags playwright ./examples/social/ -url "https://x.com/elonmusk" -headless false
//	go run -tags playwright ./examples/social/ -url "https://www.tiktok.com/@natgeo" -max 50
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/PuerkitoBio/goquery"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/behavior"
	"github.com/sadewadee/foxhound/engine"
	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/identity"
	"github.com/sadewadee/foxhound/middleware"
	"github.com/sadewadee/foxhound/pipeline/export"
	"github.com/sadewadee/foxhound/queue"
)

func main() {
	profileURL := flag.String("url", "", "Social media profile URL to scrape")
	headless := flag.String("headless", "true", "Browser mode: true, false, virtual")
	maxPosts := flag.Int("max", 30, "Maximum posts to collect from feed")
	output := flag.String("output", "social_results.jsonl", "Output file path")
	proxyURL := flag.String("proxy", "", "Proxy URL (socks5:// or http://)")
	flag.Parse()

	if *profileURL == "" {
		fmt.Fprintln(os.Stderr, "Usage: social -url <profile-url>")
		os.Exit(1)
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	// --- Identity: generate a consistent Firefox profile ---
	id := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSMacOS),
	)

	// --- Behavior: use Careful profile for maximum stealth ---
	// This profile uses:
	//   - Weibull-distributed rhythm delays (mode ~550ms, right-skewed)
	//   - Gamma-distributed scroll distances (mode-heavy)
	//   - Bigram-aware typing (per-character speed varies by hand/finger)
	//   - Session fatigue (warmup=0.5, fatigue=0.3)
	//   - Gaussian mouse jitter (center-heavy)
	// Jitter() adds ±15% random perturbation to ALL profile parameters,
	// preventing anti-bot ML from clustering sessions into discrete archetypes.
	profile := behavior.CarefulProfile().Jitter()

	// --- Session Fatigue: inverted-U speed curve (with per-call noise) ---
	// factor(t) = warmup(t) * fatigue(t)
	// warmup(t) = 1.0 + 0.5 * exp(-t/120s)   → 50% slower at session start
	// fatigue(t) = 1.0 + 0.3 * (1 - exp(-t/1800s)) → 30% slower after 30min
	fatigue := behavior.NewSessionFatigue(profile.Fatigue)
	fatigue.Start()

	// --- Browser Fetcher: Camoufox with all anti-detection ---
	opts := []fetch.CamoufoxOption{
		fetch.WithBrowserIdentity(id),
		fetch.WithBehaviorProfile(profile),
		fetch.WithHeadless(*headless),
		fetch.WithBlockImages(true),
		fetch.WithMaxBrowserRequests(100),
	}
	if *proxyURL != "" {
		opts = append(opts, fetch.WithBrowserProxy(*proxyURL))
	}

	browser, err := fetch.NewCamoufox(opts...)
	if err != nil {
		slog.Error("failed to create browser", "err", err)
		os.Exit(1)
	}
	defer browser.Close()

	// --- SmartFetcher with Bayesian domain learning ---
	// Social media preset: Beta(3,1) prior = 75% prior block rate.
	// Escalates to browser after just 1 blocked static attempt.
	// Asymmetric decay: blocks decay 4x slower than successes (24h halflife).
	scorer := fetch.NewDomainScorer(fetch.SocialMediaScoreConfig())
	static, _ := fetch.NewStealth(id)
	smart := fetch.NewSmart(static, browser,
		fetch.WithDomainScorer(scorer),
	)

	// --- Middleware stack ---
	chain := middleware.Chain(
		// Circuit breaker: open_duration = min(30s * 2^(trips-1), 10min) * (1±10% jitter)
		middleware.NewCircuitBreaker(middleware.DefaultCircuitBreakerConfig()),
		// Per-domain delay with ±25% jitter (Randomize fix)
		middleware.NewDomainDelay(middleware.DomainDelayConfig{
			DefaultDelay: 3 * time.Second,
			Randomize:    true,
		}),
		// Adaptive throttle: delay = EMA(latency, alpha=0.3) / concurrency
		// With outlier dampening: clamp to median ± 3*MAD
		middleware.NewAutoThrottle(middleware.AutoThrottleConfig{
			TargetConcurrency: 1,
			InitialDelay:      2 * time.Second,
			MinDelay:          1 * time.Second,
			MaxDelay:          15 * time.Second,
			Alpha:             0.3,
		}),
		// Block detection with exponential backoff
		middleware.NewBlockDetector(middleware.DefaultBlockDetectorConfig()),
	)

	fetcher := chain.Wrap(smart)

	// --- Trail: navigate to profile and scroll the feed ---
	trail := engine.NewTrail("social-feed").
		Navigate(*profileURL).
		// Dismiss cookie/login popups (platform-agnostic selectors)
		WaitOptional("body", 5*time.Second).
		ClickOptional("button[data-testid='cookie-policy-manage-dialog-accept-button']").
		ClickOptional("[aria-label='Close']").
		ClickOptional("button:has-text('Accept')").
		ClickOptional("button:has-text('Not now')").
		// Wait for feed content to render
		WaitOptional("article", 10*time.Second).
		WaitOptional("[data-testid='tweet']", 10*time.Second).
		// Infinite scroll the feed to load posts
		// Uses Gamma-distributed scroll distances and Weibull-distributed pauses
		InfiniteScrollUntil("article, [data-testid='tweet'], [data-e2e='user-post-item']", *maxPosts, *maxPosts*5).
		// Extract post URLs via JavaScript (bypasses obfuscated CSS selectors)
		Evaluate(fmt.Sprintf(`() => {
			const links = new Set();
			document.querySelectorAll('a[href]').forEach(a => {
				const href = a.getAttribute('href');
				if (href && (href.includes('/p/') || href.includes('/status/') ||
				    href.includes('/video/') || href.includes('/reel/'))) {
					links.add(href.startsWith('http') ? href : window.location.origin + href);
				}
			});
			return Array.from(links).slice(0, %d);
		}`, *maxPosts))

	// --- Output writer ---
	writer, err := export.NewJSONLinesWriter(*output)
	if err != nil {
		slog.Error("failed to create writer", "err", err)
		os.Exit(1)
	}

	// --- Processor: extract post metadata from each page ---
	processor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
		doc, err := goquery.NewDocumentFromReader(resp.BodyReader())
		if err != nil {
			return &foxhound.Result{}, nil
		}

		title := doc.Find("title").First().Text()
		description, _ := doc.Find("meta[property='og:description']").Attr("content")
		image, _ := doc.Find("meta[property='og:image']").Attr("content")

		item := foxhound.Item{
			"url":         resp.URL,
			"title":       title,
			"description": description,
			"image":       image,
		}

		return &foxhound.Result{Items: []foxhound.Item{item}}, nil
	})

	// --- Hunt configuration ---
	q := queue.NewMemory(256)
	hunt := engine.NewHunt(engine.HuntConfig{
		Name:    "social-scrape",
		Walkers: 1, // Single walker for social media — multiple looks suspicious
		Seeds:   trail.ToJobs(),
		Queue:   q,
		Fetcher: fetcher,
		Writers: []foxhound.Writer{writer},
		Processor: processor,
		// Careful profile rhythm: bursts of 5-10 actions, pauses 30-90s,
		// long pauses 3-8min (25% probability)
		BehaviorProfile: profile,
	})

	// --- Run with graceful shutdown ---
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	slog.Info("starting social media scrape",
		"url", *profileURL,
		"max_posts", *maxPosts,
		"profile", profile.Name,
		"fatigue_warmup", profile.Fatigue.WarmupAmplitude,
		"fatigue_tau", profile.Fatigue.FatigueTau,
	)

	result := hunt.Run(ctx)

	slog.Info("scrape complete",
		"items", result.TotalItems,
		"requests", result.TotalRequests,
		"errors", result.TotalErrors,
		"blocked", result.TotalBlocked,
		"duration", result.Elapsed,
	)
}
