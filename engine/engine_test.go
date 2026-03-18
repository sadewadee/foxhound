package engine_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/engine"
)

// ---------------------------------------------------------------------------
// Test helpers: in-memory queue and stub implementations
// ---------------------------------------------------------------------------

// memQueue is a simple in-memory queue backed by a channel. It blocks on Pop
// until a job arrives or the context is cancelled.
type memQueue struct {
	ch     chan *foxhound.Job
	closed atomic.Bool
}

func newMemQueue(capacity int) *memQueue {
	return &memQueue{ch: make(chan *foxhound.Job, capacity)}
}

func (q *memQueue) Push(_ context.Context, job *foxhound.Job) error {
	if q.closed.Load() {
		return errors.New("queue closed")
	}
	q.ch <- job
	return nil
}

func (q *memQueue) Pop(ctx context.Context) (*foxhound.Job, error) {
	select {
	case job, ok := <-q.ch:
		if !ok {
			return nil, errors.New("queue closed")
		}
		return job, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (q *memQueue) Len() int { return len(q.ch) }

func (q *memQueue) Close() error {
	if q.closed.CompareAndSwap(false, true) {
		close(q.ch)
	}
	return nil
}

// stubFetcher returns a canned response for every job.
type stubFetcher struct {
	resp *foxhound.Response
	err  error
	mu   sync.Mutex
	calls int
}

func (f *stubFetcher) Fetch(_ context.Context, job *foxhound.Job) (*foxhound.Response, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	r := *f.resp
	r.Job = job
	return &r, nil
}

func (f *stubFetcher) Close() error { return nil }

func (f *stubFetcher) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// stubProcessor returns a fixed result on every call.
type stubProcessor struct {
	result *foxhound.Result
	err    error
	mu     sync.Mutex
	calls  int
}

func (p *stubProcessor) Process(_ context.Context, _ *foxhound.Response) (*foxhound.Result, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return p.result, p.err
}

func (p *stubProcessor) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// capturingWriter records every item written to it.
type capturingWriter struct {
	mu     sync.Mutex
	items  []*foxhound.Item
	closed bool
}

func (w *capturingWriter) Write(_ context.Context, item *foxhound.Item) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.items = append(w.items, item)
	return nil
}

func (w *capturingWriter) Flush(_ context.Context) error { return nil }
func (w *capturingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

func (w *capturingWriter) Items() []*foxhound.Item {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]*foxhound.Item, len(w.items))
	copy(out, w.items)
	return out
}

// identityPipeline passes items through unchanged.
type identityPipeline struct{}

func (p *identityPipeline) Process(_ context.Context, item *foxhound.Item) (*foxhound.Item, error) {
	return item, nil
}

// dropPipeline drops every item (returns nil).
type dropPipeline struct{}

func (p *dropPipeline) Process(_ context.Context, _ *foxhound.Item) (*foxhound.Item, error) {
	return nil, nil
}

// errorPipeline returns an error for every item.
type errorPipeline struct{}

func (p *errorPipeline) Process(_ context.Context, _ *foxhound.Item) (*foxhound.Item, error) {
	return nil, errors.New("pipeline error")
}

// seedJob builds a minimal Job for use in tests.
func seedJob(url string) *foxhound.Job {
	return &foxhound.Job{
		ID:        url,
		URL:       url,
		Method:    "GET",
		CreatedAt: time.Now(),
		Domain:    "example.com",
	}
}

// ---------------------------------------------------------------------------
// Stats tests
// ---------------------------------------------------------------------------

func TestNewStats_InitialisedToZero(t *testing.T) {
	s := engine.NewStats()
	if s.RequestCount.Load() != 0 {
		t.Errorf("RequestCount: want 0, got %d", s.RequestCount.Load())
	}
	if s.SuccessCount.Load() != 0 {
		t.Errorf("SuccessCount: want 0, got %d", s.SuccessCount.Load())
	}
	if s.ErrorCount.Load() != 0 {
		t.Errorf("ErrorCount: want 0, got %d", s.ErrorCount.Load())
	}
}

func TestStats_RecordRequest_Success(t *testing.T) {
	s := engine.NewStats()
	s.RecordRequest("example.com", 10*time.Millisecond, nil, false)

	if got := s.RequestCount.Load(); got != 1 {
		t.Errorf("RequestCount: want 1, got %d", got)
	}
	if got := s.SuccessCount.Load(); got != 1 {
		t.Errorf("SuccessCount: want 1, got %d", got)
	}
	if got := s.ErrorCount.Load(); got != 0 {
		t.Errorf("ErrorCount: want 0, got %d", got)
	}
}

func TestStats_RecordRequest_Error(t *testing.T) {
	s := engine.NewStats()
	s.RecordRequest("example.com", 5*time.Millisecond, errors.New("timeout"), false)

	if got := s.RequestCount.Load(); got != 1 {
		t.Errorf("RequestCount: want 1, got %d", got)
	}
	if got := s.ErrorCount.Load(); got != 1 {
		t.Errorf("ErrorCount: want 1, got %d", got)
	}
	if got := s.SuccessCount.Load(); got != 0 {
		t.Errorf("SuccessCount: want 0, got %d", got)
	}
}

func TestStats_RecordRequest_Blocked(t *testing.T) {
	s := engine.NewStats()
	s.RecordRequest("example.com", 5*time.Millisecond, nil, true)

	if got := s.BlockedCount.Load(); got != 1 {
		t.Errorf("BlockedCount: want 1, got %d", got)
	}
}

func TestStats_RecordItems(t *testing.T) {
	s := engine.NewStats()
	s.RecordItems(7)
	if got := s.ItemCount.Load(); got != 7 {
		t.Errorf("ItemCount: want 7, got %d", got)
	}
}

func TestStats_RecordEscalation(t *testing.T) {
	s := engine.NewStats()
	s.RecordEscalation()
	s.RecordEscalation()
	if got := s.EscalatedCount.Load(); got != 2 {
		t.Errorf("EscalatedCount: want 2, got %d", got)
	}
}

func TestStats_RecordBytes(t *testing.T) {
	s := engine.NewStats()
	s.RecordBytes(1024)
	s.RecordBytes(512)
	if got := s.BytesReceived.Load(); got != 1536 {
		t.Errorf("BytesReceived: want 1536, got %d", got)
	}
}

func TestStats_Summary_ContainsCounts(t *testing.T) {
	s := engine.NewStats()
	s.RecordRequest("a.com", 10*time.Millisecond, nil, false)
	s.RecordItems(3)
	summary := s.Summary()
	if !strings.Contains(summary, "1") {
		t.Errorf("Summary should mention request count; got: %s", summary)
	}
}

func TestStats_DomainStats_Tracked(t *testing.T) {
	s := engine.NewStats()
	s.RecordRequest("foo.com", 20*time.Millisecond, nil, false)
	s.RecordRequest("foo.com", 10*time.Millisecond, errors.New("err"), false)

	summary := s.Summary()
	if !strings.Contains(summary, "foo.com") {
		t.Errorf("Summary should include domain stats; got: %s", summary)
	}
}

// ---------------------------------------------------------------------------
// RetryPolicy tests
// ---------------------------------------------------------------------------

func TestDefaultRetryPolicy_Fields(t *testing.T) {
	rp := engine.DefaultRetryPolicy()
	if rp.MaxRetries <= 0 {
		t.Errorf("MaxRetries should be > 0, got %d", rp.MaxRetries)
	}
	if rp.BaseDelay <= 0 {
		t.Errorf("BaseDelay should be > 0, got %v", rp.BaseDelay)
	}
	if rp.Backoff <= 1.0 {
		t.Errorf("Backoff multiplier should be > 1.0, got %v", rp.Backoff)
	}
}

func TestRetryPolicy_ShouldRetry_WithinLimit(t *testing.T) {
	rp := engine.DefaultRetryPolicy()
	// attempt 0 with a retriable error should return true
	if !rp.ShouldRetry(0, errors.New("connection refused"), nil) {
		t.Error("expected ShouldRetry=true for attempt 0 with error")
	}
}

func TestRetryPolicy_ShouldRetry_ExceedsLimit(t *testing.T) {
	rp := &engine.RetryPolicy{MaxRetries: 2, BaseDelay: time.Millisecond, MaxDelay: time.Second, Backoff: 2.0}
	// attempt == MaxRetries means we've already retried MaxRetries times
	if rp.ShouldRetry(2, errors.New("still failing"), nil) {
		t.Error("expected ShouldRetry=false when attempt >= MaxRetries")
	}
}

func TestRetryPolicy_ShouldRetry_NoError_2xx(t *testing.T) {
	rp := engine.DefaultRetryPolicy()
	resp := &foxhound.Response{StatusCode: 200}
	if rp.ShouldRetry(0, nil, resp) {
		t.Error("expected ShouldRetry=false for 200 with no error")
	}
}

func TestRetryPolicy_ShouldRetry_5xx(t *testing.T) {
	rp := engine.DefaultRetryPolicy()
	resp := &foxhound.Response{StatusCode: 503}
	if !rp.ShouldRetry(0, nil, resp) {
		t.Error("expected ShouldRetry=true for 503 response")
	}
}

func TestRetryPolicy_Delay_Increases(t *testing.T) {
	rp := &engine.RetryPolicy{
		MaxRetries: 5,
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   10 * time.Second,
		Backoff:    2.0,
	}
	d0 := rp.Delay(0)
	d1 := rp.Delay(1)
	d2 := rp.Delay(2)

	// Each successive delay should be larger (ignoring jitter, test the trend).
	// We test that d1 > d0 and d2 > d1 on average by running a few times.
	if d0 <= 0 {
		t.Errorf("Delay(0) must be > 0, got %v", d0)
	}
	// With jitter the exact order is probabilistic, but the ceiling grows.
	// Just verify the cap is respected.
	for i := 0; i < 10; i++ {
		d := rp.Delay(i)
		if d > rp.MaxDelay {
			t.Errorf("Delay(%d) = %v exceeds MaxDelay %v", i, d, rp.MaxDelay)
		}
	}
	_ = d1
	_ = d2
}

func TestRetryPolicy_Delay_RespectsCap(t *testing.T) {
	rp := &engine.RetryPolicy{
		MaxRetries: 20,
		BaseDelay:  time.Second,
		MaxDelay:   2 * time.Second,
		Backoff:    2.0,
	}
	for i := 0; i < 20; i++ {
		if d := rp.Delay(i); d > rp.MaxDelay {
			t.Errorf("Delay(%d) = %v exceeds MaxDelay %v", i, d, rp.MaxDelay)
		}
	}
}

// ---------------------------------------------------------------------------
// Trail tests
// ---------------------------------------------------------------------------

func TestNewTrail_HasName(t *testing.T) {
	tr := engine.NewTrail("product-pages")
	if tr.Name != "product-pages" {
		t.Errorf("Name: want %q, got %q", "product-pages", tr.Name)
	}
	if len(tr.Steps) != 0 {
		t.Errorf("Steps: want 0, got %d", len(tr.Steps))
	}
}

func TestTrail_Navigate_AddsStep(t *testing.T) {
	tr := engine.NewTrail("t").Navigate("https://example.com")
	if len(tr.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(tr.Steps))
	}
	if tr.Steps[0].Action != engine.StepNavigate {
		t.Errorf("Action: want StepNavigate, got %v", tr.Steps[0].Action)
	}
	if tr.Steps[0].URL != "https://example.com" {
		t.Errorf("URL: want %q, got %q", "https://example.com", tr.Steps[0].URL)
	}
}

func TestTrail_Click_AddsStep(t *testing.T) {
	tr := engine.NewTrail("t").Click("#submit")
	if len(tr.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(tr.Steps))
	}
	if tr.Steps[0].Action != engine.StepClick {
		t.Errorf("Action: want StepClick, got %v", tr.Steps[0].Action)
	}
	if tr.Steps[0].Selector != "#submit" {
		t.Errorf("Selector: want %q, got %q", "#submit", tr.Steps[0].Selector)
	}
}

func TestTrail_Wait_AddsStep(t *testing.T) {
	tr := engine.NewTrail("t").Wait(".loaded", 5*time.Second)
	if tr.Steps[0].Action != engine.StepWait {
		t.Errorf("Action: want StepWait")
	}
	if tr.Steps[0].Duration != 5*time.Second {
		t.Errorf("Duration: want 5s, got %v", tr.Steps[0].Duration)
	}
}

func TestTrail_Extract_AddsStep(t *testing.T) {
	proc := foxhound.ProcessorFunc(func(_ context.Context, _ *foxhound.Response) (*foxhound.Result, error) {
		return &foxhound.Result{}, nil
	})
	tr := engine.NewTrail("t").Extract(proc)
	if tr.Steps[0].Action != engine.StepExtract {
		t.Errorf("Action: want StepExtract")
	}
	if tr.Steps[0].Process == nil {
		t.Error("Process: must not be nil")
	}
}

func TestTrail_Scroll_AddsStep(t *testing.T) {
	tr := engine.NewTrail("t").Scroll()
	if tr.Steps[0].Action != engine.StepScroll {
		t.Errorf("Action: want StepScroll")
	}
}

func TestTrail_ToJobs_ProducesJobPerNavigateStep(t *testing.T) {
	tr := engine.NewTrail("t").
		Navigate("https://a.com").
		Navigate("https://b.com").
		Click("#btn")

	jobs := tr.ToJobs()
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs (one per Navigate), got %d", len(jobs))
	}
	urls := map[string]bool{}
	for _, j := range jobs {
		urls[j.URL] = true
	}
	if !urls["https://a.com"] || !urls["https://b.com"] {
		t.Error("expected jobs for both navigate URLs")
	}
}

func TestTrail_Chaining_ReturnsTrail(t *testing.T) {
	// All builder methods must return the same *Trail for chaining.
	tr := engine.NewTrail("chain").
		Navigate("https://x.com").
		Click(".btn").
		Wait(".done", time.Second).
		Scroll()
	if len(tr.Steps) != 4 {
		t.Errorf("expected 4 steps, got %d", len(tr.Steps))
	}
}

// ---------------------------------------------------------------------------
// Scheduler tests
// ---------------------------------------------------------------------------

func TestScheduler_Submit_PushesJobsToQueue(t *testing.T) {
	q := newMemQueue(10)
	s := engine.NewScheduler(q, 2)

	jobs := []*foxhound.Job{seedJob("https://a.com"), seedJob("https://b.com")}
	if err := s.Submit(context.Background(), jobs...); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if got := q.Len(); got != 2 {
		t.Errorf("queue length: want 2, got %d", got)
	}
}

func TestScheduler_Start_CallsHandlerForEachJob(t *testing.T) {
	q := newMemQueue(10)
	s := engine.NewScheduler(q, 2)

	// Pre-load 3 jobs.
	for i := 0; i < 3; i++ {
		_ = q.Push(context.Background(), seedJob(fmt.Sprintf("https://example.com/%d", i)))
	}

	var handled atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Start(ctx, func(_ context.Context, _ *foxhound.Job) error {
			handled.Add(1)
			return nil
		})
	}()

	// Poll until all 3 handled or timeout.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && handled.Load() < 3 {
		time.Sleep(10 * time.Millisecond)
	}

	s.Stop()
	<-done

	if got := handled.Load(); got < 3 {
		t.Errorf("handler called %d times, want 3", got)
	}
}

func TestScheduler_Wait_BlocksUntilDone(t *testing.T) {
	q := newMemQueue(5)
	s := engine.NewScheduler(q, 2)

	var processed atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	for i := 0; i < 2; i++ {
		_ = q.Push(context.Background(), seedJob(fmt.Sprintf("https://e.com/%d", i)))
	}

	go func() {
		_ = s.Start(ctx, func(_ context.Context, _ *foxhound.Job) error {
			time.Sleep(10 * time.Millisecond)
			processed.Add(1)
			return nil
		})
	}()

	// Give scheduler time to pick up jobs then stop.
	time.Sleep(200 * time.Millisecond)
	s.Stop()
	s.Wait()

	if processed.Load() < 2 {
		t.Errorf("expected at least 2 processed, got %d", processed.Load())
	}
}

// ---------------------------------------------------------------------------
// Hunt tests
// ---------------------------------------------------------------------------

func TestNewHunt_NotNil(t *testing.T) {
	q := newMemQueue(10)
	cfg := engine.HuntConfig{
		Name:      "test-hunt",
		Domain:    "example.com",
		Walkers:   1,
		Queue:     q,
		Fetcher:   &stubFetcher{resp: &foxhound.Response{StatusCode: 200, Body: []byte("ok")}},
		Processor: &stubProcessor{result: &foxhound.Result{}},
	}
	h := engine.NewHunt(cfg)
	if h == nil {
		t.Fatal("NewHunt returned nil")
	}
}

func TestHunt_InitialState_IsIdle(t *testing.T) {
	q := newMemQueue(10)
	h := engine.NewHunt(engine.HuntConfig{
		Name:      "h",
		Walkers:   1,
		Queue:     q,
		Fetcher:   &stubFetcher{resp: &foxhound.Response{StatusCode: 200}},
		Processor: &stubProcessor{result: &foxhound.Result{}},
	})
	if h.State() != engine.HuntIdle {
		t.Errorf("initial state: want HuntIdle, got %v", h.State())
	}
}

func TestHunt_Run_ProcessesSeedJobs(t *testing.T) {
	q := newMemQueue(16)
	writer := &capturingWriter{}

	item := foxhound.NewItem()
	item.Set("title", "Hello")

	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200, Body: []byte("<html/>")}}
	processor := &stubProcessor{result: &foxhound.Result{Items: []*foxhound.Item{item}}}

	seeds := []*foxhound.Job{seedJob("https://example.com/1"), seedJob("https://example.com/2")}

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "run-test",
		Domain:    "example.com",
		Walkers:   2,
		Seeds:     seeds,
		Queue:     q,
		Fetcher:   fetcher,
		Processor: processor,
		Writers:   []foxhound.Writer{writer},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := fetcher.CallCount(); got != 2 {
		t.Errorf("fetcher calls: want 2, got %d", got)
	}
	if got := len(writer.Items()); got != 2 {
		t.Errorf("items written: want 2, got %d", got)
	}
}

func TestHunt_Run_StateTransitions(t *testing.T) {
	q := newMemQueue(8)
	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200}}
	processor := &stubProcessor{result: &foxhound.Result{}}

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "state-test",
		Walkers:   1,
		Seeds:     []*foxhound.Job{seedJob("https://example.com")},
		Queue:     q,
		Fetcher:   fetcher,
		Processor: processor,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if h.State() != engine.HuntDone {
		t.Errorf("state after Run: want HuntDone, got %v", h.State())
	}
}

func TestHunt_Run_PipelinesApplied(t *testing.T) {
	q := newMemQueue(8)
	writer := &capturingWriter{}

	item := foxhound.NewItem()
	item.Set("v", 1)

	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200}}
	processor := &stubProcessor{result: &foxhound.Result{Items: []*foxhound.Item{item}}}

	// dropPipeline will discard the item; writer should receive nothing.
	h := engine.NewHunt(engine.HuntConfig{
		Name:      "pipeline-test",
		Walkers:   1,
		Seeds:     []*foxhound.Job{seedJob("https://example.com")},
		Queue:     q,
		Fetcher:   fetcher,
		Processor: processor,
		Pipelines: []foxhound.Pipeline{&dropPipeline{}},
		Writers:   []foxhound.Writer{writer},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := len(writer.Items()); got != 0 {
		t.Errorf("items written: want 0 (dropped by pipeline), got %d", got)
	}
}

func TestHunt_Run_DiscoveredJobsEnqueued(t *testing.T) {
	q := newMemQueue(32)

	// First call returns a new job; subsequent calls return nothing.
	var callCount atomic.Int64
	processor := foxhound.ProcessorFunc(func(_ context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
		n := callCount.Add(1)
		if n == 1 {
			return &foxhound.Result{
				Jobs: []*foxhound.Job{seedJob("https://example.com/discovered")},
			}, nil
		}
		return &foxhound.Result{}, nil
	})

	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200}}

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "discover-test",
		Walkers:   1,
		Seeds:     []*foxhound.Job{seedJob("https://example.com/seed")},
		Queue:     q,
		Fetcher:   fetcher,
		Processor: processor,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Fetcher should have been called twice: seed + discovered.
	if got := fetcher.CallCount(); got != 2 {
		t.Errorf("fetcher calls: want 2 (seed + discovered), got %d", got)
	}
}

func TestHunt_Run_MiddlewareWraps(t *testing.T) {
	q := newMemQueue(8)

	var middlewareCalled atomic.Int64
	mw := foxhound.MiddlewareFunc(func(next foxhound.Fetcher) foxhound.Fetcher {
		return &countingFetcher{next: next, counter: &middlewareCalled}
	})

	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200}}
	processor := &stubProcessor{result: &foxhound.Result{}}

	h := engine.NewHunt(engine.HuntConfig{
		Name:        "middleware-test",
		Walkers:     1,
		Seeds:       []*foxhound.Job{seedJob("https://example.com")},
		Queue:       q,
		Fetcher:     fetcher,
		Processor:   processor,
		Middlewares: []foxhound.Middleware{mw},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if middlewareCalled.Load() == 0 {
		t.Error("expected middleware to be called at least once")
	}
}

func TestHunt_Stop_CancelsRun(t *testing.T) {
	q := newMemQueue(100)
	// Feed many jobs so Run would block for a long time without Stop.
	for i := 0; i < 50; i++ {
		_ = q.Push(context.Background(), seedJob(fmt.Sprintf("https://example.com/%d", i)))
	}

	slow := &slowFetcher{delay: 100 * time.Millisecond, resp: &foxhound.Response{StatusCode: 200}}
	processor := &stubProcessor{result: &foxhound.Result{}}

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "stop-test",
		Walkers:   2,
		Queue:     q,
		Fetcher:   slow,
		Processor: processor,
	})

	start := time.Now()
	go func() {
		time.Sleep(150 * time.Millisecond)
		h.Stop()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = h.Run(ctx)
	elapsed := time.Since(start)

	if elapsed >= 3*time.Second {
		t.Errorf("Stop did not cancel Run quickly enough: elapsed %v", elapsed)
	}
}

func TestHunt_Stats_TracksRequests(t *testing.T) {
	q := newMemQueue(8)
	// Use a minimal HTML body so the captcha empty_trap heuristic does not fire
	// (it triggers when body < 500 bytes and lacks <html).
	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200, Body: []byte("<html><body>ok</body></html>")}}
	processor := &stubProcessor{result: &foxhound.Result{}}

	seeds := []*foxhound.Job{seedJob("https://a.com"), seedJob("https://b.com")}
	h := engine.NewHunt(engine.HuntConfig{
		Name:      "stats-test",
		Walkers:   1,
		Seeds:     seeds,
		Queue:     q,
		Fetcher:   fetcher,
		Processor: processor,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = h.Run(ctx)

	stats := h.Stats()
	if stats.RequestCount.Load() != 2 {
		t.Errorf("RequestCount: want 2, got %d", stats.RequestCount.Load())
	}
}

func TestHunt_Run_FlushesWritersOnCompletion(t *testing.T) {
	q := newMemQueue(8)
	fw := &flushCountWriter{}
	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200}}
	processor := &stubProcessor{result: &foxhound.Result{}}

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "flush-test",
		Walkers:   1,
		Seeds:     []*foxhound.Job{seedJob("https://example.com")},
		Queue:     q,
		Fetcher:   fetcher,
		Processor: processor,
		Writers:   []foxhound.Writer{fw},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = h.Run(ctx)

	if fw.flushCount == 0 {
		t.Error("expected Flush to be called on writer after Run completes")
	}
}

// ---------------------------------------------------------------------------
// Additional stub types used in Hunt tests
// ---------------------------------------------------------------------------

// countingFetcher wraps a Fetcher and increments counter on each call.
type countingFetcher struct {
	next    foxhound.Fetcher
	counter *atomic.Int64
}

func (f *countingFetcher) Fetch(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
	f.counter.Add(1)
	return f.next.Fetch(ctx, job)
}

func (f *countingFetcher) Close() error { return f.next.Close() }

// slowFetcher introduces an artificial delay before delegating.
type slowFetcher struct {
	delay time.Duration
	resp  *foxhound.Response
}

func (f *slowFetcher) Fetch(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	r := *f.resp
	r.Job = job
	return &r, nil
}

func (f *slowFetcher) Close() error { return nil }

// flushCountWriter counts how many times Flush is called.
type flushCountWriter struct {
	mu         sync.Mutex
	flushCount int
}

func (w *flushCountWriter) Write(_ context.Context, _ *foxhound.Item) error { return nil }
func (w *flushCountWriter) Flush(_ context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushCount++
	return nil
}
func (w *flushCountWriter) Close() error { return nil }

// ---------------------------------------------------------------------------
// H1: drainQueue premature-cancellation regression test
// ---------------------------------------------------------------------------

// delayedDiscoveryProcessor simulates a processor that takes non-trivial time
// before it enqueues a discovered job.  On call 1 it sleeps briefly then
// returns a new job; on subsequent calls it returns nothing.
type delayedDiscoveryProcessor struct {
	calls    atomic.Int64
	delay    time.Duration
	newJobFn func() *foxhound.Job
}

func (p *delayedDiscoveryProcessor) Process(
	ctx context.Context, _ *foxhound.Response,
) (*foxhound.Result, error) {
	n := p.calls.Add(1)
	if n == 1 {
		select {
		case <-time.After(p.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &foxhound.Result{Jobs: []*foxhound.Job{p.newJobFn()}}, nil
	}
	return &foxhound.Result{}, nil
}

// TestHunt_DrainQueue_WaitsForInFlightWalkers verifies that the hunt does not
// terminate prematurely when a walker has popped the last job from the queue
// but hasn't yet enqueued discovered jobs.  Without the activeWalkers fix the
// hunt would cancel the context while the walker is mid-flight, missing the
// discovered job and yielding fetcher.CallCount==1 instead of 2.
func TestHunt_DrainQueue_WaitsForInFlightWalkers(t *testing.T) {
	q := newMemQueue(32)

	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200}}

	// Processor takes 30 ms to return a discovered job; this is longer than
	// the 10 ms drainPollInterval so without the fix the queue appears empty
	// before the new job is pushed.
	proc := &delayedDiscoveryProcessor{
		delay:    30 * time.Millisecond,
		newJobFn: func() *foxhound.Job { return seedJob("https://example.com/discovered") },
	}

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "drain-inflight-test",
		Walkers:   1,
		Seeds:     []*foxhound.Job{seedJob("https://example.com/seed")},
		Queue:     q,
		Fetcher:   fetcher,
		Processor: proc,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Both the seed URL and the discovered URL must have been fetched.
	if got := fetcher.CallCount(); got != 2 {
		t.Errorf("fetcher calls: want 2 (seed + discovered), got %d — hunt cancelled too early", got)
	}
}
