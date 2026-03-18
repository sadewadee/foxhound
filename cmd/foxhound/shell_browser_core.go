package main

// shell_browser_core.go — platform-independent helpers for the browser shell REPL.
//
// This file has no build tags so the parsing utilities and help text are
// available to both the playwright build and the test binary.  The interactive
// browser REPL itself (cmdBrowserShell) lives in:
//
//   shell_browser.go      — //go:build playwright  (real implementation)
//   shell_browser_stub.go — //go:build !playwright (stub that prints error)

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// browserShellHelpText returns the help string for the interactive browser REPL.
// It is a standalone function so tests can call it without launching a browser.
func browserShellHelpText() string {
	return `Browser Shell Commands
======================

Navigation:
  goto <url>                    Navigate to URL
  back                          Go back in history
  forward                       Go forward in history
  reload                        Reload the current page
  url                           Print the current page URL
  title                         Print the current page title

Interaction:
  click <selector>              Click an element matching selector
  type <selector> <text>        Type text into an input element
  select <selector> <value>     Select a dropdown option by value
  scroll [down|up] [pixels]     Scroll the page (default: down 300px)
  scroll bottom                 Scroll to the bottom of the page
  wait <selector> [timeout_sec] Wait for an element to appear (default: 10s)

Extraction:
  text <selector>               Extract text content from matching elements
  attr <selector> <attr>        Extract an attribute value from matching elements
  html [selector]               Print page HTML (or element HTML if selector given)
  links [selector]              List all links (optionally scoped to selector)
  count <selector>              Count elements matching selector

Utility:
  screenshot [filename]         Save a screenshot (default: foxhound-<timestamp>.png)
  eval <js_code>                Execute JavaScript and print the result
  cookies                       List all cookies for the current page
  status                        Print current page URL, title, and content size

Shell:
  help                          Show this help message
  clear                         Clear the terminal screen
  exit / quit                   Close the browser and exit

Examples:
  goto https://example.com
  click button[type=submit]
  type #search fox scraping framework
  text h1
  attr a[href] href
  screenshot landing-page.png
  eval document.querySelectorAll('p').length
`
}

// parseBrowserCommand splits a raw input line into (command, args).
// Returns ("", nil) for blank or whitespace-only lines.
func parseBrowserCommand(line string) (cmd string, args []string) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], parts[1:]
}

// defaultScreenshotFilename returns a timestamped PNG filename for screenshots
// when the user does not supply one.
func defaultScreenshotFilename() string {
	return fmt.Sprintf("foxhound-%s.png", time.Now().Format("20060102-150405"))
}

// parseScrollPixels converts a string to a pixel count.
// Returns (0, false) if the string is empty or not a valid integer.
func parseScrollPixels(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	px, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return px, true
}

// joinArgs rejoins a slice of argument strings with single spaces.
// Returns "" for an empty slice.
func joinArgs(args []string) string {
	return strings.Join(args, " ")
}
