package engine_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/engine"
)

// TestHunt_DevelopmentMode verifies first run hits fetcher and subsequent
// identical run replays from disk cache without calling fetcher again.
func TestHunt_DevelopmentMode(t *testing.T) {
	dir := t.TempDir()

	fetcher := &stubFetcher{resp: &foxhound.Response{
		StatusCode: 200,
		Body:       []byte("<html>dev-mode</html>"),
	}}

	processor := foxhound.ProcessorFunc(func(_ context.Context, _ *foxhound.Response) (*foxhound.Result, error) {
		return &foxhound.Result{}, nil
	})

	run := func() {
		q := newMemQueue(8)
		_ = q.Push(context.Background(), &foxhound.Job{URL: "https://example.com/dev", Domain: "example.com"})
		h := engine.NewHunt(engine.HuntConfig{
			Name:      "dev-test",
			Domain:    "example.com",
			Walkers:   1,
			Fetcher:   fetcher,
			Processor: processor,
			Queue:     q,
		}).WithDevelopmentMode(dir)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.Run(ctx)
	}

	run()
	firstCalls := fetcher.CallCount()
	if firstCalls == 0 {
		t.Fatal("first run: expected fetcher call")
	}
	run()
	if fetcher.CallCount() != firstCalls {
		t.Fatalf("second run should replay from cache; calls went %d -> %d", firstCalls, fetcher.CallCount())
	}
}

// TestHunt_SessionRouting verifies jobs with SessionID route to the named
// session's fetcher, while jobs without SessionID use the default fetcher.
func TestHunt_SessionRouting(t *testing.T) {
	defaultFetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200, Body: []byte("default")}}
	inner := &stubFetcher{resp: &foxhound.Response{StatusCode: 200, Body: []byte("session")}}
	var sessionCalls atomic.Int64
	sessionFetcher := &countingFetcher{next: inner, counter: &sessionCalls}

	processor := foxhound.ProcessorFunc(func(_ context.Context, _ *foxhound.Response) (*foxhound.Result, error) {
		return &foxhound.Result{}, nil
	})

	q := newMemQueue(8)
	_ = q.Push(context.Background(), &foxhound.Job{URL: "https://example.com/a", Domain: "example.com"})
	_ = q.Push(context.Background(), &foxhound.Job{URL: "https://example.com/b", Domain: "example.com", SessionID: "index"})

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "session-test",
		Domain:    "example.com",
		Walkers:   1,
		Fetcher:   defaultFetcher,
		Processor: processor,
		Queue:     q,
	}).AddSession("index", engine.SessionConfig{Fetcher: sessionFetcher})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = h.Run(ctx)

	if defaultFetcher.CallCount() < 1 {
		t.Fatalf("default fetcher not invoked: %d", defaultFetcher.CallCount())
	}
	if sessionCalls.Load() < 1 {
		t.Fatalf("session fetcher not invoked: %d", sessionCalls.Load())
	}
	if got := h.Session("index"); got == nil {
		t.Fatal("Hunt.Session lookup returned nil")
	}
	if h.Session("missing") != nil {
		t.Fatal("unknown session should be nil")
	}
}

// TestHunt_BlockedDomains_NoOpWithoutCamoufox verifies the setters are safe
// to call with a non-Camoufox fetcher (no-op with warning, not a crash).
func TestHunt_BlockedDomains_NoOpWithoutCamoufox(t *testing.T) {
	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200}}
	processor := foxhound.ProcessorFunc(func(_ context.Context, _ *foxhound.Response) (*foxhound.Result, error) {
		return &foxhound.Result{}, nil
	})

	q := newMemQueue(4)
	_ = q.Push(context.Background(), &foxhound.Job{URL: "https://example.com/x", Domain: "example.com"})

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "block-test",
		Domain:    "example.com",
		Walkers:   1,
		Fetcher:   fetcher,
		Processor: processor,
		Queue:     q,
	}).
		WithBlockedDomains("ads.example.com", "tracker.example.com").
		WithDisableResources("image", "media")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
}
