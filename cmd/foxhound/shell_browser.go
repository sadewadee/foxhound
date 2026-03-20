//go:build playwright

// shell_browser.go — interactive Camoufox browser REPL for foxhound.
//
// Compiled only when the "playwright" build tag is present:
//
//	go build -tags playwright ./cmd/foxhound/
//
// Launch:
//
//	foxhound browser-shell [flags]
//
// The REPL opens a visible Camoufox (Firefox-fork) window.  The user types
// commands at the "foxhound> " prompt and the results are printed to stdout.
// The same browser page is kept alive between commands so session cookies,
// localStorage, and navigation history are all preserved.

package main

import (
	"bufio"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

// cmdBrowserShell is the entry point for the interactive browser REPL.
// Called by main.go when the user runs: foxhound browser-shell
func cmdBrowserShell(args []string) {
	fs := flag.NewFlagSet("browser-shell", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: foxhound browser-shell [flags]")
		fmt.Fprintln(os.Stderr, "\nStart an interactive Camoufox browser REPL.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}

	proxyURL := fs.String("proxy", os.Getenv("FOXHOUND_PROXY"), "proxy URL (or set FOXHOUND_PROXY)")
	extensionPath := fs.String("extension", os.Getenv("NOPECHA_EXT"), "path to Firefox extension directory (or set NOPECHA_EXT)")
	headlessDefault := "false"
	if globalHeadless != "" {
		headlessDefault = globalHeadless
	}
	headless := fs.String("headless", headlessDefault, `browser display mode: "true" (headless), "false" (visible window), "virtual" (Xvfb)`)
	timeout := fs.Duration("timeout", 120*time.Second, "per-navigation timeout")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *proxyURL != "" {
		slog.Info("browser-shell: proxy configured", "proxy", *proxyURL)
	}
	if *extensionPath != "" {
		slog.Info("browser-shell: extension loaded", "path", *extensionPath)
	}

	// Launch playwright directly — the REPL needs a visible browser page,
	// not the CamoufoxFetcher's fetch-oriented API.
	pw, err := playwright.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "browser-shell: playwright runtime error: %v\n", err)
		os.Exit(1)
	}
	defer pw.Stop() //nolint:errcheck

	launchOpts := playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(*headless == "true"),
	}
	slog.Info("browser-shell: headless mode", "mode", *headless)
	if *proxyURL != "" {
		launchOpts.Proxy = &playwright.Proxy{Server: *proxyURL}
	}
	if *extensionPath != "" {
		launchOpts.Args = []string{"--load-extension=" + *extensionPath}
	}
	browser, err := pw.Firefox.Launch(launchOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "browser-shell: could not launch Firefox: %v\n", err)
		os.Exit(1)
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "browser-shell: could not open page: %v\n", err)
		os.Exit(1)
	}
	defer page.Close()

	// Set per-navigation timeout from flag.
	page.SetDefaultTimeout(float64(timeout.Milliseconds()))

	runBrowserREPL(page)
}

// runBrowserREPL is the main REPL loop.  It is extracted from cmdBrowserShell
// to make the logic independently testable.
func runBrowserREPL(page playwright.Page) {
	fmt.Println("Foxhound Browser Shell")
	fmt.Println("Camoufox browser is open. Type 'help' for commands, 'exit' to quit.")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print(colorize(ansiGreen, "foxhound") + colorize(ansiBold, "> "))
		if !scanner.Scan() {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		cmd, args := parseBrowserCommand(line)
		if cmd == "" {
			continue
		}

		done := dispatchBrowserCommand(page, cmd, args)
		if done {
			fmt.Println("Goodbye.")
			return
		}
	}
}

// dispatchBrowserCommand executes a single browser shell command.
// Returns true when the user has requested exit/quit.
func dispatchBrowserCommand(page playwright.Page, cmd string, args []string) bool {
	switch cmd {

	// -----------------------------------------------------------------------
	// Navigation
	// -----------------------------------------------------------------------

	case "goto":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, colorize(ansiYellow, "Usage: goto <url>"))
			return false
		}
		targetURL := args[0]
		_, err := page.Goto(targetURL, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, colorize(ansiRed, "goto error: ")+"%v\n", err)
			return false
		}
		u := page.URL()
		title, _ := page.Title()
		fmt.Printf("%s %s\n", colorize(ansiGreen, "navigated:"), u)
		fmt.Printf("%s %s\n", colorize(ansiBold, "title:"), title)

	case "back":
		if _, err := page.GoBack(); err != nil {
			fmt.Fprintf(os.Stderr, colorize(ansiRed, "back error: ")+"%v\n", err)
			return false
		}
		fmt.Printf("url: %s\n", page.URL())

	case "forward":
		if _, err := page.GoForward(); err != nil {
			fmt.Fprintf(os.Stderr, colorize(ansiRed, "forward error: ")+"%v\n", err)
			return false
		}
		fmt.Printf("url: %s\n", page.URL())

	case "reload":
		if _, err := page.Reload(); err != nil {
			fmt.Fprintf(os.Stderr, colorize(ansiRed, "reload error: ")+"%v\n", err)
			return false
		}
		fmt.Printf("reloaded: %s\n", page.URL())

	case "url":
		fmt.Println(page.URL())

	case "title":
		title, err := page.Title()
		if err != nil {
			fmt.Fprintf(os.Stderr, colorize(ansiRed, "title error: ")+"%v\n", err)
			return false
		}
		fmt.Println(title)

	// -----------------------------------------------------------------------
	// Interaction
	// -----------------------------------------------------------------------

	case "click":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, colorize(ansiYellow, "Usage: click <selector>"))
			return false
		}
		sel := args[0]
		if err := page.Click(sel); err != nil {
			fmt.Fprintf(os.Stderr, colorize(ansiRed, "click error: ")+"%v\n", err)
			return false
		}
		fmt.Printf("clicked: %s\n", colorize(ansiGreen, sel))

	case "type":
		// type <selector> <text...>
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, colorize(ansiYellow, "Usage: type <selector> <text>"))
			return false
		}
		sel := args[0]
		text := joinArgs(args[1:])
		if err := page.Fill(sel, text); err != nil {
			fmt.Fprintf(os.Stderr, colorize(ansiRed, "type error: ")+"%v\n", err)
			return false
		}
		fmt.Printf("typed into %s: %q\n", colorize(ansiGreen, sel), text)

	case "select":
		// select <selector> <value>
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, colorize(ansiYellow, "Usage: select <selector> <value>"))
			return false
		}
		sel := args[0]
		val := args[1]
		if _, err := page.SelectOption(sel, playwright.SelectOptionValues{
			Values: playwright.StringSlice(val),
		}); err != nil {
			fmt.Fprintf(os.Stderr, colorize(ansiRed, "select error: ")+"%v\n", err)
			return false
		}
		fmt.Printf("selected %q in %s\n", val, colorize(ansiGreen, sel))

	case "scroll":
		browserScrollPage(page, args)

	case "wait":
		// wait <selector> [timeout_sec]
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, colorize(ansiYellow, "Usage: wait <selector> [timeout_sec]"))
			return false
		}
		sel := args[0]
		waitTimeout := 10000.0 // ms
		if len(args) >= 2 {
			if secs, err := strconv.ParseFloat(args[1], 64); err == nil {
				waitTimeout = secs * 1000
			}
		}
		if _, err := page.WaitForSelector(sel, playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(waitTimeout),
		}); err != nil {
			fmt.Fprintf(os.Stderr, colorize(ansiRed, "wait error: ")+"%v\n", err)
			return false
		}
		fmt.Printf("element found: %s\n", colorize(ansiGreen, sel))

	// -----------------------------------------------------------------------
	// Extraction
	// -----------------------------------------------------------------------

	case "text":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, colorize(ansiYellow, "Usage: text <selector>"))
			return false
		}
		browserExtractText(page, args[0])

	case "attr":
		// attr <selector> <attribute>
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, colorize(ansiYellow, "Usage: attr <selector> <attribute>"))
			return false
		}
		browserExtractAttr(page, args[0], args[1])

	case "html":
		sel := ""
		if len(args) > 0 {
			sel = args[0]
		}
		browserPrintHTML(page, sel)

	case "links":
		scope := ""
		if len(args) > 0 {
			scope = args[0]
		}
		browserListLinks(page, scope)

	case "count":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, colorize(ansiYellow, "Usage: count <selector>"))
			return false
		}
		browserCountElements(page, args[0])

	// -----------------------------------------------------------------------
	// Utility
	// -----------------------------------------------------------------------

	case "screenshot":
		filename := defaultScreenshotFilename()
		if len(args) > 0 {
			filename = args[0]
		}
		browserTakeScreenshot(page, filename)

	case "eval":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, colorize(ansiYellow, "Usage: eval <js_code>"))
			return false
		}
		js := joinArgs(args)
		result, err := page.Evaluate(js)
		if err != nil {
			fmt.Fprintf(os.Stderr, colorize(ansiRed, "eval error: ")+"%v\n", err)
			return false
		}
		fmt.Printf("%v\n", result)

	case "cookies":
		browserListCookies(page)

	case "status":
		browserPrintStatus(page)

	// -----------------------------------------------------------------------
	// Shell control
	// -----------------------------------------------------------------------

	case "help":
		fmt.Print(browserShellHelpText())

	case "clear":
		fmt.Print("\033[2J\033[H")

	case "exit", "quit":
		return true

	default:
		fmt.Fprintf(os.Stderr, colorize(ansiYellow, "Unknown command %q")+
			" — type 'help' for commands\n", cmd)
	}

	return false
}

// ---------------------------------------------------------------------------
// scroll helper
// ---------------------------------------------------------------------------

// browserScrollPage handles the scroll command variants:
//
//	scroll [down|up] [pixels]   — scroll by pixels (default 300)
//	scroll bottom               — jump to page bottom via JS
func browserScrollPage(page playwright.Page, args []string) {
	direction := "down"
	pixels := 300

	if len(args) > 0 {
		switch args[0] {
		case "bottom":
			if _, err := page.Evaluate("window.scrollTo(0, document.body.scrollHeight)"); err != nil {
				fmt.Fprintf(os.Stderr, colorize(ansiRed, "scroll error: ")+"%v\n", err)
				return
			}
			fmt.Println("scrolled to bottom")
			return
		case "up":
			direction = "up"
			if len(args) > 1 {
				if px, ok := parseScrollPixels(args[1]); ok {
					pixels = px
				}
			}
		case "down":
			direction = "down"
			if len(args) > 1 {
				if px, ok := parseScrollPixels(args[1]); ok {
					pixels = px
				}
			}
		default:
			// Bare number: scroll down by that many pixels.
			if px, ok := parseScrollPixels(args[0]); ok {
				pixels = px
			}
		}
	}

	deltaY := float64(pixels)
	if direction == "up" {
		deltaY = -deltaY
	}
	if err := page.Mouse().Wheel(0, deltaY); err != nil {
		fmt.Fprintf(os.Stderr, colorize(ansiRed, "scroll error: ")+"%v\n", err)
		return
	}
	fmt.Printf("scrolled %s %dpx\n", direction, pixels)
}

// ---------------------------------------------------------------------------
// extraction helpers
// ---------------------------------------------------------------------------

func browserExtractText(page playwright.Page, selector string) {
	elements, err := page.QuerySelectorAll(selector)
	if err != nil {
		fmt.Fprintf(os.Stderr, colorize(ansiRed, "text error: ")+"%v\n", err)
		return
	}
	if len(elements) == 0 {
		fmt.Printf("no elements matched %q\n", selector)
		return
	}
	fmt.Printf("found %d match(es) for %q:\n", len(elements), selector)
	for i, el := range elements {
		txt, err := el.TextContent()
		if err != nil {
			continue
		}
		txt = strings.TrimSpace(txt)
		if txt != "" {
			fmt.Printf("  [%d] %s\n", i+1, txt)
		}
	}
}

func browserExtractAttr(page playwright.Page, selector, attr string) {
	elements, err := page.QuerySelectorAll(selector)
	if err != nil {
		fmt.Fprintf(os.Stderr, colorize(ansiRed, "attr error: ")+"%v\n", err)
		return
	}
	if len(elements) == 0 {
		fmt.Printf("no elements matched %q\n", selector)
		return
	}
	fmt.Printf("found %d match(es) for %q[%s]:\n", len(elements), selector, attr)
	for i, el := range elements {
		val, err := el.GetAttribute(attr)
		if err != nil || val == "" {
			continue
		}
		fmt.Printf("  [%d] %s\n", i+1, val)
	}
}

func browserPrintHTML(page playwright.Page, selector string) {
	if selector == "" {
		content, err := page.Content()
		if err != nil {
			fmt.Fprintf(os.Stderr, colorize(ansiRed, "html error: ")+"%v\n", err)
			return
		}
		fmt.Println(content)
		return
	}

	el, err := page.QuerySelector(selector)
	if err != nil || el == nil {
		fmt.Fprintf(os.Stderr, colorize(ansiYellow, "html: no element matched %q\n"), selector)
		return
	}
	inner, err := el.InnerHTML()
	if err != nil {
		fmt.Fprintf(os.Stderr, colorize(ansiRed, "html error: ")+"%v\n", err)
		return
	}
	fmt.Println(inner)
}

func browserListLinks(page playwright.Page, scope string) {
	sel := "a[href]"
	if scope != "" {
		sel = scope + " a[href]"
	}
	elements, err := page.QuerySelectorAll(sel)
	if err != nil {
		fmt.Fprintf(os.Stderr, colorize(ansiRed, "links error: ")+"%v\n", err)
		return
	}
	if len(elements) == 0 {
		fmt.Println("no links found")
		return
	}
	fmt.Printf("found %d link(s):\n", len(elements))
	for i, el := range elements {
		href, _ := el.GetAttribute("href")
		text, _ := el.TextContent()
		text = strings.TrimSpace(text)
		if text == "" {
			text = "(no text)"
		}
		if href == "" {
			continue
		}
		fmt.Printf("  [%d] %s  %s\n", i+1, colorize(ansiGreen, href), text)
	}
}

func browserCountElements(page playwright.Page, selector string) {
	elements, err := page.QuerySelectorAll(selector)
	if err != nil {
		fmt.Fprintf(os.Stderr, colorize(ansiRed, "count error: ")+"%v\n", err)
		return
	}
	fmt.Printf("%d element(s) match %q\n", len(elements), selector)
}

// ---------------------------------------------------------------------------
// utility helpers
// ---------------------------------------------------------------------------

func browserTakeScreenshot(page playwright.Page, filename string) {
	if _, err := page.Screenshot(playwright.PageScreenshotOptions{
		Path:     playwright.String(filename),
		FullPage: playwright.Bool(false),
	}); err != nil {
		fmt.Fprintf(os.Stderr, colorize(ansiRed, "screenshot error: ")+"%v\n", err)
		return
	}
	fmt.Printf("screenshot saved: %s\n", colorize(ansiGreen, filename))
}

func browserListCookies(page playwright.Page) {
	ctx := page.Context()
	cookies, err := ctx.Cookies()
	if err != nil {
		fmt.Fprintf(os.Stderr, colorize(ansiRed, "cookies error: ")+"%v\n", err)
		return
	}
	if len(cookies) == 0 {
		fmt.Println("no cookies set")
		return
	}
	fmt.Printf("%d cookie(s):\n", len(cookies))
	for _, c := range cookies {
		expires := "(session)"
		if c.Expires > 0 {
			t := time.Unix(int64(c.Expires), 0)
			expires = t.Format("2006-01-02 15:04:05")
		}
		fmt.Printf("  %-30s = %-40s  (domain: %s, expires: %s)\n",
			c.Name, truncate(c.Value, 40), c.Domain, expires)
	}
}

func browserPrintStatus(page playwright.Page) {
	u := page.URL()
	title, _ := page.Title()

	// Count approximate content size via JS evaluation.
	sizeResult, _ := page.Evaluate("() => document.documentElement.outerHTML.length")
	size := 0
	if n, ok := sizeResult.(float64); ok {
		size = int(n)
	}

	fmt.Printf("url:   %s\n", colorize(ansiGreen, u))
	fmt.Printf("title: %s\n", title)
	fmt.Printf("size:  %d bytes (approx)\n", size)
}

// truncate shortens s to maxLen runes, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}
