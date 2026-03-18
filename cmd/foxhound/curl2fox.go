package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/fetch"
)

// cmdCurl2Fox converts a curl command to foxhound Go code.
func cmdCurl2Fox(args []string) {
	fs := flag.NewFlagSet("curl2fox", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: foxhound curl2fox <curl command...>")
		fmt.Fprintln(os.Stderr, "       foxhound curl2fox 'curl https://example.com -H \"Accept: text/html\"'")
		fmt.Fprintln(os.Stderr, "\nConverts a curl command to foxhound Go code.")
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		fs.Usage()
		os.Exit(1)
	}

	// Join all args into the curl command string.
	curlCmd := strings.Join(remaining, " ")
	parsed := parseCurl(curlCmd)
	code := generateCode(parsed)
	fmt.Println(code)
}

// curlParsed holds the parsed components of a curl command.
type curlParsed struct {
	URL     string
	Method  string
	Headers http.Header
	Data    string
	IsJSON  bool
}

// parseCurl extracts URL, method, headers, and body from a curl command string.
func parseCurl(cmd string) *curlParsed {
	p := &curlParsed{
		Method:  "GET",
		Headers: make(http.Header),
	}

	// Remove 'curl' prefix if present.
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, "curl ") {
		cmd = cmd[5:]
	} else if cmd == "curl" {
		return p
	}

	// Tokenize respecting quotes.
	tokens := tokenize(cmd)

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]

		switch {
		case tok == "-X" || tok == "--request":
			if i+1 < len(tokens) {
				i++
				p.Method = strings.ToUpper(tokens[i])
			}
		case tok == "-H" || tok == "--header":
			if i+1 < len(tokens) {
				i++
				header := tokens[i]
				parts := strings.SplitN(header, ":", 2)
				if len(parts) == 2 {
					key := strings.TrimSpace(parts[0])
					val := strings.TrimSpace(parts[1])
					p.Headers.Add(key, val)
				}
			}
		case tok == "-d" || tok == "--data" || tok == "--data-raw" || tok == "--data-binary":
			if i+1 < len(tokens) {
				i++
				p.Data = tokens[i]
				if p.Method == "GET" {
					p.Method = "POST"
				}
			}
		case tok == "--json":
			if i+1 < len(tokens) {
				i++
				p.Data = tokens[i]
				p.IsJSON = true
				if p.Method == "GET" {
					p.Method = "POST"
				}
				p.Headers.Set("Content-Type", "application/json")
			}
		case tok == "-I" || tok == "--head":
			p.Method = "HEAD"
		case tok == "-L" || tok == "--location":
			// Foxhound follows redirects by default via middleware.
		case tok == "-k" || tok == "--insecure":
			// Noted but not directly used.
		case tok == "-o" || tok == "--output":
			if i+1 < len(tokens) {
				i++ // Skip the output file.
			}
		case tok == "-s" || tok == "--silent":
			// No-op for code generation.
		case tok == "-v" || tok == "--verbose":
			// No-op for code generation.
		case tok == "--compressed":
			// Foxhound handles compression automatically.
		case tok == "-A" || tok == "--user-agent":
			if i+1 < len(tokens) {
				i++
				p.Headers.Set("User-Agent", tokens[i])
			}
		case tok == "-b" || tok == "--cookie":
			if i+1 < len(tokens) {
				i++
				p.Headers.Set("Cookie", tokens[i])
			}
		case tok == "-e" || tok == "--referer":
			if i+1 < len(tokens) {
				i++
				p.Headers.Set("Referer", tokens[i])
			}
		case !strings.HasPrefix(tok, "-"):
			// This is likely the URL.
			if p.URL == "" {
				p.URL = tok
			}
		}
	}

	// Detect if content type suggests JSON.
	if ct := p.Headers.Get("Content-Type"); strings.Contains(ct, "json") {
		p.IsJSON = true
	}

	return p
}

// generateCode produces foxhound Go code from a parsed curl command.
func generateCode(p *curlParsed) string {
	var b strings.Builder

	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"context\"\n")
	b.WriteString("\t\"fmt\"\n")
	if p.Method != "GET" || p.Data != "" {
		b.WriteString("\t\"net/http\"\n")
	}
	b.WriteString("\t\"time\"\n\n")
	b.WriteString("\tfoxhound \"github.com/sadewadee/foxhound\"\n")
	b.WriteString("\t\"github.com/sadewadee/foxhound/engine\"\n")
	b.WriteString("\t\"github.com/sadewadee/foxhound/fetch\"\n")
	b.WriteString("\t\"github.com/sadewadee/foxhound/identity\"\n")
	b.WriteString("\t\"github.com/sadewadee/foxhound/queue\"\n")
	b.WriteString("\t_ \"github.com/sadewadee/foxhound/parse\"\n")
	b.WriteString(")\n\n")

	b.WriteString("func main() {\n")
	b.WriteString("\tctx := context.Background()\n\n")

	// Identity.
	b.WriteString("\t// Generate identity profile for anti-detection.\n")
	b.WriteString("\tprof := identity.Generate(\n")
	b.WriteString("\t\tidentity.WithBrowser(identity.BrowserFirefox),\n")
	b.WriteString("\t\tidentity.WithOS(identity.OSWindows),\n")
	b.WriteString("\t)\n\n")

	// Fetcher.
	b.WriteString("\t// Create the stealth HTTP fetcher.\n")
	b.WriteString("\tfetcher := fetch.NewStealth(fetch.WithIdentity(prof))\n")
	b.WriteString("\tsmartFetcher := fetch.NewSmart(fetcher, nil)\n\n")

	// Job.
	b.WriteString("\t// Create the job.\n")
	fmt.Fprintf(&b, "\tjob := &foxhound.Job{\n")
	fmt.Fprintf(&b, "\t\tID:        %q,\n", p.URL)
	fmt.Fprintf(&b, "\t\tURL:       %q,\n", p.URL)
	fmt.Fprintf(&b, "\t\tMethod:    %q,\n", p.Method)
	b.WriteString("\t\tFetchMode: foxhound.FetchAuto,\n")
	b.WriteString("\t\tPriority:  foxhound.PriorityHigh,\n")
	b.WriteString("\t\tCreatedAt: time.Now(),\n")

	// Parse domain from URL.
	if u, err := url.Parse(p.URL); err == nil {
		fmt.Fprintf(&b, "\t\tDomain:    %q,\n", u.Host)
	}

	// Headers.
	if len(p.Headers) > 0 {
		b.WriteString("\t\tHeaders:   http.Header{\n")
		for key, vals := range p.Headers {
			for _, val := range vals {
				fmt.Fprintf(&b, "\t\t\t%q: {%q},\n", key, val)
			}
		}
		b.WriteString("\t\t},\n")
	}

	// Body.
	if p.Data != "" {
		fmt.Fprintf(&b, "\t\tBody:      []byte(%q),\n", p.Data)
	}

	b.WriteString("\t}\n\n")

	// Processor.
	b.WriteString("\t// Define the processor — extract data from responses.\n")
	b.WriteString("\tprocessor := foxhound.ProcessorFunc(func(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {\n")
	b.WriteString("\t\titem := foxhound.NewItem()\n")
	b.WriteString("\t\titem.Set(\"url\", resp.URL)\n")
	b.WriteString("\t\titem.Set(\"status\", resp.StatusCode)\n")
	b.WriteString("\t\titem.Set(\"title\", resp.CSS(\"title\").Text())\n")
	b.WriteString("\t\titem.URL = resp.URL\n\n")
	b.WriteString("\t\tfmt.Printf(\"Scraped: %s (status %d)\\n\", resp.URL, resp.StatusCode)\n\n")
	b.WriteString("\t\treturn &foxhound.Result{Items: []*foxhound.Item{item}}, nil\n")
	b.WriteString("\t})\n\n")

	// Hunt.
	b.WriteString("\t// Run the hunt.\n")
	b.WriteString("\th := engine.NewHunt(engine.HuntConfig{\n")
	fmt.Fprintf(&b, "\t\tName:      %q,\n", "curl-import")
	b.WriteString("\t\tWalkers:   1,\n")
	b.WriteString("\t\tSeeds:     []*foxhound.Job{job},\n")
	b.WriteString("\t\tProcessor: processor,\n")
	b.WriteString("\t\tFetcher:   smartFetcher,\n")
	b.WriteString("\t\tQueue:     queue.NewMemoryQueue(),\n")
	b.WriteString("\t})\n\n")
	b.WriteString("\tif err := h.Run(ctx); err != nil {\n")
	b.WriteString("\t\tfmt.Printf(\"Error: %v\\n\", err)\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n")

	return b.String()
}

// tokenize splits a command string respecting single and double quotes.
func tokenize(s string) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for _, ch := range s {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}

		switch {
		case ch == '\\' && !inSingle:
			escaped = true
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
		case ch == '"' && !inSingle:
			inDouble = !inDouble
		case (ch == ' ' || ch == '\t') && !inSingle && !inDouble:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// cmdPreview opens a URL in a headless browser and outputs the HTML.
// This is useful for debugging what the browser fetcher sees.
func cmdPreview(args []string) {
	fs := flag.NewFlagSet("preview", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: foxhound preview <url>")
		fmt.Fprintln(os.Stderr, "\nFetches a URL using the stealth HTTP client and prints the response body.")
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		fs.Usage()
		os.Exit(1)
	}

	targetURL := remaining[0]
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}

	// Create a quick fetcher.
	fetcher := fetchForPreview()
	defer fetcher.Close()

	job := &foxhound.Job{
		ID:        targetURL,
		URL:       targetURL,
		Method:    "GET",
		FetchMode: foxhound.FetchStatic,
		CreatedAt: timeNow(),
	}

	resp, err := fetcher.Fetch(context.Background(), job)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching %s: %v\n", targetURL, err)
		os.Exit(1)
	}

	fmt.Printf("Status: %d\n", resp.StatusCode)
	fmt.Printf("URL: %s\n", resp.URL)
	fmt.Printf("Duration: %s\n", resp.Duration)
	fmt.Printf("Mode: %s\n", resp.FetchMode)
	fmt.Println("---")
	fmt.Println(string(resp.Body))
}

// fetchForPreview creates a simple stealth fetcher for the preview command.
func fetchForPreview() foxhound.Fetcher {
	return fetch.NewStealth()
}

// timeNow is indirected for testing.
var timeNow = func() time.Time { return time.Now() }
