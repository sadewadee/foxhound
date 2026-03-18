package queue_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/queue"
	"github.com/redis/go-redis/v9"
)

// newRedisQueue attempts to connect to a local Redis instance.
// If the connection fails (Redis not available) the test is skipped.
func newRedisQueue(t *testing.T, suffix string) foxhound.Queue {
	t.Helper()
	key := fmt.Sprintf("foxhound:test:%s:%d", suffix, time.Now().UnixNano())
	q, err := queue.NewRedis("localhost:6379", "", 15, key)
	if err != nil {
		t.Skipf("redis not available: %v", err)
	}
	// Verify connectivity with a PING.
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379", DB: 15})
	defer client.Close()
	if pingErr := client.Ping(context.Background()).Err(); pingErr != nil {
		q.Close()
		t.Skipf("redis not available: %v", pingErr)
	}
	t.Cleanup(func() { q.Close() })
	return q
}

func TestRedisQueue_PushPop(t *testing.T) {
	q := newRedisQueue(t, "pushpop")

	job := newJob("r1", foxhound.PriorityNormal)
	if err := q.Push(context.Background(), job); err != nil {
		t.Fatalf("Push: %v", err)
	}

	got, err := q.Pop(context.Background())
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if got.ID != "r1" {
		t.Errorf("Pop returned job %q, want %q", got.ID, "r1")
	}
}

func TestRedisQueue_PriorityOrder(t *testing.T) {
	q := newRedisQueue(t, "priority")

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

func TestRedisQueue_SamePriorityFIFO(t *testing.T) {
	q := newRedisQueue(t, "fifo")

	t0 := time.Now()
	j1 := newJob("first", foxhound.PriorityNormal)
	j1.CreatedAt = t0
	j2 := newJob("second", foxhound.PriorityNormal)
	j2.CreatedAt = t0.Add(time.Millisecond)

	_ = q.Push(context.Background(), j1)
	_ = q.Push(context.Background(), j2)

	got, _ := q.Pop(context.Background())
	if got.ID != "first" {
		t.Errorf("expected older job first, got %q", got.ID)
	}
}

func TestRedisQueue_Len(t *testing.T) {
	q := newRedisQueue(t, "len")

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

func TestRedisQueue_PopRespectsContextCancellation(t *testing.T) {
	q := newRedisQueue(t, "cancel")

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, err := q.Pop(ctx)
	if err == nil {
		t.Fatal("expected error when context cancelled, got nil")
	}
}

func TestRedisQueue_JobFieldsPreserved(t *testing.T) {
	q := newRedisQueue(t, "fields")

	job := &foxhound.Job{
		ID:       "full-job",
		URL:      "https://example.com/full",
		Method:   "POST",
		Priority: foxhound.PriorityHigh,
		Depth:    3,
		Domain:   "example.com",
		Meta:     map[string]any{"key": "value"},
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
	if got.Method != job.Method {
		t.Errorf("Method: got %q, want %q", got.Method, job.Method)
	}
	if got.Depth != job.Depth {
		t.Errorf("Depth: got %d, want %d", got.Depth, job.Depth)
	}
	if got.Domain != job.Domain {
		t.Errorf("Domain: got %q, want %q", got.Domain, job.Domain)
	}
	if v, ok := got.Meta["key"]; !ok || v != "value" {
		t.Errorf("Meta[key]: got %v, want %q", v, "value")
	}
}
