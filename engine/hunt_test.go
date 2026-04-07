package engine_test

import (
	"context"
	"sync"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/engine"
)

// TestHunt_PoolFeedsSeeds verifies that URLs added to a Pool are drained and
// processed as seed jobs before walkers start.
func TestHunt_PoolFeedsSeeds(t *testing.T) {
	pool := engine.NewMemoryPool()
	pool.Add(context.Background(), "https://example.com/page/1")
	pool.Add(context.Background(), "https://example.com/page/2")
	pool.Add(context.Background(), "https://example.com/page/3")

	q := newMemQueue(16)

	var processed []string
	var mu sync.Mutex

	fetcher := &stubFetcher{
		resp: &foxhound.Response{
			StatusCode: 200,
			Body:       []byte("<html><body>ok</body></html>"),
		},
	}

	processor := foxhound.ProcessorFunc(func(_ context.Context, resp *foxhound.Response) (*foxhound.Result, error) {
		mu.Lock()
		processed = append(processed, resp.Job.URL)
		mu.Unlock()
		return &foxhound.Result{}, nil
	})

	h := engine.NewHunt(engine.HuntConfig{
		Name:             "pool-test",
		Domain:           "example.com",
		Walkers:          2,
		Pool:             pool,
		Fetcher:          fetcher,
		Processor:        processor,
		Queue:            q,
		PoolFetchMode:    foxhound.FetchStatic,
		PoolFetchModeSet: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(processed) != 3 {
		t.Fatalf("processed %d URLs, want 3; URLs: %v", len(processed), processed)
	}
}
