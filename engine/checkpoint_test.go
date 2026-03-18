package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/engine"
)

// ---------------------------------------------------------------------------
// TestSaveCheckpoint
// ---------------------------------------------------------------------------

// TestSaveCheckpoint verifies that SaveCheckpoint writes valid JSON that can
// be decoded and contains the expected field values.
func TestSaveCheckpoint(t *testing.T) {
	h := minimalHunt("save-cp-test")

	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	if err := h.SaveCheckpoint(path); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// File must exist.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("checkpoint file not created: %v", err)
	}

	cp, err := engine.LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}

	if cp.HuntName != "save-cp-test" {
		t.Errorf("HuntName: want %q, got %q", "save-cp-test", cp.HuntName)
	}
	if cp.Timestamp.IsZero() {
		t.Error("Timestamp must not be zero")
	}
}

// ---------------------------------------------------------------------------
// TestLoadCheckpoint
// ---------------------------------------------------------------------------

// TestLoadCheckpoint verifies that LoadCheckpoint correctly deserialises a
// checkpoint written by SaveCheckpoint.
func TestLoadCheckpoint(t *testing.T) {
	h := minimalHunt("load-cp-test")

	dir := t.TempDir()
	path := filepath.Join(dir, "cp.json")

	if err := h.SaveCheckpoint(path); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	cp, err := engine.LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}

	if cp == nil {
		t.Fatal("LoadCheckpoint returned nil")
	}
	if cp.HuntName == "" {
		t.Error("HuntName must not be empty after round-trip")
	}
}

// ---------------------------------------------------------------------------
// TestLoadCheckpoint_FileNotFound
// ---------------------------------------------------------------------------

// TestLoadCheckpoint_FileNotFound verifies that LoadCheckpoint returns an
// error when the file does not exist.
func TestLoadCheckpoint_FileNotFound(t *testing.T) {
	_, err := engine.LoadCheckpoint("/tmp/foxhound-nonexistent-checkpoint-xyz.json")
	if err == nil {
		t.Error("expected error for non-existent checkpoint file, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestCheckpointRoundtrip
// ---------------------------------------------------------------------------

// TestCheckpointRoundtrip saves a checkpoint, loads it back, and verifies
// every exported field matches the original snapshot.
func TestCheckpointRoundtrip(t *testing.T) {
	h := minimalHunt("roundtrip-test")
	// Simulate some stats by advancing counters.
	h.Stats().RecordItems(7)
	h.Stats().RecordRequest("example.com", 10*time.Millisecond, nil, false)
	h.Stats().RecordRequest("example.com", 5*time.Millisecond, nil, true)

	dir := t.TempDir()
	path := filepath.Join(dir, "rt.json")

	if err := h.SaveCheckpoint(path); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	cp, err := engine.LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}

	if cp.HuntName != "roundtrip-test" {
		t.Errorf("HuntName: want roundtrip-test, got %q", cp.HuntName)
	}
	if cp.ItemsProcessed != 7 {
		t.Errorf("ItemsProcessed: want 7, got %d", cp.ItemsProcessed)
	}
	if cp.RequestsDone != 2 {
		t.Errorf("RequestsDone: want 2, got %d", cp.RequestsDone)
	}
	if cp.Timestamp.IsZero() {
		t.Error("Timestamp must not be zero")
	}
	if cp.ElapsedMs < 0 {
		t.Errorf("ElapsedMs must be >= 0, got %d", cp.ElapsedMs)
	}
}

// ---------------------------------------------------------------------------
// TestAutoCheckpoint
// ---------------------------------------------------------------------------

// cpHTMLBody is a minimal HTML body that bypasses the captcha detector's
// empty_trap heuristic (which fires when body < 500 bytes and lacks <html).
const cpHTMLBody = "<html><body><p>content</p></body></html>"

// TestAutoCheckpoint verifies that when CheckpointConfig.Enabled=true a
// checkpoint file is written after at least Interval items are processed.
func TestAutoCheckpoint(t *testing.T) {
	dir := t.TempDir()
	cpPath := filepath.Join(dir, "auto.json")

	q := newMemQueue(64)

	const totalJobs = 5 // 5 jobs → 5 items → interval=3 triggers at item 3
	items := make([]*foxhound.Item, 1)
	items[0] = foxhound.NewItem()
	items[0].Set("v", "x")

	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200, Body: []byte(cpHTMLBody)}}
	processor := &stubProcessor{result: &foxhound.Result{Items: items}}

	seeds := make([]*foxhound.Job, totalJobs)
	for i := 0; i < totalJobs; i++ {
		seeds[i] = seedJob("https://example.com/" + string(rune('a'+i)))
	}

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "auto-cp-test",
		Domain:    "example.com",
		Walkers:   1,
		Seeds:     seeds,
		Queue:     q,
		Fetcher:   fetcher,
		Processor: processor,
		Checkpoint: engine.CheckpointConfig{
			Enabled:  true,
			Path:     cpPath,
			Interval: 3,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// After 5 items with interval=3, a checkpoint must have been written.
	if _, err := os.Stat(cpPath); os.IsNotExist(err) {
		t.Error("expected auto-checkpoint file to exist after run, but it was not created")
	}

	cp, err := engine.LoadCheckpoint(cpPath)
	if err != nil {
		t.Fatalf("LoadCheckpoint after auto: %v", err)
	}
	if cp.HuntName != "auto-cp-test" {
		t.Errorf("auto-checkpoint HuntName: want auto-cp-test, got %q", cp.HuntName)
	}
}

// ---------------------------------------------------------------------------
// TestCheckpoint_Disabled
// ---------------------------------------------------------------------------

// TestCheckpoint_Disabled verifies that when CheckpointConfig.Enabled=false
// no checkpoint file is written even after many items.
func TestCheckpoint_Disabled(t *testing.T) {
	dir := t.TempDir()
	cpPath := filepath.Join(dir, "disabled.json")

	q := newMemQueue(32)
	items := []*foxhound.Item{foxhound.NewItem()}

	seeds := []*foxhound.Job{seedJob("https://example.com/1"), seedJob("https://example.com/2")}
	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200, Body: []byte(cpHTMLBody)}}
	processor := &stubProcessor{result: &foxhound.Result{Items: items}}

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "no-cp-test",
		Domain:    "example.com",
		Walkers:   1,
		Seeds:     seeds,
		Queue:     q,
		Fetcher:   fetcher,
		Processor: processor,
		Checkpoint: engine.CheckpointConfig{
			Enabled:  false,
			Path:     cpPath,
			Interval: 1,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(cpPath); !os.IsNotExist(err) {
		t.Error("checkpoint file must NOT be created when Enabled=false")
	}
}

// ---------------------------------------------------------------------------
// TestCheckpoint_SaveCheckpoint_IncludesQueueLen
// ---------------------------------------------------------------------------

// TestCheckpoint_SaveCheckpoint_IncludesQueueLen verifies that QueueLen in
// the checkpoint reflects the hunt's queue length at save time.
func TestCheckpoint_SaveCheckpoint_IncludesQueueLen(t *testing.T) {
	// Build a hunt with a non-empty queue at save time.
	q := newMemQueue(64)
	// Pre-push some jobs so the queue is non-empty.
	for i := 0; i < 3; i++ {
		_ = q.Push(context.Background(), seedJob("https://example.com/"+string(rune('a'+i))))
	}

	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200}}
	processor := &stubProcessor{result: &foxhound.Result{}}

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "qlen-cp-test",
		Domain:    "example.com",
		Walkers:   1,
		Queue:     q,
		Fetcher:   fetcher,
		Processor: processor,
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "qlen.json")

	if err := h.SaveCheckpoint(path); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	cp, err := engine.LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}

	// Queue has 3 pre-pushed jobs; checkpoint must capture that.
	if cp.QueueLen != 3 {
		t.Errorf("QueueLen: want 3, got %d", cp.QueueLen)
	}
}

// ---------------------------------------------------------------------------
// TestAutoCheckpoint_DefaultInterval
// ---------------------------------------------------------------------------

// TestAutoCheckpoint_DefaultInterval verifies that Interval=0 is treated as
// the default (100) and does not cause a division-by-zero panic.
func TestAutoCheckpoint_DefaultInterval(t *testing.T) {
	dir := t.TempDir()
	cpPath := filepath.Join(dir, "default-interval.json")

	q := newMemQueue(8)
	seeds := []*foxhound.Job{seedJob("https://example.com/")}
	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200}}
	processor := &stubProcessor{result: &foxhound.Result{}}

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "default-interval-test",
		Domain:    "example.com",
		Walkers:   1,
		Seeds:     seeds,
		Queue:     q,
		Fetcher:   fetcher,
		Processor: processor,
		Checkpoint: engine.CheckpointConfig{
			Enabled:  true,
			Path:     cpPath,
			Interval: 0, // should default to 100
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Must not panic.
	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Internal helper
// ---------------------------------------------------------------------------

// minimalHunt creates a minimal Hunt that is not yet started (no Run call).
// It is used for direct SaveCheckpoint / LoadCheckpoint testing without
// needing to start a full scraping campaign.
func minimalHunt(name string) *engine.Hunt {
	q := newMemQueue(8)
	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200}}
	processor := &stubProcessor{result: &foxhound.Result{}}
	return engine.NewHunt(engine.HuntConfig{
		Name:      name,
		Domain:    "example.com",
		Walkers:   1,
		Queue:     q,
		Fetcher:   fetcher,
		Processor: processor,
	})
}

