package queue_test

import (
	"context"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/queue"
)

func TestReliableQueue_AckRemovesFromInFlight(t *testing.T) {
	inner := queue.NewMemoryQueue()
	rq := queue.NewReliable(inner, queue.DefaultReliableConfig())
	defer rq.Close()

	ctx := context.Background()
	job := &foxhound.Job{URL: "https://example.com/1"}
	rq.Push(ctx, job)

	popped, _ := rq.Pop(ctx)
	if rq.InFlightLen() != 1 {
		t.Fatalf("Expected 1 in-flight, got %d", rq.InFlightLen())
	}

	rq.Ack(popped)
	if rq.InFlightLen() != 0 {
		t.Fatalf("Expected 0 in-flight after ack, got %d", rq.InFlightLen())
	}
}

func TestReliableQueue_NackRequeuesJob(t *testing.T) {
	inner := queue.NewMemoryQueue()
	rq := queue.NewReliable(inner, queue.ReliableConfig{
		MaxRetries:         3,
		AckTimeout:         5 * time.Minute,
		StaleCheckInterval: 1 * time.Hour,
	})
	defer rq.Close()

	ctx := context.Background()
	job := &foxhound.Job{URL: "https://example.com/retry"}
	rq.Push(ctx, job)

	popped, _ := rq.Pop(ctx)
	rq.Nack(ctx, popped)

	// Should be re-queued.
	if rq.Len() != 1 {
		t.Fatalf("Expected job to be re-queued, queue len = %d", rq.Len())
	}
	if rq.DLQLen() != 0 {
		t.Fatalf("Expected 0 in DLQ, got %d", rq.DLQLen())
	}
}

func TestReliableQueue_NackMovesToDLQAfterMaxRetries(t *testing.T) {
	inner := queue.NewMemoryQueue()
	rq := queue.NewReliable(inner, queue.ReliableConfig{
		MaxRetries:         2,
		AckTimeout:         5 * time.Minute,
		StaleCheckInterval: 1 * time.Hour,
	})
	defer rq.Close()

	ctx := context.Background()
	job := &foxhound.Job{URL: "https://example.com/fail"}

	// Fail twice.
	for i := 0; i < 2; i++ {
		rq.Push(ctx, job)
		popped, _ := rq.Pop(ctx)
		rq.Nack(ctx, popped)
	}

	if rq.DLQLen() != 1 {
		t.Fatalf("Expected 1 in DLQ after max retries, got %d", rq.DLQLen())
	}
}

func TestReliableQueue_RetryDLQ(t *testing.T) {
	inner := queue.NewMemoryQueue()
	rq := queue.NewReliable(inner, queue.ReliableConfig{
		MaxRetries:         1,
		AckTimeout:         5 * time.Minute,
		StaleCheckInterval: 1 * time.Hour,
	})
	defer rq.Close()

	ctx := context.Background()
	job := &foxhound.Job{URL: "https://example.com/dlq"}
	rq.Push(ctx, job)
	popped, _ := rq.Pop(ctx)
	rq.Nack(ctx, popped)

	if rq.DLQLen() != 1 {
		t.Fatalf("Expected 1 in DLQ, got %d", rq.DLQLen())
	}

	n, err := rq.RetryDLQ(ctx)
	if err != nil {
		t.Fatalf("RetryDLQ error: %v", err)
	}
	if n != 1 {
		t.Fatalf("Expected 1 retried, got %d", n)
	}
	if rq.DLQLen() != 0 {
		t.Fatalf("Expected 0 in DLQ after retry, got %d", rq.DLQLen())
	}
	if rq.Len() != 1 {
		t.Fatalf("Expected 1 in queue after retry, got %d", rq.Len())
	}
}
