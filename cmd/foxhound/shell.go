package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/identity"
	"github.com/sadewadee/foxhound/parse"
	"github.com/sadewadee/foxhound/proxy"
)

// ANSI color codes for terminal output.
const (
	ansiReset  = "\033[0m"
	ansiGreen  = "\033[32m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiBold   = "\033[1m"
)

func colorize(color, text string) string {
	return color + text + ansiReset
}

// cmdShell starts an interactive scraping REPL.
func cmdShell(args []string) {
	fs := flag.NewFlagSet("shell", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: foxhound shell [flags]")
		fmt.Fprintln(os.Stderr, "\nStart an interactive scraping shell.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}

	configPath := fs.String("config", "config.yaml", "path to configuration file")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// Load config if present; errors are non-fatal in shell mode.
	var pool *proxy.Pool
	if cfg, err := foxhound.LoadConfig(*configPath); err == nil {
		var providers []proxy.Provider
		for _, entry := range cfg.Proxy.Providers {
			if entry.Type == "static" && len(entry.List) > 0 {
				providers = append(providers, proxy.Static(entry.List))
			}
		}
		pool = proxy.NewPool(providers...)
	} else {
		pool = proxy.NewPool()
	}
	defer pool.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	session := newShellSession()

	fmt.Println("Foxhound Interactive Shell")
	fmt.Println("Type 'help' for available commands, 'exit' to quit.")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("foxhound> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		cmd := parts[0]
		cmdArgs := parts[1:]

		switch cmd {
		case "help":
			fmt.Print(shellHelpText())

		case "identity":
			profile := identity.Generate()
			fmt.Printf("Browser    : %s %s\n", profile.BrowserName, profile.BrowserVer)
			fmt.Printf("OS         : %s %s\n", profile.OS, profile.OSVersion)
			fmt.Printf("User-Agent : %s\n", profile.UA)
			fmt.Printf("TLS Profile: %s\n", profile.TLSProfile)
			fmt.Printf("Locale     : %s\n", profile.Locale)
			fmt.Printf("Timezone   : %s\n", profile.Timezone)
			fmt.Printf("Screen     : %dx%d\n", profile.ScreenW, profile.ScreenH)

		case "fetch":
			if len(cmdArgs) == 0 {
				fmt.Fprintln(os.Stderr, "Usage: fetch <url>")
				continue
			}
			shellFetchSession(client, session, cmdArgs[0], false)

		case "headers":
			if len(cmdArgs) == 0 {
				fmt.Fprintln(os.Stderr, "Usage: headers <url>")
				continue
			}
			shellFetchSession(client, session, cmdArgs[0], true)

		case "parse":
			if len(cmdArgs) < 2 {
				fmt.Fprintln(os.Stderr, "Usage: parse <url> <css-selector>")
				continue
			}
			shellParse(client, cmdArgs[0], cmdArgs[1])

		case "proxy":
			fmt.Printf("Proxy pool: %d proxies\n", pool.Len())

		// --- New commands ---

		case "adaptive":
			// adaptive <url> <selector> — fetch URL, apply adaptive selector.
			if len(cmdArgs) < 2 {
				fmt.Fprintln(os.Stderr, colorize(ansiYellow, "Usage: adaptive <url> <selector>"))
				continue
			}
			shellAdaptive(client, session, cmdArgs[0], cmdArgs[1])

		case "extract":
			// extract <url> key1=selector1 key2=selector2 ...
			if len(cmdArgs) < 2 {
				fmt.Fprintln(os.Stderr, colorize(ansiYellow, "Usage: extract <url> key1=selector1 key2=selector2 ..."))
				continue
			}
			shellExtract(client, session, cmdArgs[0], cmdArgs[1:])

		case "export":
			// export <format> [path]
			if len(cmdArgs) == 0 {
				fmt.Fprintln(os.Stderr, colorize(ansiYellow, "Usage: export <format> [path]  (formats: json, csv, markdown, text)"))
				continue
			}
			format := cmdArgs[0]
			path := ""
			if len(cmdArgs) > 1 {
				path = cmdArgs[1]
			}
			shellExport(session, format, path)

		case "history":
			// history — show last 10 fetched URLs.
			shellHistory(session)

		case "timing":
			// timing — show latency stats.
			shellTiming(session)

		case "compare":
			// compare <url1> <url2>
			if len(cmdArgs) < 2 {
				fmt.Fprintln(os.Stderr, colorize(ansiYellow, "Usage: compare <url1> <url2>"))
				continue
			}
			shellCompare(client, cmdArgs[0], cmdArgs[1])

		case "status":
			// status — show session stats.
			shellStatus(session, pool)

		case "exit", "quit":
			fmt.Println("Goodbye.")
			return

		default:
			fmt.Fprintf(os.Stderr, "Unknown command %q — type 'help' for commands\n", cmd)
		}
	}
}

// shellHelpText returns the help string for the shell REPL.
// It is a standalone function so tests can call it without blocking on stdin.
func shellHelpText() string {
	return `Available commands:
  identity                          Generate and display a new identity profile
  fetch <url>                       Fetch a URL and show status, size, and duration
  headers <url>                     Fetch a URL and display response headers
  parse <url> <sel>                 Fetch a URL and extract elements matching CSS selector
  adaptive <url> <sel>              Fetch URL, apply adaptive selector, show results
  extract <url> k1=sel1 k2=sel2     Structured multi-field extraction
  export <format> [path]            Export last fetch (json, csv, markdown, text)
  history                           Show last 10 fetched URLs with status/duration
  timing                            Show avg/min/max latency stats for this session
  compare <url1> <url2>             Diff two pages and show unique selectors summary
  status                            Show current session stats
  proxy                             Show proxy pool status
  help                              Show this help message
  exit / quit                       Exit the shell
`
}

// shellFetchSession performs an HTTP GET, records the result in the session,
// and prints the response summary.  If headersOnly is true response headers
// are also printed.
func shellFetchSession(client *http.Client, session *shellSession, rawURL string, headersOnly bool) {
	start := time.Now()
	resp, err := client.Get(rawURL) //nolint:noctx
	if err != nil {
		fmt.Fprintf(os.Stderr, colorize(ansiRed, "fetch error: ")+"%v\n", err)
		return
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	elapsed := time.Since(start)
	elapsedMS := elapsed.Milliseconds()

	session.RecordFetch(rawURL, resp.StatusCode, buf.Len(), elapsedMS)
	session.SetLastBody(buf.Bytes())

	statusText := fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	if resp.StatusCode >= 400 {
		statusText = colorize(ansiRed, statusText)
	} else {
		statusText = colorize(ansiGreen, statusText)
	}

	fmt.Printf("Status  : %s\n", statusText)
	fmt.Printf("Size    : %d bytes\n", buf.Len())
	fmt.Printf("Duration: %s\n", elapsed.Round(time.Millisecond))

	if headersOnly {
		fmt.Println("\nHeaders:")
		for k, vs := range resp.Header {
			fmt.Printf("  %s: %s\n", k, strings.Join(vs, ", "))
		}
	}
}

// shellFetch is kept for backward-compatibility with code that calls it directly
// (e.g., existing tests via the old call site in cmdShell).
func shellFetch(client *http.Client, rawURL string, headersOnly bool) {
	start := time.Now()
	resp, err := client.Get(rawURL) //nolint:noctx
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	elapsed := time.Since(start)

	fmt.Printf("Status  : %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
	fmt.Printf("Size    : %d bytes\n", buf.Len())
	fmt.Printf("Duration: %s\n", elapsed.Round(time.Millisecond))

	if headersOnly {
		fmt.Println("\nHeaders:")
		for k, vs := range resp.Header {
			fmt.Printf("  %s: %s\n", k, strings.Join(vs, ", "))
		}
	}
}

// shellParse fetches a URL and extracts text from elements matching the CSS selector.
func shellParse(client *http.Client, rawURL, selector string) {
	resp, err := client.Get(rawURL) //nolint:noctx
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse fetch error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)

	fResp := &foxhound.Response{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       buf.Bytes(),
		URL:        rawURL,
	}

	doc, err := parse.NewDocument(fResp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		return
	}

	texts := doc.Texts(selector)
	if len(texts) == 0 {
		fmt.Printf("No elements matched selector %q\n", selector)
		return
	}
	fmt.Printf("Found %d match(es) for %q:\n", len(texts), selector)
	for i, t := range texts {
		t = strings.TrimSpace(t)
		if t != "" {
			fmt.Printf("  [%d] %s\n", i+1, t)
		}
	}
}

// shellAdaptive fetches a URL and applies a CSS selector, showing results.
// The "adaptive" behaviour here means it tries the given selector then falls
// back to the nearest text-containing parent if nothing matches.
func shellAdaptive(client *http.Client, session *shellSession, rawURL, selector string) {
	resp, err := client.Get(rawURL) //nolint:noctx
	if err != nil {
		fmt.Fprintf(os.Stderr, colorize(ansiRed, "adaptive fetch error: ")+"%v\n", err)
		return
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	session.SetLastBody(buf.Bytes())

	fResp := &foxhound.Response{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       buf.Bytes(),
		URL:        rawURL,
	}

	doc, err := parse.NewDocument(fResp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "adaptive parse error: %v\n", err)
		return
	}

	texts := doc.Texts(selector)
	if len(texts) > 0 {
		fmt.Printf(colorize(ansiGreen, "Selector %q matched %d element(s):\n"), selector, len(texts))
		for i, t := range texts {
			t = strings.TrimSpace(t)
			if t != "" {
				fmt.Printf("  [%d] %s\n", i+1, t)
			}
		}
		return
	}

	// Fallback: try stripping the last combinator segment.
	parts := strings.Fields(selector)
	if len(parts) > 1 {
		fallback := strings.Join(parts[:len(parts)-1], " ")
		texts = doc.Texts(fallback)
		if len(texts) > 0 {
			fmt.Printf(colorize(ansiYellow, "Exact selector had no match; fallback %q matched %d element(s):\n"), fallback, len(texts))
			for i, t := range texts {
				t = strings.TrimSpace(t)
				if t != "" {
					fmt.Printf("  [%d] %s\n", i+1, t)
				}
			}
			return
		}
	}

	fmt.Println(colorize(ansiRed, "No elements matched selector or its fallback."))
}

// shellExtract fetches a URL and extracts multiple fields by CSS selector.
// Each arg must be in the form key=selector.
func shellExtract(client *http.Client, session *shellSession, rawURL string, pairs []string) {
	resp, err := client.Get(rawURL) //nolint:noctx
	if err != nil {
		fmt.Fprintf(os.Stderr, colorize(ansiRed, "extract fetch error: ")+"%v\n", err)
		return
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	session.SetLastBody(buf.Bytes())

	fResp := &foxhound.Response{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       buf.Bytes(),
		URL:        rawURL,
	}

	doc, err := parse.NewDocument(fResp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "extract parse error: %v\n", err)
		return
	}

	fmt.Printf(ansiBold+"Extraction results for %s:"+ansiReset+"\n", rawURL)
	for _, pair := range pairs {
		idx := strings.IndexByte(pair, '=')
		if idx < 1 {
			fmt.Fprintf(os.Stderr, colorize(ansiYellow, "  skipping malformed pair %q (expected key=selector)\n"), pair)
			continue
		}
		key := pair[:idx]
		sel := pair[idx+1:]
		texts := doc.Texts(sel)
		if len(texts) == 0 {
			fmt.Printf("  %-20s %s\n", key+":", colorize(ansiYellow, "(no match)"))
		} else {
			fmt.Printf("  %-20s %s\n", key+":", colorize(ansiGreen, strings.TrimSpace(texts[0])))
			for _, extra := range texts[1:] {
				extra = strings.TrimSpace(extra)
				if extra != "" {
					fmt.Printf("  %-20s %s\n", "", extra)
				}
			}
		}
	}
}

// shellExport writes the last fetched body to a file in the requested format.
func shellExport(session *shellSession, format, path string) {
	body := session.LastBody()
	if body == nil {
		fmt.Fprintln(os.Stderr, colorize(ansiYellow, "No fetch result in session — run 'fetch <url>' first."))
		return
	}

	var out string
	switch strings.ToLower(format) {
	case "json":
		// Attempt to pretty-print if already JSON; otherwise wrap in an object.
		var v interface{}
		if json.Unmarshal(body, &v) == nil {
			b, _ := json.MarshalIndent(v, "", "  ")
			out = string(b)
		} else {
			out = fmt.Sprintf(`{"body":%s}`, string(body))
		}
	case "csv":
		var sb strings.Builder
		w := csv.NewWriter(&sb)
		_ = w.Write([]string{"body"})
		_ = w.Write([]string{string(body)})
		w.Flush()
		out = sb.String()
	case "markdown", "md":
		out = "# Exported Content\n\n```\n" + string(body) + "\n```\n"
	case "text", "txt":
		out = string(body)
	default:
		fmt.Fprintf(os.Stderr, colorize(ansiRed, "Unknown format %q — use: json, csv, markdown, text\n"), format)
		return
	}

	if path == "" {
		fmt.Println(out)
		return
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, colorize(ansiRed, "export write error: ")+"%v\n", err)
		return
	}
	fmt.Printf(colorize(ansiGreen, "Exported %d bytes to %s\n"), len(out), path)
}

// shellHistory prints the last 10 fetched URLs.
func shellHistory(session *shellSession) {
	entries := session.History()
	if len(entries) == 0 {
		fmt.Println(colorize(ansiYellow, "No fetches in this session yet."))
		return
	}
	fmt.Println(ansiBold + "Fetch history:" + ansiReset)
	for i, e := range entries {
		statusColor := ansiGreen
		if e.Status >= 400 {
			statusColor = ansiRed
		}
		fmt.Printf("  [%2d] %s  %s  %d bytes  %dms\n",
			i+1,
			colorize(statusColor, fmt.Sprintf("%d", e.Status)),
			e.URL,
			e.Bytes,
			e.DurationMS,
		)
	}
}

// shellTiming prints latency statistics for this session.
func shellTiming(session *shellSession) {
	avg, min, max := session.TimingStats()
	entries := session.History()
	if len(entries) == 0 {
		fmt.Println(colorize(ansiYellow, "No timing data yet — run 'fetch <url>' first."))
		return
	}
	fmt.Println(ansiBold + "Latency stats:" + ansiReset)
	fmt.Printf("  Samples : %d\n", len(entries))
	fmt.Printf("  Avg     : %dms\n", avg)
	fmt.Printf("  Min     : %dms\n", min)
	fmt.Printf("  Max     : %dms\n", max)
}

// shellCompare fetches two URLs and prints a diff summary of their CSS selectors.
func shellCompare(client *http.Client, url1, url2 string) {
	fetch := func(rawURL string) (map[string]bool, error) {
		resp, err := client.Get(rawURL) //nolint:noctx
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)

		fResp := &foxhound.Response{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       buf.Bytes(),
			URL:        rawURL,
		}
		doc, err := parse.NewDocument(fResp)
		if err != nil {
			return nil, err
		}
		// Collect text from common structural selectors.
		selectors := []string{"h1", "h2", "h3", "title", "p", "a", "li", "span", "div"}
		tags := make(map[string]bool)
		for _, sel := range selectors {
			texts := doc.Texts(sel)
			for _, t := range texts {
				t = strings.TrimSpace(t)
				if t != "" {
					tags[t] = true
				}
			}
		}
		return tags, nil
	}

	tags1, err := fetch(url1)
	if err != nil {
		fmt.Fprintf(os.Stderr, colorize(ansiRed, "compare: error fetching %s: ")+"%v\n", url1, err)
		return
	}
	tags2, err := fetch(url2)
	if err != nil {
		fmt.Fprintf(os.Stderr, colorize(ansiRed, "compare: error fetching %s: ")+"%v\n", url2, err)
		return
	}

	// Find text unique to each page.
	var onlyIn1, onlyIn2 []string
	for t := range tags1 {
		if !tags2[t] {
			onlyIn1 = append(onlyIn1, t)
		}
	}
	for t := range tags2 {
		if !tags1[t] {
			onlyIn2 = append(onlyIn2, t)
		}
	}
	sort.Strings(onlyIn1)
	sort.Strings(onlyIn2)

	fmt.Printf(ansiBold+"Compare: %s vs %s"+ansiReset+"\n", url1, url2)
	fmt.Printf("  Common text nodes : %d\n", len(tags1)-len(onlyIn1))
	fmt.Printf("  Only in page 1    : %d\n", len(onlyIn1))
	for _, t := range onlyIn1 {
		if len(t) > 80 {
			t = t[:80] + "..."
		}
		fmt.Printf("    - %s\n", colorize(ansiGreen, t))
	}
	fmt.Printf("  Only in page 2    : %d\n", len(onlyIn2))
	for _, t := range onlyIn2 {
		if len(t) > 80 {
			t = t[:80] + "..."
		}
		fmt.Printf("    - %s\n", colorize(ansiRed, t))
	}
}

// shellStatus prints overall session statistics.
func shellStatus(session *shellSession, pool *proxy.Pool) {
	entries := session.History()
	var success, errors int
	var totalBytes int
	for _, e := range entries {
		if e.Status >= 200 && e.Status < 300 {
			success++
		} else if e.Status >= 400 {
			errors++
		}
		totalBytes += e.Bytes
	}
	avg, _, _ := session.TimingStats()

	fmt.Println(ansiBold + "Session status:" + ansiReset)
	fmt.Printf("  Total fetches : %d\n", len(entries))
	fmt.Printf("  Success (2xx) : %s\n", colorize(ansiGreen, fmt.Sprintf("%d", success)))
	fmt.Printf("  Errors (4xx+) : %s\n", colorize(ansiRed, fmt.Sprintf("%d", errors)))
	fmt.Printf("  Total bytes   : %d\n", totalBytes)
	fmt.Printf("  Avg latency   : %dms\n", avg)
	fmt.Printf("  Proxy pool    : %d proxies\n", pool.Len())
}
