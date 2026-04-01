package middleware_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/middleware"
)

// --- helpers ---

// mockFetcher is a controllable Fetcher for testing.
type mockFetcher struct {
	mu       sync.Mutex
	calls    int
	response *foxhound.Response
	err      error
	delay    time.Duration
}

func (m *mockFetcher) Fetch(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
	m.mu.Lock()
	m.calls++
	resp := m.response
	err := m.err
	delay := m.delay
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return resp, err
}

func (m *mockFetcher) Close() error { return nil }

func (m *mockFetcher) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func okResponse(job *foxhound.Job) *foxhound.Response {
	return &foxhound.Response{StatusCode: 200, Job: job, Headers: make(http.Header)}
}

func newJob(url string) *foxhound.Job {
	return &foxhound.Job{URL: url, Domain: "example.com"}
}

// --- Chain tests ---

func TestChainAppliesMiddlewaresInOrder(t *testing.T) {
	var order []int
	makeMiddleware := func(n int) foxhound.Middleware {
		return foxhound.MiddlewareFunc(func(next foxhound.Fetcher) foxhound.Fetcher {
			return foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
				order = append(order, n)
				return next.Fetch(ctx, job)
			})
		})
	}

	mock := &mockFetcher{response: okResponse(newJob("https://example.com"))}
	chain := middleware.Chain(makeMiddleware(1), makeMiddleware(2), makeMiddleware(3))
	fetcher := chain.Wrap(mock)

	job := newJob("https://example.com")
	_, err := fetcher.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("middlewares not applied in order: %v", order)
	}
}

func TestChainEmpty(t *testing.T) {
	mock := &mockFetcher{response: okResponse(newJob("https://example.com"))}
	chain := middleware.Chain()
	fetcher := chain.Wrap(mock)

	job := newJob("https://example.com")
	_, err := fetcher.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("empty chain should pass through: %v", err)
	}
}

// --- RateLimit tests ---

func TestRateLimitAllowsRequestsUnderLimit(t *testing.T) {
	mock := &mockFetcher{response: okResponse(newJob("https://example.com"))}
	rl := middleware.NewRateLimit(100, 10) // generous limit
	fetcher := rl.Wrap(mock)

	job := &foxhound.Job{URL: "https://example.com", Domain: "example.com"}
	_, err := fetcher.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRateLimitCreatesPerDomainLimiters(t *testing.T) {
	mock := &mockFetcher{response: &foxhound.Response{StatusCode: 200, Headers: make(http.Header)}}
	rl := middleware.NewRateLimit(100, 100)
	fetcher := rl.Wrap(mock)

	for _, domain := range []string{"a.com", "b.com", "a.com"} {
		job := &foxhound.Job{URL: "https://" + domain + "/", Domain: domain}
		if _, err := fetcher.Fetch(context.Background(), job); err != nil {
			t.Fatalf("domain %s: unexpected error: %v", domain, err)
		}
	}
	if mock.CallCount() != 3 {
		t.Errorf("expected 3 calls, got %d", mock.CallCount())
	}
}

func TestRateLimitRespectsContextCancellation(t *testing.T) {
	mock := &mockFetcher{response: okResponse(newJob("https://slow.com"))}
	// Very low rate — effectively blocks immediately after burst.
	rl := middleware.NewRateLimit(0.0001, 1)
	fetcher := rl.Wrap(mock)

	// First request consumes burst token.
	job := &foxhound.Job{URL: "https://slow.com/", Domain: "slow.com"}
	_, _ = fetcher.Fetch(context.Background(), job)

	// Second request should block and context should cancel it.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := fetcher.Fetch(ctx, job)
	if err == nil {
		t.Fatal("expected error from context cancellation under tight rate limit")
	}
}

// --- Dedup tests ---

func TestDedupAllowsFirstRequest(t *testing.T) {
	mock := &mockFetcher{response: okResponse(newJob("https://example.com/page"))}
	dd := middleware.NewDedup()
	fetcher := dd.Wrap(mock)

	job := &foxhound.Job{URL: "https://example.com/page"}
	resp, err := fetcher.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestDedupBlocksDuplicateURL(t *testing.T) {
	mock := &mockFetcher{response: okResponse(newJob("https://example.com/page"))}
	dd := middleware.NewDedup()
	fetcher := dd.Wrap(mock)

	job := &foxhound.Job{URL: "https://example.com/page"}
	_, _ = fetcher.Fetch(context.Background(), job)

	resp, err := fetcher.Fetch(context.Background(), job)
	// Dedup returns a zero-status response (no error) or an error.
	// In either case the underlying fetcher must not be called again.
	_ = resp
	_ = err
	if mock.CallCount() != 1 {
		t.Errorf("expected underlying fetcher called once, got %d", mock.CallCount())
	}
}

func TestDedupTreatsCanonicalURLsAsSame(t *testing.T) {
	mock := &mockFetcher{response: okResponse(newJob("https://example.com/"))}
	dd := middleware.NewDedup()
	fetcher := dd.Wrap(mock)

	// Both URLs are the same after sorting query params.
	job1 := &foxhound.Job{URL: "https://example.com/?b=2&a=1"}
	job2 := &foxhound.Job{URL: "https://example.com/?a=1&b=2"}

	_, _ = fetcher.Fetch(context.Background(), job1)
	_, _ = fetcher.Fetch(context.Background(), job2)

	if mock.CallCount() != 1 {
		t.Errorf("canonicalized URLs should dedup: underlying called %d times", mock.CallCount())
	}
}

func TestDedupAllowsDifferentURLs(t *testing.T) {
	mock := &mockFetcher{response: okResponse(newJob("https://example.com"))}
	dd := middleware.NewDedup()
	fetcher := dd.Wrap(mock)

	for _, url := range []string{
		"https://example.com/a",
		"https://example.com/b",
		"https://example.com/c",
	} {
		_, _ = fetcher.Fetch(context.Background(), &foxhound.Job{URL: url})
	}
	if mock.CallCount() != 3 {
		t.Errorf("expected 3 distinct fetches, got %d", mock.CallCount())
	}
}

func TestDedupConcurrentSafety(t *testing.T) {
	var count int64
	fetcher := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		atomic.AddInt64(&count, 1)
		return okResponse(job), nil
	})
	dd := middleware.NewDedup()
	wrapped := dd.Wrap(fetcher)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = wrapped.Fetch(context.Background(), &foxhound.Job{URL: "https://example.com/same"})
		}()
	}
	wg.Wait()

	if atomic.LoadInt64(&count) != 1 {
		t.Errorf("concurrent dedup: underlying called %d times, expected 1", count)
	}
}

// --- DepthLimit tests ---

func TestDepthLimitAllowsWithinLimit(t *testing.T) {
	mock := &mockFetcher{response: okResponse(newJob("https://example.com"))}
	dl := middleware.NewDepthLimit(3)
	fetcher := dl.Wrap(mock)

	job := &foxhound.Job{URL: "https://example.com", Depth: 2}
	_, err := fetcher.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error at depth 2 with limit 3: %v", err)
	}
}

func TestDepthLimitBlocksExceededDepth(t *testing.T) {
	mock := &mockFetcher{response: okResponse(newJob("https://example.com"))}
	dl := middleware.NewDepthLimit(3)
	fetcher := dl.Wrap(mock)

	job := &foxhound.Job{URL: "https://example.com", Depth: 4}
	_, err := fetcher.Fetch(context.Background(), job)
	if err == nil {
		t.Fatal("expected error when depth exceeds limit")
	}
}

func TestDepthLimitAllowsAtExactLimit(t *testing.T) {
	mock := &mockFetcher{response: okResponse(newJob("https://example.com"))}
	dl := middleware.NewDepthLimit(3)
	fetcher := dl.Wrap(mock)

	job := &foxhound.Job{URL: "https://example.com", Depth: 3}
	_, err := fetcher.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("depth equal to limit should be allowed: %v", err)
	}
}

func TestDepthLimitDoesNotCallUnderlyingWhenExceeded(t *testing.T) {
	mock := &mockFetcher{response: okResponse(newJob("https://example.com"))}
	dl := middleware.NewDepthLimit(2)
	fetcher := dl.Wrap(mock)

	job := &foxhound.Job{URL: "https://example.com", Depth: 5}
	_, _ = fetcher.Fetch(context.Background(), job)

	if mock.CallCount() != 0 {
		t.Errorf("underlying fetcher should not be called when depth exceeded")
	}
}

// --- Retry tests ---

func TestRetrySucceedsOnFirstAttempt(t *testing.T) {
	mock := &mockFetcher{response: okResponse(newJob("https://example.com"))}
	r := middleware.NewRetry(3, time.Millisecond)
	fetcher := r.Wrap(mock)

	job := newJob("https://example.com")
	resp, err := fetcher.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if mock.CallCount() != 1 {
		t.Errorf("expected 1 call, got %d", mock.CallCount())
	}
}

func TestRetryRetriesOnError(t *testing.T) {
	var count int
	mock := &mockFetcher{}
	mock.err = errors.New("connection reset")
	fetcher := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		count++
		if count < 3 {
			return nil, errors.New("connection reset")
		}
		return okResponse(job), nil
	})

	r := middleware.NewRetry(3, time.Millisecond)
	wrapped := r.Wrap(fetcher)

	job := newJob("https://example.com")
	resp, err := wrapped.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if count != 3 {
		t.Errorf("expected 3 attempts, got %d", count)
	}
}

func TestRetryExhaustedReturnsLastError(t *testing.T) {
	sentinelErr := errors.New("permanent failure")
	fetcher := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		return nil, sentinelErr
	})

	r := middleware.NewRetry(2, time.Millisecond)
	wrapped := r.Wrap(fetcher)

	job := newJob("https://example.com")
	_, err := wrapped.Fetch(context.Background(), job)
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
}

func TestRetryRespectsContextCancellation(t *testing.T) {
	fetcher := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		return nil, errors.New("connection timeout")
	})

	r := middleware.NewRetry(10, 50*time.Millisecond)
	wrapped := r.Wrap(fetcher)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := wrapped.Fetch(ctx, newJob("https://example.com"))
	if err == nil {
		t.Fatal("expected error from context cancellation during retry")
	}
}

func TestRetryOnBlockedStatusCodes(t *testing.T) {
	var count int
	fetcher := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		count++
		if count < 3 {
			return &foxhound.Response{StatusCode: 429, Headers: make(http.Header)}, nil
		}
		return okResponse(job), nil
	})

	r := middleware.NewRetry(3, time.Millisecond)
	wrapped := r.Wrap(fetcher)

	resp, err := wrapped.Fetch(context.Background(), newJob("https://example.com"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 after retrying blocked response, got %d", resp.StatusCode)
	}
}

func TestRetryRetriesNetworkErrors(t *testing.T) {
	callCount := 0
	flaky := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		callCount++
		if callCount <= 2 {
			return nil, fmt.Errorf("NS_ERROR_NET_RESET")
		}
		return &foxhound.Response{StatusCode: 200, Headers: make(http.Header)}, nil
	})

	mw := middleware.NewRetry(3, 10*time.Millisecond)
	fetcher := mw.Wrap(flaky)

	resp, err := fetcher.Fetch(context.Background(), &foxhound.Job{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("Expected success after retry, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	if callCount != 3 {
		t.Fatalf("Expected 3 calls (2 failures + 1 success), got %d", callCount)
	}
}

func TestRetryDoesNotRetryNonNetworkErrors(t *testing.T) {
	callCount := 0
	fetcher := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		callCount++
		return nil, errors.New("some non-retryable application error")
	})

	mw := middleware.NewRetry(3, 10*time.Millisecond)
	wrapped := mw.Wrap(fetcher)

	_, err := wrapped.Fetch(context.Background(), &foxhound.Job{URL: "https://example.com"})
	if err == nil {
		t.Fatal("Expected error for non-retryable failure")
	}
	if callCount != 1 {
		t.Fatalf("Expected 1 call (no retries for non-network error), got %d", callCount)
	}
}
