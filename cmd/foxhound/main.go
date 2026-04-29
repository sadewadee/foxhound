// Command foxhound is the CLI entry point for the Foxhound scraping framework.
package main

import (
	"fmt"
	"os"
	"strings"

	foxhound "github.com/sadewadee/foxhound"
)

const version = "0.0.19"

// globalVerbose is the verbosity level set by -v / -vv flags.
// 0 = normal (info), 1 = verbose (debug), 2 = very verbose (debug + source).
var globalVerbose int

// globalHeadless is the browser display mode set by --headless flag.
// "true" = headless, "false" = visible window, "virtual" = Xvfb.
// Empty string means not set (subcommands use their own default).
var globalHeadless string

// globalFast disables warm-up steps and sets BehaviorProfile to "aggressive".
var globalFast bool

func main() {
	// Parse global flags before the command name.
	// Supports: -v, -vv, --verbose
	args := os.Args[1:]
	args = parseGlobalFlags(args)

	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	cmd := args[0]
	cmdArgs := args[1:]

	// Setup logging with the appropriate verbosity for commands that don't
	// load their own config. Commands that load config call SetupLogging
	// themselves with the config's logging section.
	switch cmd {
	case "version", "help":
		// No logging needed.
	case "check", "shell", "browser-shell":
		// These commands don't load config — use default logging config.
		foxhound.SetupLogging(foxhound.LoggingConfig{
			Level:  "info",
			Format: "text",
			Output: "stderr",
		}, globalVerbose)
	}

	switch cmd {
	case "init":
		cmdInit(cmdArgs)
	case "run":
		cmdRun(cmdArgs)
	case "check":
		cmdCheck(cmdArgs)
	case "proxy-test":
		cmdProxyTest(cmdArgs)
	case "shell":
		cmdShell(cmdArgs)
	case "browser-shell":
		cmdBrowserShell(cmdArgs)
	case "resume":
		cmdResume(cmdArgs)
	case "curl2fox":
		cmdCurl2Fox(cmdArgs)
	case "preview":
		cmdPreview(cmdArgs)
	case "version":
		fmt.Printf("foxhound v%s\n", version)
	case "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

// parseGlobalFlags extracts -v, -vv, --verbose flags from args and sets
// globalVerbose. Returns remaining args with global flags removed.
func parseGlobalFlags(args []string) []string {
	var remaining []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-vv":
			globalVerbose = 2
		case "-v", "--verbose":
			if globalVerbose < 1 {
				globalVerbose = 1
			}
		case "--headless":
			// Consume the next arg as the headless mode value.
			if i+1 < len(args) {
				i++
				globalHeadless = args[i]
			}
		case "--fast":
			globalFast = true
		default:
			// --headless=value form
			if strings.HasPrefix(arg, "--headless=") {
				globalHeadless = strings.TrimPrefix(arg, "--headless=")
				continue
			}
			// Check for -v combined with other short flags (unlikely but safe).
			if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg, "v") {
				// Count v's: -v = 1, -vv = 2
				vCount := strings.Count(arg, "v")
				if vCount > globalVerbose {
					globalVerbose = vCount
				}
				// Remove v's from the flag — if nothing left, skip it.
				cleaned := strings.ReplaceAll(arg, "v", "")
				if cleaned == "-" {
					continue
				}
				remaining = append(remaining, cleaned)
				continue
			}
			remaining = append(remaining, arg)
		}
	}
	return remaining
}

func printUsage() {
	fmt.Print(`Usage: foxhound [global flags] <command> [command flags]

Global Flags:
  -v                Verbose output (debug level logging)
  -vv               Very verbose output (debug + source location)
  --headless MODE   Browser display mode: "true", "false", "virtual" (default "false")
  --fast              Disable warm-up, use aggressive timing (dev/testing)

Commands:
  init        Scaffold a new foxhound project
  run         Run a hunt
  check       Test identity fingerprint and TLS consistency
  proxy-test  Test proxy pool health and latency
  shell       Start an interactive scraping shell (REPL)
  browser-shell  Start an interactive Camoufox browser REPL (requires -tags playwright)
  resume      Resume an interrupted hunt from a persistent queue
  curl2fox    Convert a curl command to foxhound Go code
  preview     Fetch a URL and print the response body
  version     Print version
  help        Show this help

Run "foxhound <command> -help" for command-specific flags.
`)
}

// resolveHeadless returns the global --headless flag if set, otherwise
// falls back to the config value. This lets "foxhound --headless false run"
// override the YAML config.
func resolveHeadless(configValue string) string {
	if globalHeadless != "" {
		return globalHeadless
	}
	return configValue
}
