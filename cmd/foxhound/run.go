package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// cmdRun loads the configuration and prints a summary of the hunt that would
// be executed. Full engine wiring is performed when all packages are integrated.
func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: foxhound run [flags]")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}

	configPath := fs.String("config", "config.yaml", "path to configuration file")
	hunt := fs.String("hunt", "", "name of the hunt to run (optional, uses config default)")
	workers := fs.Int("workers", 0, "number of walker workers (overrides config)")
	dryRun := fs.Bool("dry-run", false, "validate config and print summary without running")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	cfg, err := foxhound.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration %q: %v\n", *configPath, err)
		os.Exit(1)
	}

	// Setup logging from config + global verbose flag.
	foxhound.SetupLogging(cfg.Logging, globalVerbose)

	// CLI flags override config values when explicitly provided.
	if *workers > 0 {
		cfg.Hunt.Walkers = *workers
	}
	if *hunt != "" {
		cfg.Hunt.Domain = *hunt
	}

	slog.Info("configuration loaded",
		"config", *configPath,
		"domain", cfg.Hunt.Domain,
		"walkers", cfg.Hunt.Walkers,
		"queue", cfg.Queue.Backend,
		"log_level", cfg.Logging.Level,
	)

	printRunSummary(cfg, *configPath)

	if *dryRun {
		fmt.Println("\nDry run complete. Configuration is valid.")
		return
	}

	// TODO: wire engine.Hunt when the engine package is implemented.
	fmt.Println("\nEngine not yet wired — run will be available in Phase 1 completion.")
	fmt.Println("Run with --dry-run to validate your configuration.")
}

func printRunSummary(cfg *foxhound.Config, configPath string) {
	fmt.Printf("\nFoxhound Hunt Summary\n")
	fmt.Printf("%-20s %s\n", "Config:", configPath)
	fmt.Printf("%-20s %s\n", "Domain:", cfg.Hunt.Domain)
	fmt.Printf("%-20s %d\n", "Walkers:", cfg.Hunt.Walkers)
	fmt.Printf("%-20s %s\n", "Queue backend:", cfg.Queue.Backend)
	fmt.Printf("%-20s %s\n", "Static timeout:", cfg.Fetch.Static.Timeout.Duration)
	fmt.Printf("%-20s %s\n", "Browser timeout:", cfg.Fetch.Browser.Timeout.Duration)
	fmt.Printf("%-20s %t\n", "Rate limit:", cfg.Middleware.RateLimit.Enabled)
	if cfg.Middleware.RateLimit.Enabled {
		fmt.Printf("%-20s %.2f req/s (burst %d)\n",
			"  Rate:",
			cfg.Middleware.RateLimit.RequestsPerSec,
			cfg.Middleware.RateLimit.BurstSize,
		)
	}
	fmt.Printf("%-20s %d\n", "Max depth:", cfg.Middleware.DepthLimit.Max)
	fmt.Printf("%-20s %d pipeline stage(s)\n", "Pipeline:", len(cfg.Pipeline))
}
