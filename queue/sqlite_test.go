package queue_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/queue"
)

// newSQLiteQueue creates a SQLiteQueue backed by a temp file.
// The file is cleaned up after the test.
func newSQLiteQueue(t *testing.T) (foxhound.Queue, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	q, err := queue.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	t.Cleanup(func() { q.Close() })
	return q, dbPath
}

func TestSQLiteQueue_PushPop(t *testing.T) {
	q, _ := newSQLiteQueue(t)

	job := newJob("s1", foxhound.PriorityNormal)
	if err := q.Push(context.Background(), job); err != nil {
		t.Fatalf("Push: %v", err)
	}

	got, err := q.Pop(context.Background())
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if got.ID != "s1" {
		t.Errorf("Pop returned job %q, want %q", got.ID, "s1")
	}
}

func TestSQLiteQueue_PriorityOrder(t *testing.T) {
	q, _ := newSQLiteQueue(t)

	_ = q.Push(context.Background(), newJob("low", foxhound.PriorityLow))
	_ = q.Push(context.Background(), newJob("high", foxhound.PriorityHigh))
	_ = q.Push(context.Background(), newJob("normal", foxhound.PriorityNormal))

	first, _ := q.Pop(context.Background())
	if first.ID != "high" {
		t.Errorf("expected high priority first, got %q", first.ID)
	}
	second, _ := q.Pop(context.Background())
	if second.ID != "normal" {
		t.Errorf("expected normal priority second, got %q", second.ID)
	}
	third, _ := q.Pop(context.Background())
	if third.ID != "low" {
		t.Errorf("expected low priority third, got %q", third.ID)
	}
}

func TestSQLiteQueue_SamePriorityFIFO(t *testing.T) {
	q, _ := newSQLiteQueue(t)

	t0 := time.Now()
	j1 := newJob("first", foxhound.PriorityNormal)
	j1.CreatedAt = t0
	j2 := newJob("second", foxhound.PriorityNormal)
	j2.CreatedAt = t0.Add(time.Millisecond)

	// Push older job first; FIFO should still preserve order.
	_ = q.Push(context.Background(), j1)
	_ = q.Push(context.Background(), j2)

	got, _ := q.Pop(context.Background())
	if got.ID != "first" {
		t.Errorf("expected older job first, got %q", got.ID)
	}
}

func TestSQLiteQueue_Len(t *testing.T) {
	q, _ := newSQLiteQueue(t)

	if n := q.Len(); n != 0 {
		t.Fatalf("expected empty queue, got %d", n)
	}
	_ = q.Push(context.Background(), newJob("a", foxhound.PriorityNormal))
	_ = q.Push(context.Background(), newJob("b", foxhound.PriorityNormal))
	if n := q.Len(); n != 2 {
		t.Errorf("expected 2 after two pushes, got %d", n)
	}
	_, _ = q.Pop(context.Background())
	if n := q.Len(); n != 1 {
		t.Errorf("expected 1 after one pop, got %d", n)
	}
}

// TestSQLiteQueue_Persistence verifies that jobs survive a close/reopen cycle.
func TestSQLiteQueue_Persistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")

	// Write jobs in first queue instance.
	q1, err := queue.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite (first): %v", err)
	}
	_ = q1.Push(context.Background(), newJob("persist-1", foxhound.PriorityHigh))
	_ = q1.Push(context.Background(), newJob("persist-2", foxhound.PriorityLow))
	if err := q1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Confirm the file still exists.
	if _, statErr := os.Stat(dbPath); statErr != nil {
		t.Fatalf("DB file missing after close: %v", statErr)
	}

	// Reopen and verify jobs are still there.
	q2, err := queue.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite (second): %v", err)
	}
	defer q2.Close()

	if n := q2.Len(); n != 2 {
		t.Fatalf("expected 2 pending jobs after reopen, got %d", n)
	}

	// Higher priority should still come out first.
	got, err := q2.Pop(context.Background())
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if got.ID != "persist-1" {
		t.Errorf("expected persist-1 first, got %q", got.ID)
	}
}

func TestSQLiteQueue_PopRespectsContextCancellation(t *testing.T) {
	q, _ := newSQLiteQueue(t)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, err := q.Pop(ctx)
	if err == nil {
		t.Fatal("expected error when context cancelled, got nil")
	}
}

func TestSQLiteQueue_PopMarksProcessing(t *testing.T) {
	// After a Pop the job's status moves to 'processing',
	// so Len (which counts 'pending') should not include it.
	q, _ := newSQLiteQueue(t)

	_ = q.Push(context.Background(), newJob("m1", foxhound.PriorityNormal))
	_ = q.Push(context.Background(), newJob("m2", foxhound.PriorityNormal))

	_, _ = q.Pop(context.Background())

	if n := q.Len(); n != 1 {
		t.Errorf("expected 1 pending after pop, got %d", n)
	}
}

func TestSQLiteQueue_JobFieldsPreserved(t *testing.T) {
	q, _ := newSQLiteQueue(t)

	job := &foxhound.Job{
		ID:       "full-sqlite",
		URL:      "https://example.com/sqlite",
		Method:   "POST",
		Priority: foxhound.PriorityHigh,
		Depth:    2,
		Domain:   "example.com",
		Meta:     map[string]any{"foo": "bar"},
	}
	job.CreatedAt = time.Now()

	if err := q.Push(context.Background(), job); err != nil {
		t.Fatalf("Push: %v", err)
	}

	got, err := q.Pop(context.Background())
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if got.ID != job.ID {
		t.Errorf("ID: got %q, want %q", got.ID, job.ID)
	}
	if got.URL != job.URL {
		t.Errorf("URL: got %q, want %q", got.URL, job.URL)
	}
	if got.Depth != job.Depth {
		t.Errorf("Depth: got %d, want %d", got.Depth, job.Depth)
	}
	if v, ok := got.Meta["foo"]; !ok || v != "bar" {
		t.Errorf("Meta[foo]: got %v, want %q", v, "bar")
	}
}
