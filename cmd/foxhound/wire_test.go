package main

// wire_test.go — RED-phase tests for engine wiring helpers introduced in
// run.go and resume.go.

import (
	"context"
	"os"
	"strings"
	"testing"

	foxhound "github.com/sadewadee/foxhound"
)

// ---------------------------------------------------------------------------
// buildQueue
// ---------------------------------------------------------------------------

func TestBuildQueueReturnsMemoryQueueForMemoryBackend(t *testing.T) {
	q, err := buildQueue("memory", "")
	if err != nil {
		t.Fatalf("unexpected error building memory queue: %v", err)
	}
	if q == nil {
		t.Fatal("expected non-nil queue for memory backend")
	}
	_ = q.Close()
}

func TestBuildQueueReturnsSQLiteQueueForFilePath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.db"
	q, err := buildQueue("sqlite", path)
	if err != nil {
		t.Fatalf("unexpected error building sqlite queue from path: %v", err)
	}
	if q == nil {
		t.Fatal("expected non-nil queue for sqlite backend")
	}
	_ = q.Close()
}

func TestBuildQueueReturnsSQLiteQueueForSQLiteURLScheme(t *testing.T) {
	dir := t.TempDir()
	url := "sqlite://" + dir + "/test2.db"
	q, err := buildQueue("sqlite", url)
	if err != nil {
		t.Fatalf("unexpected error building sqlite queue with sqlite:// URL: %v", err)
	}
	if q == nil {
		t.Fatal("expected non-nil queue for sqlite:// URL")
	}
	_ = q.Close()
}

func TestBuildQueueReturnsErrorForUnknownBackend(t *testing.T) {
	_, err := buildQueue("kafka", "")
	if err == nil {
		t.Fatal("expected error for unknown queue backend, got nil")
	}
}

// ---------------------------------------------------------------------------
// buildMiddlewares
// ---------------------------------------------------------------------------

func TestBuildMiddlewaresAlwaysIncludesDedupAndRetry(t *testing.T) {
	cfg := minimalConfig()
	mws := buildMiddlewares(cfg)
	// At minimum dedup + retry must be present.
	if len(mws) < 2 {
		t.Errorf("expected at least 2 middlewares (dedup + retry), got %d", len(mws))
	}
}

func TestBuildMiddlewaresIncludesRateLimitWhenEnabled(t *testing.T) {
	cfg := minimalConfig()
	cfg.Middleware.RateLimit.Enabled = true
	cfg.Middleware.RateLimit.RequestsPerSec = 2.0
	cfg.Middleware.RateLimit.BurstSize = 5
	mws := buildMiddlewares(cfg)
	// ratelimit + dedup + retry = 3
	if len(mws) < 3 {
		t.Errorf("expected at least 3 middlewares when ratelimit enabled, got %d", len(mws))
	}
}

func TestBuildMiddlewaresIncludesDepthLimitWhenMaxGreaterThanZero(t *testing.T) {
	cfg := minimalConfig()
	cfg.Middleware.DepthLimit.Max = 5
	mws := buildMiddlewares(cfg)
	// dedup + depth + retry = 3
	if len(mws) < 3 {
		t.Errorf("expected at least 3 middlewares when depth_limit.max > 0, got %d", len(mws))
	}
}

// ---------------------------------------------------------------------------
// buildPipelineStages
// ---------------------------------------------------------------------------

func TestBuildPipelineStagesReturnsEmptySlicesForNoPipeline(t *testing.T) {
	stages, writers := buildPipelineStages(nil, t.TempDir())
	if stages == nil {
		t.Fatal("expected non-nil stages slice (should be empty, not nil)")
	}
	if writers == nil {
		t.Fatal("expected non-nil writers slice (should be empty, not nil)")
	}
}

func TestBuildPipelineStagesCreatesValidateStageFromConfig(t *testing.T) {
	entries := []foxhound.PipelineEntry{
		{
			Validate: &foxhound.ValidateConfig{Required: []string{"url", "title"}},
		},
	}
	stages, _ := buildPipelineStages(entries, t.TempDir())
	if len(stages) != 1 {
		t.Errorf("expected 1 pipeline stage for validate entry, got %d", len(stages))
	}
}

func TestBuildPipelineStagesCreatesCleanStageFromConfig(t *testing.T) {
	entries := []foxhound.PipelineEntry{
		{
			Clean: &foxhound.CleanConfig{TrimWhitespace: true},
		},
	}
	stages, _ := buildPipelineStages(entries, t.TempDir())
	if len(stages) != 1 {
		t.Errorf("expected 1 pipeline stage for clean entry, got %d", len(stages))
	}
}

func TestBuildPipelineStagesCreatesJSONLWriterForExportEntry(t *testing.T) {
	dir := t.TempDir()
	entries := []foxhound.PipelineEntry{
		{
			Export: []foxhound.ExportConfig{
				{Type: "jsonl", Path: dir + "/out.jsonl"},
			},
		},
	}
	_, writers := buildPipelineStages(entries, dir)
	if len(writers) != 1 {
		t.Errorf("expected 1 writer for jsonl export, got %d", len(writers))
	}
	for _, w := range writers {
		_ = w.Close()
	}
}

func TestBuildPipelineStagesCreatesCSVWriterForExportEntry(t *testing.T) {
	dir := t.TempDir()
	entries := []foxhound.PipelineEntry{
		{
			Export: []foxhound.ExportConfig{
				{Type: "csv", Path: dir + "/out.csv"},
			},
		},
	}
	_, writers := buildPipelineStages(entries, dir)
	if len(writers) != 1 {
		t.Errorf("expected 1 writer for csv export, got %d", len(writers))
	}
	for _, w := range writers {
		_ = w.Close()
	}
}

// ---------------------------------------------------------------------------
// cmdRun --dry-run  (integration smoke test — no network)
// ---------------------------------------------------------------------------

func TestCmdRunDryRunValidatesConfigAndPrintsSummary(t *testing.T) {
	cfg := writeTempConfig(t, `
hunt:
  domain: example.com
  walkers: 2
queue:
  backend: memory
logging:
  level: info
  format: json
  output: stderr
fetch:
  static:
    timeout: 10s
  browser:
    timeout: 30s
    instances: 0
middleware:
  ratelimit:
    enabled: false
  depth_limit:
    max: 3
`)
	out := captureOutput(func() {
		cmdRun([]string{"--config", cfg, "--dry-run"})
	})
	if !strings.Contains(out, "example.com") {
		t.Errorf("expected domain in dry-run output, got:\n%s", out)
	}
	if !strings.Contains(out, "Dry run") {
		t.Errorf("expected 'Dry run' in output, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// cmdResume — persistent queue integration
// ---------------------------------------------------------------------------

func TestCmdResumeWithSQLiteQueueAndNoPendingJobsPrintsNoPending(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/resume_test.db"

	cfg := writeTempConfig(t, `
hunt:
  domain: example.com
  walkers: 1
queue:
  backend: sqlite
logging:
  level: info
  format: json
  output: stderr
fetch:
  static:
    timeout: 10s
  browser:
    timeout: 30s
    instances: 0
middleware:
  ratelimit:
    enabled: false
  depth_limit:
    max: 0
`)
	out := captureOutput(func() {
		cmdResume([]string{
			"--hunt-id", "test-resume-001",
			"--config", cfg,
			"--queue", dbPath,
		})
	})
	// An empty DB has no pending jobs.
	if !strings.Contains(out, "No pending") && !strings.Contains(out, "no pending") &&
		!strings.Contains(strings.ToLower(out), "no pending") {
		t.Errorf("expected 'No pending jobs' message for empty queue, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// defaultProcessor smoke tests
// ---------------------------------------------------------------------------

func TestDefaultProcessorReturnsItemWithURL(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		URL:        "https://example.com",
		Body:       []byte(`<html><head><title>Test Page</title></head><body><a href="/about">About</a></body></html>`),
		Job:        &foxhound.Job{Depth: 0},
	}
	result, err := defaultProcessor.Process(context.Background(), resp)
	if err != nil {
		t.Fatalf("defaultProcessor returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Items) == 0 {
		t.Fatal("expected at least one item")
	}
	urlVal, ok := result.Items[0].Get("url")
	if !ok {
		t.Fatal("expected 'url' field in item")
	}
	if urlVal != "https://example.com" {
		t.Errorf("expected url = 'https://example.com', got %v", urlVal)
	}
}

func TestDefaultProcessorExtractsTitleField(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		URL:        "https://example.com",
		Body:       []byte(`<html><head><title>Hello World</title></head><body></body></html>`),
		Job:        &foxhound.Job{Depth: 0},
	}
	result, err := defaultProcessor.Process(context.Background(), resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) == 0 {
		t.Fatal("expected at least one item")
	}
	titleVal, ok := result.Items[0].Get("title")
	if !ok {
		t.Fatal("expected 'title' field in item")
	}
	title, isStr := titleVal.(string)
	if !isStr || title == "" {
		t.Errorf("expected non-empty title string, got %v", titleVal)
	}
}

func TestDefaultProcessorFollowsSameDomainLinksOnly(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		URL:        "https://example.com/",
		Body: []byte(`<html><body>
			<a href="/page1">same domain relative</a>
			<a href="https://example.com/page2">same domain absolute</a>
			<a href="https://other.com/page3">external domain</a>
		</body></html>`),
		Job: &foxhound.Job{Depth: 0},
	}
	result, err := defaultProcessor.Process(context.Background(), resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, job := range result.Jobs {
		if strings.Contains(job.URL, "other.com") {
			t.Errorf("defaultProcessor should not follow external domain links, got: %s", job.URL)
		}
	}
	if len(result.Jobs) < 2 {
		t.Errorf("expected at least 2 same-domain jobs (/page1, /page2), got %d", len(result.Jobs))
	}
}

func TestDefaultProcessorIncrementsDepthForDiscoveredJobs(t *testing.T) {
	resp := &foxhound.Response{
		StatusCode: 200,
		URL:        "https://example.com/",
		Body:       []byte(`<html><body><a href="/child">child</a></body></html>`),
		Job:        &foxhound.Job{Depth: 2},
	}
	result, err := defaultProcessor.Process(context.Background(), resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, job := range result.Jobs {
		if job.Depth != 3 {
			t.Errorf("expected discovered job depth = 3 (parent=2+1), got %d", job.Depth)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeTempConfig creates a temporary YAML config file and returns its path.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "foxhound-*.yaml")
	if err != nil {
		t.Fatalf("creating temp config: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	f.Close()
	return f.Name()
}

// minimalConfig returns a *foxhound.Config with the smallest valid settings.
func minimalConfig() *foxhound.Config {
	return &foxhound.Config{
		Hunt:  foxhound.HuntConfig{Domain: "example.com", Walkers: 1},
		Queue: foxhound.QueueConfig{Backend: "memory"},
		Fetch: foxhound.FetchConfig{
			Browser: foxhound.BrowserFetchConfig{Instances: 0},
		},
		Middleware: foxhound.MiddlewareConfig{
			RateLimit:  foxhound.RateLimitConfig{Enabled: false},
			DepthLimit: foxhound.DepthLimitConfig{Max: 0},
		},
	}
}
