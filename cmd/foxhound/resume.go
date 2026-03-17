package main

import (
	"flag"
	"fmt"
	"os"
)

// cmdResume loads an interrupted hunt's state from a persistent queue and
// prints a summary of remaining work. Full engine wiring is in Phase 3.
func cmdResume(args []string) {
	fs := flag.NewFlagSet("resume", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: foxhound resume [flags]")
		fmt.Fprintln(os.Stderr, "\nResume an interrupted hunt from a persistent queue.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}

	huntID := fs.String("hunt-id", "", "ID of the hunt to resume (required)")
	queueURL := fs.String("queue", "", "queue backend URL (e.g. redis://localhost:6379/0)")
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

	fmt.Printf("Resume Hunt\n")
	fmt.Printf("  Hunt ID   : %s\n", *huntID)
	fmt.Printf("  Config    : %s\n", *configPath)
	if *queueURL != "" {
		fmt.Printf("  Queue     : %s\n", *queueURL)
	}

	// TODO: Phase 3 — connect to persistent queue (redis/sqlite), load remaining
	// jobs, and wire the engine to process them.
	fmt.Println()
	fmt.Println("Persistent queue resume will be available in Phase 3.")
	fmt.Println("Run with a memory queue to restart from scratch, or use redis/sqlite")
	fmt.Println("backends with deltafetch to avoid re-scraping visited URLs.")
}
