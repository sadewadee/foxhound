package queue_test

import (
	"context"
	"sync"
	"testing"
	"time"

	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/queue"
)

func newJob(id string, priority foxhound.Priority) *foxhound.Job {
	return &foxhound.Job{
		ID:        id,
		URL:       "https://example.com/" + id,
		Priority:  priority,
		CreatedAt: time.Now(),
	}
}

func TestMemoryQueuePushPop(t *testing.T) {
	q := queue.NewMemoryQueue()
	defer q.Close()

	job := newJob("j1", foxhound.PriorityNormal)
	if err := q.Push(context.Background(), job); err != nil {
		t.Fatalf("Push: %v", err)
	}

	got, err := q.Pop(context.Background())
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if got.ID != job.ID {
		t.Errorf("Pop returned job %q, want %q", got.ID, job.ID)
	}
}

func TestMemoryQueuePriorityOrder(t *testing.T) {
	q := queue.NewMemoryQueue()
	defer q.Close()

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

func TestMemoryQueueSamePriorityFIFO(t *testing.T) {
	q := queue.NewMemoryQueue()
	defer q.Close()

	// Give jobs distinct CreatedAt values so FIFO ordering is deterministic.
	t0 := time.Now()
	j1 := newJob("first", foxhound.PriorityNormal)
	j1.CreatedAt = t0
	j2 := newJob("second", foxhound.PriorityNormal)
	j2.CreatedAt = t0.Add(time.Millisecond)

	_ = q.Push(context.Background(), j2)
	_ = q.Push(context.Background(), j1)

	got, _ := q.Pop(context.Background())
	if got.ID != "first" {
		t.Errorf("expected older job first, got %q", got.ID)
	}
}

func TestMemoryQueueLen(t *testing.T) {
	q := queue.NewMemoryQueue()
	defer q.Close()

	if q.Len() != 0 {
		t.Fatalf("expected empty queue, got %d", q.Len())
	}
	_ = q.Push(context.Background(), newJob("a", foxhound.PriorityNormal))
	_ = q.Push(context.Background(), newJob("b", foxhound.PriorityNormal))
	if q.Len() != 2 {
		t.Errorf("expected 2, got %d", q.Len())
	}
	_, _ = q.Pop(context.Background())
	if q.Len() != 1 {
		t.Errorf("expected 1 after pop, got %d", q.Len())
	}
}

func TestMemoryQueuePopBlocksUntilPush(t *testing.T) {
	q := queue.NewMemoryQueue()
	defer q.Close()

	ch := make(chan *foxhound.Job, 1)
	go func() {
		job, _ := q.Pop(context.Background())
		ch <- job
	}()

	time.Sleep(20 * time.Millisecond) // ensure goroutine is blocking
	_ = q.Push(context.Background(), newJob("late", foxhound.PriorityNormal))

	select {
	case got := <-ch:
		if got.ID != "late" {
			t.Errorf("expected job 'late', got %q", got.ID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Pop did not unblock after Push")
	}
}

func TestMemoryQueuePopRespectsContextCancellation(t *testing.T) {
	q := queue.NewMemoryQueue()
	defer q.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := q.Pop(ctx)
	if err == nil {
		t.Fatal("expected error when context cancelled, got nil")
	}
}

func TestMemoryQueueCloseUnblocksWaiters(t *testing.T) {
	q := queue.NewMemoryQueue()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = q.Pop(context.Background())
	}()

	time.Sleep(20 * time.Millisecond)
	q.Close()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Close did not unblock Pop")
	}
}

func TestMemoryQueuePushAfterCloseReturnsError(t *testing.T) {
	q := queue.NewMemoryQueue()
	q.Close()

	err := q.Push(context.Background(), newJob("x", foxhound.PriorityNormal))
	if err == nil {
		t.Fatal("expected error when pushing to closed queue")
	}
}

func TestMemoryQueueConcurrentSafety(t *testing.T) {
	q := queue.NewMemoryQueue()
	defer q.Close()

	const producers = 5
	const jobsEach = 20
	total := producers * jobsEach

	var wg sync.WaitGroup
	for i := 0; i < producers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < jobsEach; j++ {
				_ = q.Push(context.Background(), newJob(
					"j", foxhound.Priority(n%3*5),
				))
			}
		}(i)
	}
	wg.Wait()

	if got := q.Len(); got != total {
		t.Errorf("expected %d jobs, got %d", total, got)
	}
}
