package engine_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/engine"
)

// ---------------------------------------------------------------------------
// Helpers shared across stream tests
// ---------------------------------------------------------------------------

// streamItem produces a simple Item with a counter field set to n.
func streamItem(n int) *foxhound.Item {
	it := foxhound.NewItem()
	it.Set("n", n)
	return it
}

// htmlBody is a minimal valid HTML body that will not trigger captcha/detect's
// empty_trap heuristic (which fires when body < 500 bytes and lacks <html).
const htmlBody = "<html><body><p>content</p></body></html>"

// huntWithItems builds a Hunt that processes one seed job and returns n items.
func huntWithItems(n int) (*engine.Hunt, *memQueue) {
	q := newMemQueue(64)

	items := make([]*foxhound.Item, n)
	for i := 0; i < n; i++ {
		items[i] = streamItem(i)
	}

	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200, Body: []byte(htmlBody)}}
	processor := &stubProcessor{result: &foxhound.Result{Items: items}}

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "stream-test",
		Domain:    "example.com",
		Walkers:   1,
		Seeds:     []*foxhound.Job{seedJob("https://example.com")},
		Queue:     q,
		Fetcher:   fetcher,
		Processor: processor,
	})
	return h, q
}

// ---------------------------------------------------------------------------
// TestStream_ReceivesItems
// ---------------------------------------------------------------------------

// TestStream_ReceivesItems verifies that every item produced by the hunt
// arrives on the channel returned by Stream.
func TestStream_ReceivesItems(t *testing.T) {
	const wantItems = 5
	h, _ := huntWithItems(wantItems)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := h.Stream(ctx)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var got int
	for range ch {
		got++
	}

	if got != wantItems {
		t.Errorf("received %d items, want %d", got, wantItems)
	}
}

// ---------------------------------------------------------------------------
// TestStream_ChannelCloses
// ---------------------------------------------------------------------------

// TestStream_ChannelCloses verifies that the channel returned by Stream is
// closed once the hunt finishes — so range loops terminate naturally.
func TestStream_ChannelCloses(t *testing.T) {
	h, _ := huntWithItems(3)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := h.Stream(ctx)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Drain to completion; we're just verifying the channel closes.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range ch {
		}
	}()

	select {
	case <-done:
		// channel closed — pass
	case <-time.After(10 * time.Second):
		t.Error("stream channel did not close after hunt completed")
	}
}

// ---------------------------------------------------------------------------
// TestStream_ContextCancel
// ---------------------------------------------------------------------------

// TestStream_ContextCancel verifies that cancelling the context causes the
// channel to close promptly, stopping item delivery.
func TestStream_ContextCancel(t *testing.T) {
	// Use a large item count to ensure the hunt would not finish on its own.
	// We cancel early, so only a subset should arrive.
	q := newMemQueue(256)

	// Processor that counts how many times it is called and pauses briefly
	// so we have time to cancel before all items arrive.
	var callCount atomic.Int64
	processor := foxhound.ProcessorFunc(func(_ context.Context, _ *foxhound.Response) (*foxhound.Result, error) {
		callCount.Add(1)
		items := []*foxhound.Item{streamItem(0)}
		// Discover a new job to keep the queue alive.
		next := seedJob("https://example.com/next")
		return &foxhound.Result{Items: items, Jobs: []*foxhound.Job{next}}, nil
	})
	fetcher := &stubFetcher{resp: &foxhound.Response{StatusCode: 200, Body: []byte("ok")}}

	h := engine.NewHunt(engine.HuntConfig{
		Name:      "stream-cancel-test",
		Domain:    "example.com",
		Walkers:   1,
		Seeds:     []*foxhound.Job{seedJob("https://example.com/seed")},
		Queue:     q,
		Fetcher:   fetcher,
		Processor: processor,
	})

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := h.Stream(ctx)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Cancel after the first item arrives.
	go func() {
		<-ch      // receive one item
		cancel()  // now cancel
	}()

	// Drain the channel; it must close after cancellation.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range ch {
		}
	}()

	select {
	case <-done:
		// pass — channel closed after cancel
	case <-time.After(10 * time.Second):
		t.Error("stream channel did not close after context cancel")
	}
}

// ---------------------------------------------------------------------------
// TestStream_RunStillWorks
// ---------------------------------------------------------------------------

// TestStream_RunStillWorks verifies that a Hunt created without Stream still
// works via the normal Run path, i.e., Stream is fully optional.
func TestStream_RunStillWorks(t *testing.T) {
	h, _ := huntWithItems(2)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run (no Stream): %v", err)
	}

	if h.State() != engine.HuntDone {
		t.Errorf("state after Run: want HuntDone, got %v", h.State())
	}
}

// ---------------------------------------------------------------------------
// TestStreamWithStats
// ---------------------------------------------------------------------------

// TestStreamWithStats verifies that both item events and stats events are
// delivered on the StreamEvent channel.
func TestStreamWithStats(t *testing.T) {
	const wantItems = 4
	h, _ := huntWithItems(wantItems)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use a short stats interval so at least one stats event is emitted.
	ch, err := h.StreamWithStats(ctx, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("StreamWithStats: %v", err)
	}

	var itemCount, statsCount int
	for ev := range ch {
		if ev.Item != nil {
			itemCount++
		}
		if ev.Stats != nil {
			statsCount++
		}
	}

	if itemCount != wantItems {
		t.Errorf("item events: want %d, got %d", wantItems, itemCount)
	}
	if statsCount == 0 {
		t.Error("expected at least one stats event, got none")
	}
}

// TestStreamWithStats_ChannelCloses verifies that StreamWithStats channel
// closes when the hunt finishes, just like Stream.
func TestStreamWithStats_ChannelCloses(t *testing.T) {
	h, _ := huntWithItems(2)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := h.StreamWithStats(ctx, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("StreamWithStats: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range ch {
		}
	}()

	select {
	case <-done:
		// pass
	case <-time.After(10 * time.Second):
		t.Error("StreamWithStats channel did not close after hunt completed")
	}
}
