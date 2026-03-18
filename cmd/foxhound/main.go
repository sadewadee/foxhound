// Command foxhound is the CLI entry point for the Foxhound scraping framework.
package main

import (
	"fmt"
	"os"
	"strings"

	foxhound "github.com/sadewadee/foxhound"
)

const version = "0.0.2"

// globalVerbose is the verbosity level set by -v / -vv flags.
// 0 = normal (info), 1 = verbose (debug), 2 = very verbose (debug + source).
var globalVerbose int

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
	case "check", "shell":
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
	case "resume":
		cmdResume(cmdArgs)
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
	for _, arg := range args {
		switch arg {
		case "-vv":
			globalVerbose = 2
		case "-v", "--verbose":
			if globalVerbose < 1 {
				globalVerbose = 1
			}
		default:
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
  -v          Verbose output (debug level logging)
  -vv         Very verbose output (debug + source location)

Commands:
  init        Scaffold a new foxhound project
  run         Run a hunt
  check       Test identity fingerprint and TLS consistency
  proxy-test  Test proxy pool health and latency
  shell       Start an interactive scraping shell (REPL)
  resume      Resume an interrupted hunt from a persistent queue
  version     Print version
  help        Show this help

Run "foxhound <command> -help" for command-specific flags.
`)
}
