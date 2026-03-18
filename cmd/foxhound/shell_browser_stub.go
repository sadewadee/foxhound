//go:build !playwright

// shell_browser_stub.go — stub for the browser shell when playwright is not enabled.
//
// Compiled when the "playwright" build tag is NOT present (the default).
// Prints an actionable error message and exits cleanly so the user knows
// exactly what to do to enable the real browser REPL.

package main

import "fmt"

// cmdBrowserShell is the stub entry point for the browser REPL.
// It is called by main.go when the user runs: foxhound browser-shell
func cmdBrowserShell(_ []string) {
	fmt.Print("browser-shell requires the playwright build tag.\n\n" +
		"To use the interactive browser shell:\n\n" +
		"  1. Rebuild with the playwright tag:\n" +
		"       go build -tags playwright -o foxhound ./cmd/foxhound/\n\n" +
		"  2. Install the Firefox browser binary (once per machine):\n" +
		"       go run github.com/playwright-community/playwright-go/cmd/playwright install firefox\n\n" +
		"  3. Run the browser shell:\n" +
		"       ./foxhound browser-shell\n\n" +
		"The static HTTP shell (foxhound shell) works without the playwright tag.\n",
	)
}
