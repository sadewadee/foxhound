// Package queue provides foxhound.Queue implementations.
// The memory package offers an in-process, priority-ordered queue suitable
// for single-node scraping runs.
package queue

import (
	"container/heap"
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// ErrQueueClosed is returned when operating on a closed queue.
var ErrQueueClosed = errors.New("queue: queue is closed")

// jobItem wraps a Job for use inside the heap.
type jobItem struct {
	job   *foxhound.Job
	index int // maintained by the heap interface
}

// jobHeap is a max-priority heap of *jobItem.
// Higher Priority values surface first; ties break on older CreatedAt.
type jobHeap []*jobItem

func (h jobHeap) Len() int { return len(h) }

func (h jobHeap) Less(i, j int) bool {
	pi, pj := h[i].job.Priority, h[j].job.Priority
	if pi != pj {
		return pi > pj // higher priority first
	}
	// Same priority: older CreatedAt wins (FIFO within a priority tier).
	return h[i].job.CreatedAt.Before(h[j].job.CreatedAt)
}

func (h jobHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *jobHeap) Push(x any) {
	item := x.(*jobItem)
	item.index = len(*h)
	*h = append(*h, item)
}

func (h *jobHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[:n-1]
	return item
}

// memoryQueue is an in-process, priority-ordered implementation of foxhound.Queue.
type memoryQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	h      jobHeap
	closed bool
}

// NewMemoryQueue creates a thread-safe, priority-ordered in-memory queue.
//
// Pop blocks until a job is available or the context is cancelled.
// Close unblocks all waiting Pop callers.
func NewMemoryQueue() foxhound.Queue {
	q := &memoryQueue{}
	q.cond = sync.NewCond(&q.mu)
	heap.Init(&q.h)
	return q
}

// Push adds a job to the queue ordered by Priority then CreatedAt.
// Returns ErrQueueClosed if the queue has been closed.
func (q *memoryQueue) Push(_ context.Context, job *foxhound.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return ErrQueueClosed
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	heap.Push(&q.h, &jobItem{job: job})
	slog.Debug("queue: pushed job", "id", job.ID, "url", job.URL, "priority", job.Priority, "depth", job.Depth)
	q.cond.Signal()
	return nil
}

// Pop removes and returns the highest-priority job, blocking until one is
// available or the context is cancelled.
func (q *memoryQueue) Pop(ctx context.Context) (*foxhound.Job, error) {
	// Run a goroutine to unblock the cond when the context is done.
	stop := context.AfterFunc(ctx, func() {
		q.cond.Broadcast()
	})
	defer stop()

	q.mu.Lock()
	defer q.mu.Unlock()

	for q.h.Len() == 0 && !q.closed {
		q.cond.Wait()
		// Re-check context after wakeup.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	if q.h.Len() == 0 {
		// Queue was closed with nothing in it.
		return nil, ErrQueueClosed
	}

	item := heap.Pop(&q.h).(*jobItem)
	slog.Debug("queue: popped job", "id", item.job.ID, "url", item.job.URL, "priority", item.job.Priority)
	return item.job, nil
}

// Len returns the number of jobs currently in the queue.
func (q *memoryQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.h.Len()
}

// Close marks the queue as closed and unblocks all waiting Pop calls.
func (q *memoryQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		q.closed = true
		q.cond.Broadcast()
		slog.Debug("queue: closed")
	}
	return nil
}
