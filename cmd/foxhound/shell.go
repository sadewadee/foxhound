package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/identity"
	"github.com/sadewadee/foxhound/parse"
	"github.com/sadewadee/foxhound/proxy"
)

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
			shellFetch(client, cmdArgs[0], false)

		case "headers":
			if len(cmdArgs) == 0 {
				fmt.Fprintln(os.Stderr, "Usage: headers <url>")
				continue
			}
			shellFetch(client, cmdArgs[0], true)

		case "parse":
			if len(cmdArgs) < 2 {
				fmt.Fprintln(os.Stderr, "Usage: parse <url> <css-selector>")
				continue
			}
			shellParse(client, cmdArgs[0], cmdArgs[1])

		case "proxy":
			fmt.Printf("Proxy pool: %d proxies\n", pool.Len())

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
  identity              Generate and display a new identity profile
  fetch <url>           Fetch a URL and show status, size, and duration
  headers <url>         Fetch a URL and display response headers
  parse <url> <sel>     Fetch a URL and extract elements matching CSS selector
  proxy                 Show proxy pool status
  help                  Show this help message
  exit / quit           Exit the shell
`
}

// shellFetch performs an HTTP GET and prints the response summary.
// If headersOnly is true, it also prints all response headers.
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
