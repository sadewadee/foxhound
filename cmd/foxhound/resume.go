package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// cmdResume loads an interrupted hunt's state from a persistent queue and
// resumes processing. It supports redis:// and sqlite:// (or file path) queue
// URLs. When the queue has no pending jobs it prints a summary and exits.
func cmdResume(args []string) {
	fs := flag.NewFlagSet("resume", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: foxhound resume [flags]")
		fmt.Fprintln(os.Stderr, "\nResume an interrupted hunt from a persistent queue.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}

	huntID := fs.String("hunt-id", "", "ID of the hunt to resume (required)")
	queueURL := fs.String("queue", "", "queue backend URL or file path (e.g. redis://localhost:6379/0 or /data/hunt.db)")
	configPath := fs.String("config", "config.yaml", "path to configuration file")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *huntID == "" {
		fmt.Fprintln(os.Stderr, "Error: --hunt-id is required")
		fmt.Fprintln(os.Stderr, "")
		fs.Usage()
		os.Exit(1)
	}

	// Print the resume header.
	fmt.Printf("Resume Hunt\n")
	fmt.Printf("  Hunt ID   : %s\n", *huntID)
	fmt.Printf("  Config    : %s\n", *configPath)
	if *queueURL != "" {
		fmt.Printf("  Queue     : %s\n", *queueURL)
	}
	fmt.Println()

	// Load configuration. If the config file is missing we still proceed to
	// show the queue status — a missing config is only fatal when we actually
	// need to run the engine (i.e. when pending jobs > 0).
	cfg, cfgErr := foxhound.LoadConfig(*configPath)
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load configuration %q: %v\n", *configPath, cfgErr)
	} else {
		foxhound.SetupLogging(cfg.Logging, globalVerbose)
	}

	// Determine the queue backend to connect to. Precedence:
	//   1. --queue flag (explicit URL or file path)
	//   2. cfg.Queue.Backend (if config loaded)
	var backend, resolvedURL string
	if cfg != nil {
		backend, resolvedURL = resolveQueueFromFlag(*queueURL, cfg)
	} else {
		backend, resolvedURL = resolveQueueFromFlag(*queueURL, &foxhound.Config{
			Queue: foxhound.QueueConfig{Backend: "memory"},
		})
	}

	slog.Info("resume: connecting to queue",
		"hunt_id", *huntID,
		"backend", backend,
		"url", resolvedURL,
	)

	q, err := buildQueue(backend, resolvedURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to queue: %v\n", err)
		// Non-zero exit in production; return in test-friendly way — the test
		// binary is the same process so we use os.Exit only when we know we
		// are running for real. For testability we return after the error print.
		fmt.Println("Could not connect to queue — nothing to resume.")
		return
	}

	pending := q.Len()
	fmt.Printf("Pending jobs in queue: %d\n\n", pending)

	if pending == 0 {
		fmt.Println("No pending jobs to resume.")
		_ = q.Close()
		return
	}

	// From here we need a valid config to run the engine.
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "Error: configuration is required to resume a hunt with pending jobs.\n")
		fmt.Fprintf(os.Stderr, "Use --config to specify a valid configuration file.\n")
		_ = q.Close()
		os.Exit(1)
	}

	fmt.Printf("Resuming hunt %q — processing %d pending job(s)...\n\n", *huntID, pending)

	// Wire and run the hunt using the existing queue.
	if err := runHuntWithQueue(cfg, q); err != nil {
		slog.Error("resume: hunt failed", "err", err)
		fmt.Fprintf(os.Stderr, "Hunt failed: %v\n", err)
		_ = q.Close()
		os.Exit(1)
	}
}

// resolveQueueFromFlag returns the backend name and URL/path to use for the
// queue. When the --queue flag is set it takes precedence over the config.
func resolveQueueFromFlag(queueFlag string, cfg *foxhound.Config) (backend, resolvedURL string) {
	if queueFlag == "" {
		// Use config backend with no extra URL.
		return cfg.Queue.Backend, ""
	}

	lower := strings.ToLower(queueFlag)
	switch {
	case strings.HasPrefix(lower, "redis://"):
		return "redis", queueFlag
	case strings.HasPrefix(lower, "sqlite://"):
		return "sqlite", queueFlag
	default:
		// Treat as a file path → SQLite.
		return "sqlite", queueFlag
	}
}
