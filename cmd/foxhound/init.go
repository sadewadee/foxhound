package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// cmdInit scaffolds a new Foxhound project at the given name/path.
func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: foxhound init [flags] <project-name>")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	name := fs.Arg(0)
	if name == "" {
		fmt.Fprintln(os.Stderr, "Error: project name is required")
		fs.Usage()
		os.Exit(1)
	}

	dir := name
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("creating project directory", "dir", dir, "error", err)
		os.Exit(1)
	}

	files := map[string]string{
		"go.mod":       goModTemplate(name),
		"main.go":      mainGoTemplate(name),
		"config.yaml":  configYAMLTemplate(name),
		".env.example": envExampleTemplate(),
	}

	for filename, content := range files {
		path := filepath.Join(dir, filename)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			slog.Error("writing file", "path", path, "error", err)
			os.Exit(1)
		}
		fmt.Printf("  created  %s\n", filepath.Join(name, filename))
	}

	fmt.Printf("\nProject %q created successfully.\n\n", name)
	fmt.Printf("Next steps:\n")
	fmt.Printf("  cd %s\n", name)
	fmt.Printf("  go mod tidy\n")
	fmt.Printf("  foxhound run --config config.yaml\n")
}

func goModTemplate(name string) string {
	// Derive a module path from the project name (lowercase, replace spaces).
	module := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	return fmt.Sprintf(`module %s

go 1.23

require github.com/foxhound-scraper/foxhound v0.1.0
`, module)
}

func mainGoTemplate(name string) string {
	return fmt.Sprintf(`package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/parse"
	"github.com/foxhound-scraper/foxhound/pipeline"
	"github.com/foxhound-scraper/foxhound/pipeline/export"
)

// %[1]sSpider implements the scraping logic for this hunt.
type %[1]sSpider struct{}

// Process extracts data from each response.
func (s *%[1]sSpider) Process(ctx context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
	doc, err := parse.NewDocument(resp)
	if err != nil {
		return nil, err
	}

	item := foxhound.NewItem()
	item.URL = resp.URL
	item.Set("title", doc.Text("h1"))
	item.Set("url", resp.URL)

	return &foxhound.Result{Items: []*foxhound.Item{item}}, nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	cfg, err := foxhound.LoadConfig("config.yaml")
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}

	// Build pipeline.
	jsonWriter, err := export.NewJSON("output.jsonl", export.JSONLines)
	if err != nil {
		slog.Error("creating JSON writer", "error", err)
		os.Exit(1)
	}
	defer jsonWriter.Close()

	_ = pipeline.NewChain(
		&pipeline.Validate{Required: []string{"title", "url"}},
	)
	_ = cfg

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	slog.Info("foxhound starting", "hunt", cfg.Hunt.Domain)
	<-ctx.Done()
	slog.Info("foxhound stopped")
}
`, strings.Title(strings.ReplaceAll(name, "-", " "))) //nolint:staticcheck
}

func configYAMLTemplate(name string) string {
	return fmt.Sprintf(`# Foxhound configuration for %s
# See https://github.com/foxhound-scraper/foxhound for full documentation.

hunt:
  domain: example.com
  walkers: 3

identity:
  browser: firefox
  os: [windows, macos, linux]
  fingerprint_db: embedded

proxy:
  rotation: per_session
  cooldown: 30m
  max_requests_per_proxy: 100

fetch:
  static:
    timeout: 30s
    tls_impersonate: true
  browser:
    timeout: 60s
    headless: "new"
    instances: 2
    block_images: true
    block_webrtc: true

middleware:
  ratelimit:
    enabled: true
    requests_per_sec: 2.0
    burst_size: 5
  dedup:
    strategy: url_canonical
    store: memory
  depth_limit:
    max: 5

pipeline:
  - validate:
      required: [title, url]
  - export:
      - type: jsonl
        path: output/${FOXHOUND_RUN_ID}.jsonl
      - type: csv
        path: output/${FOXHOUND_RUN_ID}.csv

queue:
  backend: memory

logging:
  level: info
  format: json
  output: stderr
`, name)
}

func envExampleTemplate() string {
	return `# Foxhound environment variables
# Copy this file to .env and fill in your values.
# Never commit .env to version control.

# Run identifier (auto-generated if not set)
# FOXHOUND_RUN_ID=my-run-001

# Proxy provider credentials
# BRIGHTDATA_API_KEY=
# OXYLABS_USERNAME=
# OXYLABS_PASSWORD=

# Redis (if using redis queue/cache)
# REDIS_URL=redis://localhost:6379/0

# PostgreSQL (if using postgres queue/export)
# DATABASE_URL=postgres://user:pass@localhost:5432/foxhound?sslmode=disable

# Captcha solvers (optional)
# CAPSOLVER_API_KEY=
# TWOCAPTCHA_API_KEY=
`
}
