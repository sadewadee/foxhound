package engine

import (
	"context"
	"log/slog"
	"sync"

	foxhound "github.com/sadewadee/foxhound"
)

// Scheduler manages a fixed-size pool of worker goroutines that consume jobs
// from a Queue and invoke a user-supplied handler for each one. It is designed
// for use as an internal component of Hunt but can also be used standalone.
type Scheduler struct {
	queue      foxhound.Queue
	maxWorkers int

	stopOnce sync.Once
	stopCh   chan struct{}
	// doneCh is closed by Start when all workers have exited, allowing Wait
	// to unblock without a data race on the internal sync.WaitGroup.
	doneCh chan struct{}
	// startOnce ensures doneCh is only initialised once across multiple calls.
	startOnce sync.Once
}

// NewScheduler creates a Scheduler backed by queue with a pool of at most
// maxWorkers concurrent workers.
func NewScheduler(queue foxhound.Queue, maxWorkers int) *Scheduler {
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	return &Scheduler{
		queue:      queue,
		maxWorkers: maxWorkers,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
}

// Submit pushes one or more jobs directly to the underlying queue. It is safe
// to call from any goroutine.
func (s *Scheduler) Submit(ctx context.Context, jobs ...*foxhound.Job) error {
	for _, job := range jobs {
		if err := s.queue.Push(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

// Start launches maxWorkers goroutines that each loop over queue.Pop →
// handler. It blocks until Stop is called or ctx is cancelled, and returns
// only after all in-flight handlers have returned.
//
// Calling Start again after it returns is not supported.
func (s *Scheduler) Start(ctx context.Context, handler func(context.Context, *foxhound.Job) error) error {
	workerCtx, workerCancel := context.WithCancel(ctx)

	var wg sync.WaitGroup
	for i := 0; i < s.maxWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s.runWorker(workerCtx, id, handler)
		}(i)
	}

	// Block until Stop() is called or the parent context expires.
	select {
	case <-s.stopCh:
		workerCancel()
	case <-ctx.Done():
		workerCancel()
	}

	// Wait for all in-flight workers to finish, then signal Wait().
	wg.Wait()
	close(s.doneCh)
	return nil
}

// runWorker is the inner loop for a single worker goroutine.
func (s *Scheduler) runWorker(ctx context.Context, id int, handler func(context.Context, *foxhound.Job) error) {
	logger := slog.With("component", "scheduler", "worker_id", id)
	for {
		job, err := s.queue.Pop(ctx)
		if err != nil {
			// Context cancelled or queue closed — exit cleanly.
			return
		}
		if err := handler(ctx, job); err != nil {
			logger.Error("handler error", "url", job.URL, "err", err)
		}
	}
}

// Stop signals all workers to stop after finishing their current job.
// It is safe to call more than once.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// Wait blocks until Start has returned and all worker goroutines have exited.
// It is safe to call concurrently with Stop.
func (s *Scheduler) Wait() {
	<-s.doneCh
}
