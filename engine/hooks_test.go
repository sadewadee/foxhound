package engine_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/engine"
)

// TestHunt_OnStart_CalledBeforeWalkers verifies that the OnStart hook is
// invoked after seeds are queued but before walkers process jobs.
func TestHunt_OnStart_CalledBeforeWalkers(t *testing.T) {
	q := newMemQueue(16)
	var started atomic.Bool

	item := foxhound.NewItem()
	item.Set("k", "v")

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "onstart-test",
		Walkers:   1,
		Seeds:     []*foxhound.Job{seedJob("https://example.com")},
		Queue:     q,
		Fetcher:   &stubFetcher{resp: &foxhound.Response{StatusCode: 200, Body: []byte("<html><body>ok</body></html>")}},
		Processor: &stubProcessor{result: &foxhound.Result{Items: []*foxhound.Item{item}}},
		OnStart: func(_ context.Context) {
			started.Store(true)
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !started.Load() {
		t.Error("OnStart was not called")
	}
}

// TestHunt_OnClose_CalledAfterCompletion verifies OnClose receives stats.
func TestHunt_OnClose_CalledAfterCompletion(t *testing.T) {
	q := newMemQueue(16)
	var closedStats *engine.Stats

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "onclose-test",
		Walkers:   1,
		Seeds:     []*foxhound.Job{seedJob("https://example.com")},
		Queue:     q,
		Fetcher:   &stubFetcher{resp: &foxhound.Response{StatusCode: 200, Body: []byte("<html><body>ok</body></html>")}},
		Processor: &stubProcessor{result: &foxhound.Result{}},
		OnClose: func(_ context.Context, stats *engine.Stats) {
			closedStats = stats
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if closedStats == nil {
		t.Error("OnClose was not called or stats was nil")
	}
}

// TestHunt_OnError_CalledOnFetchFailure verifies the error hook fires.
func TestHunt_OnError_CalledOnFetchFailure(t *testing.T) {
	q := newMemQueue(16)
	var errorCount atomic.Int64

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "onerror-test",
		Walkers:   1,
		Seeds:     []*foxhound.Job{seedJob("https://example.com")},
		Queue:     q,
		Fetcher:   &stubFetcher{err: errTest},
		Processor: &stubProcessor{result: &foxhound.Result{}},
		OnError: func(_ context.Context, _ *foxhound.Job, _ error) {
			errorCount.Add(1)
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_ = h.Run(ctx)

	if errorCount.Load() == 0 {
		t.Error("OnError was not called for fetch failure")
	}
}

// TestHunt_OnItem_CalledPerItem verifies per-item callback.
func TestHunt_OnItem_CalledPerItem(t *testing.T) {
	q := newMemQueue(16)
	var itemCount atomic.Int64

	item1 := foxhound.NewItem()
	item1.Set("n", 1)
	item2 := foxhound.NewItem()
	item2.Set("n", 2)

	h := engine.NewHunt(engine.HuntConfig{
		Name:    "onitem-test",
		Walkers: 1,
		Seeds:   []*foxhound.Job{seedJob("https://example.com")},
		Queue:   q,
		Fetcher: &stubFetcher{resp: &foxhound.Response{StatusCode: 200, Body: []byte("<html><body>ok</body></html>")}},
		Processor: &stubProcessor{result: &foxhound.Result{
			Items: []*foxhound.Item{item1, item2},
		}},
		OnItem: func(_ context.Context, _ *foxhound.Job, _ *foxhound.Item) {
			itemCount.Add(1)
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if itemCount.Load() != 2 {
		t.Errorf("OnItem called %d times, want 2", itemCount.Load())
	}
}

// TestHunt_ItemCallback_StreamingYield verifies inline item processing.
func TestHunt_ItemCallback_StreamingYield(t *testing.T) {
	q := newMemQueue(16)
	var received []*foxhound.Item

	item := foxhound.NewItem()
	item.Set("data", "test")

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "callback-test",
		Walkers:   1,
		Seeds:     []*foxhound.Job{seedJob("https://example.com")},
		Queue:     q,
		Fetcher:   &stubFetcher{resp: &foxhound.Response{StatusCode: 200, Body: []byte("<html><body>ok</body></html>")}},
		Processor: &stubProcessor{result: &foxhound.Result{Items: []*foxhound.Item{item}}},
		ItemCallback: func(_ context.Context, item *foxhound.Item) {
			received = append(received, item)
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(received) != 1 {
		t.Errorf("ItemCallback received %d items, want 1", len(received))
	}
}

// TestHunt_PageActions_InjectsEvaluateSteps verifies JS action injection.
func TestHunt_PageActions_InjectsEvaluateSteps(t *testing.T) {
	q := newMemQueue(16)

	var capturedSteps []foxhound.JobStep
	fetcher := foxhound.FetcherFunc(func(_ context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		capturedSteps = append(capturedSteps, job.Steps...)
		return &foxhound.Response{
			StatusCode: 200,
			Body:       []byte("<html><body>ok</body></html>"),
			Job:        job,
		}, nil
	})

	h := engine.NewHunt(engine.HuntConfig{
		Name:    "pageactions-test",
		Walkers: 1,
		Seeds: []*foxhound.Job{
			{
				ID:        "https://example.com",
				URL:       "https://example.com",
				Method:    "GET",
				FetchMode: foxhound.FetchBrowser,
				Domain:    "example.com",
				CreatedAt: time.Now(),
			},
		},
		Queue:       q,
		Fetcher:     fetcher,
		Processor:   &stubProcessor{result: &foxhound.Result{}},
		PageActions: []string{"document.querySelector('.lazy').click()", "window.scrollTo(0, 1000)"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(capturedSteps) != 2 {
		t.Fatalf("expected 2 evaluate steps injected, got %d", len(capturedSteps))
	}

	for _, step := range capturedSteps {
		if step.Action != foxhound.JobStepEvaluate {
			t.Errorf("step action = %d, want %d (JobStepEvaluate)", step.Action, foxhound.JobStepEvaluate)
		}
	}

	if capturedSteps[0].Script != "document.querySelector('.lazy').click()" {
		t.Errorf("step[0].Script = %q", capturedSteps[0].Script)
	}
}

// TestHunt_PageActions_OnlyBrowserMode verifies actions are only injected
// on browser/auto mode jobs.
func TestHunt_PageActions_OnlyBrowserMode(t *testing.T) {
	q := newMemQueue(16)

	var capturedSteps []foxhound.JobStep
	fetcher := foxhound.FetcherFunc(func(_ context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		capturedSteps = append(capturedSteps, job.Steps...)
		return &foxhound.Response{
			StatusCode: 200,
			Body:       []byte("<html><body>ok</body></html>"),
			Job:        job,
		}, nil
	})

	h := engine.NewHunt(engine.HuntConfig{
		Name:    "pageactions-static-test",
		Walkers: 1,
		Seeds: []*foxhound.Job{
			{
				ID:        "https://example.com",
				URL:       "https://example.com",
				Method:    "GET",
				FetchMode: foxhound.FetchStatic, // static mode
				Domain:    "example.com",
				CreatedAt: time.Now(),
			},
		},
		Queue:       q,
		Fetcher:     fetcher,
		Processor:   &stubProcessor{result: &foxhound.Result{}},
		PageActions: []string{"console.log('test')"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Static mode jobs should NOT have page actions injected.
	if len(capturedSteps) != 0 {
		t.Errorf("static mode job got %d steps, want 0", len(capturedSteps))
	}
}

// errTest is a reusable test error.
var errTest = fmt.Errorf("test error")
