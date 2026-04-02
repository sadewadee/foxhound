package queue

import (
	"context"
	"log/slog"
	"sync"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

// ReliableConfig configures the reliable queue wrapper.
type ReliableConfig struct {
	// MaxRetries is the maximum number of times a job can fail before moving to DLQ.
	// Default: 3
	MaxRetries int
	// AckTimeout is how long a job can be "in-flight" before being re-queued.
	// Default: 5 minutes
	AckTimeout time.Duration
	// StaleCheckInterval is how often to check for stale in-flight jobs.
	// Default: 30 seconds
	StaleCheckInterval time.Duration
}

// DefaultReliableConfig returns sensible defaults.
func DefaultReliableConfig() ReliableConfig {
	return ReliableConfig{
		MaxRetries:         3,
		AckTimeout:         5 * time.Minute,
		StaleCheckInterval: 30 * time.Second,
	}
}

// inFlightJob tracks a job that has been popped but not yet acknowledged.
type inFlightJob struct {
	job      *foxhound.Job
	poppedAt time.Time
	attempts int
}

// ReliableQueue wraps any foxhound.Queue with acknowledgment, retry, and dead-letter
// queue semantics. Jobs that fail multiple times are moved to the DLQ instead of
// being retried forever.
type ReliableQueue struct {
	inner  foxhound.Queue
	config ReliableConfig

	mu         sync.Mutex
	inFlight   map[string]*inFlightJob // job URL -> in-flight state
	dlq        []*foxhound.Job         // dead letter queue
	retryCount map[string]int          // job URL -> retry count

	stopCh  chan struct{}
	stopped bool
}

// NewReliable wraps an existing Queue with reliable delivery semantics.
func NewReliable(inner foxhound.Queue, cfg ReliableConfig) *ReliableQueue {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.AckTimeout <= 0 {
		cfg.AckTimeout = 5 * time.Minute
	}
	if cfg.StaleCheckInterval <= 0 {
		cfg.StaleCheckInterval = 30 * time.Second
	}

	rq := &ReliableQueue{
		inner:      inner,
		config:     cfg,
		inFlight:   make(map[string]*inFlightJob),
		retryCount: make(map[string]int),
		stopCh:     make(chan struct{}),
	}

	// Start stale job recovery goroutine.
	go rq.recoverStaleJobs()

	return rq
}

// Push delegates to the inner queue.
func (rq *ReliableQueue) Push(ctx context.Context, job *foxhound.Job) error {
	return rq.inner.Push(ctx, job)
}

// Pop returns the next job and tracks it as in-flight.
func (rq *ReliableQueue) Pop(ctx context.Context) (*foxhound.Job, error) {
	job, err := rq.inner.Pop(ctx)
	if err != nil || job == nil {
		return job, err
	}

	rq.mu.Lock()
	rq.inFlight[job.URL] = &inFlightJob{
		job:      job,
		poppedAt: time.Now(),
		attempts: rq.retryCount[job.URL] + 1,
	}
	rq.mu.Unlock()

	return job, nil
}

// Ack acknowledges successful processing of a job.
func (rq *ReliableQueue) Ack(job *foxhound.Job) {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	delete(rq.inFlight, job.URL)
	delete(rq.retryCount, job.URL)
}

// Nack signals that job processing failed. The job is re-queued or moved to DLQ.
func (rq *ReliableQueue) Nack(ctx context.Context, job *foxhound.Job) {
	rq.mu.Lock()
	rq.retryCount[job.URL]++
	attempts := rq.retryCount[job.URL]
	delete(rq.inFlight, job.URL)

	if attempts >= rq.config.MaxRetries {
		rq.dlq = append(rq.dlq, job)
		delete(rq.retryCount, job.URL)
		rq.mu.Unlock()
		slog.Warn("queue/reliable: job moved to DLQ after max retries",
			"url", job.URL, "attempts", attempts)
		return
	}
	rq.mu.Unlock()

	// Re-queue for retry.
	if err := rq.inner.Push(ctx, job); err != nil {
		slog.Error("queue/reliable: failed to re-queue job", "url", job.URL, "err", err)
	}
}

// DLQ returns all jobs in the dead letter queue.
func (rq *ReliableQueue) DLQ() []*foxhound.Job {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	result := make([]*foxhound.Job, len(rq.dlq))
	copy(result, rq.dlq)
	return result
}

// RetryDLQ moves all DLQ jobs back to the main queue.
func (rq *ReliableQueue) RetryDLQ(ctx context.Context) (int, error) {
	rq.mu.Lock()
	jobs := rq.dlq
	rq.dlq = nil
	rq.mu.Unlock()

	for _, job := range jobs {
		if err := rq.inner.Push(ctx, job); err != nil {
			return 0, err
		}
	}
	return len(jobs), nil
}

// DLQLen returns the number of jobs in the dead letter queue.
func (rq *ReliableQueue) DLQLen() int {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	return len(rq.dlq)
}

// InFlightLen returns the number of jobs currently being processed.
func (rq *ReliableQueue) InFlightLen() int {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	return len(rq.inFlight)
}

// Len returns the number of pending jobs in the inner queue.
func (rq *ReliableQueue) Len() int {
	return rq.inner.Len()
}

// Close stops the stale recovery goroutine and closes the inner queue.
func (rq *ReliableQueue) Close() error {
	rq.mu.Lock()
	if !rq.stopped {
		rq.stopped = true
		close(rq.stopCh)
	}
	rq.mu.Unlock()
	return rq.inner.Close()
}

// recoverStaleJobs periodically checks for in-flight jobs that have exceeded
// the ack timeout and re-queues them.
func (rq *ReliableQueue) recoverStaleJobs() {
	ticker := time.NewTicker(rq.config.StaleCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rq.stopCh:
			return
		case <-ticker.C:
			rq.mu.Lock()
			now := time.Now()
			var stale []*foxhound.Job
			for url, ifj := range rq.inFlight {
				if now.Sub(ifj.poppedAt) > rq.config.AckTimeout {
					stale = append(stale, ifj.job)
					delete(rq.inFlight, url)
				}
			}
			rq.mu.Unlock()

			for _, job := range stale {
				ctx := context.Background()
				rq.Nack(ctx, job)
				slog.Warn("queue/reliable: recovered stale in-flight job",
					"url", job.URL)
			}
		}
	}
}
