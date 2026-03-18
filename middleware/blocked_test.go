package middleware_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/middleware"
)

// blockResp builds a Response for block-detection tests.
func blockResp(statusCode int, body string) *foxhound.Response {
	return &foxhound.Response{
		StatusCode: statusCode,
		Headers:    make(http.Header),
		Body:       []byte(body),
		URL:        "https://example.com/page",
	}
}

// sequenceFetcher returns responses from a slice in order, cycling on the last.
type sequenceFetcher struct {
	resps []*foxhound.Response
	idx   int
}

func (s *sequenceFetcher) Fetch(_ context.Context, _ *foxhound.Job) (*foxhound.Response, error) {
	r := s.resps[s.idx]
	if s.idx < len(s.resps)-1 {
		s.idx++
	}
	return r, nil
}
func (s *sequenceFetcher) Close() error { return nil }

// ---------------------------------------------------------------------------
// TestBlockDetector_CloudflarePattern
// ---------------------------------------------------------------------------

func TestBlockDetector_CloudflarePattern(t *testing.T) {
	body := `<html><body>Checking your browser before accessing example.com. challenge-platform</body></html>`
	inner := &mockFetcher{response: blockResp(200, body)}
	bd := middleware.NewBlockDetector(0, time.Millisecond) // 0 retries = detect only
	fetcher := bd.Wrap(inner)

	// Even with 0 retries the response is returned (no retry means we still
	// return the last blocked response).
	resp, err := fetcher.Fetch(context.Background(), newJob("https://example.com/"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The response must come through — block detection does not swallow it.
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

// ---------------------------------------------------------------------------
// TestBlockDetector_RateLimitPattern
// ---------------------------------------------------------------------------

func TestBlockDetector_RateLimitPattern(t *testing.T) {
	tests := []struct {
		name string
		resp *foxhound.Response
	}{
		{
			name: "status 429",
			resp: blockResp(429, `<html><body>Too Many Requests</body></html>`),
		},
		{
			name: "body contains rate limit",
			resp: blockResp(200, `{"error":"rate limit exceeded","retry_after":60}`),
		},
		{
			name: "body contains too many requests",
			resp: blockResp(200, `<html><body>Too many requests, please slow down.</body></html>`),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inner := &mockFetcher{response: tc.resp}
			// 1 retry so we can observe the retry behaviour without long delay.
			bd := middleware.NewBlockDetector(1, time.Millisecond)
			fetcher := bd.Wrap(inner)

			resp, err := fetcher.Fetch(context.Background(), newJob("https://example.com/"))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("expected non-nil response")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestBlockDetector_EmptyTrap
// ---------------------------------------------------------------------------

func TestBlockDetector_EmptyTrap(t *testing.T) {
	// 200 OK but body is tiny and has no <html marker.
	body := `{"status":"ok"}`
	inner := &mockFetcher{response: blockResp(200, body)}
	bd := middleware.NewBlockDetector(1, time.Millisecond)
	fetcher := bd.Wrap(inner)

	resp, err := fetcher.Fetch(context.Background(), newJob("https://example.com/"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	// The inner fetcher should have been called twice (initial + 1 retry).
	if inner.CallCount() != 2 {
		t.Errorf("expected 2 calls for empty-trap with 1 retry, got %d", inner.CallCount())
	}
}

// ---------------------------------------------------------------------------
// TestBlockDetector_NormalResponsePassesThrough
// ---------------------------------------------------------------------------

func TestBlockDetector_NormalResponsePassesThrough(t *testing.T) {
	body := `<html><head><title>Normal Page</title></head><body><p>Hello, world!</p></body></html>`
	inner := &mockFetcher{response: blockResp(200, body)}
	bd := middleware.NewBlockDetector(3, time.Millisecond)
	fetcher := bd.Wrap(inner)

	resp, err := fetcher.Fetch(context.Background(), newJob("https://example.com/"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	// Normal page: no retries, exactly 1 call.
	if inner.CallCount() != 1 {
		t.Errorf("normal response should not cause retries: got %d calls", inner.CallCount())
	}
}

// ---------------------------------------------------------------------------
// TestBlockDetector_RetryOnBlock
// ---------------------------------------------------------------------------

func TestBlockDetector_RetryOnBlock(t *testing.T) {
	blockedBody := `<html><body>Just a moment... cloudflare</body></html>`
	normalBody := `<html><head><title>OK</title></head><body>All good</body></html>`

	seq := &sequenceFetcher{
		resps: []*foxhound.Response{
			blockResp(200, blockedBody), // first call: blocked
			blockResp(200, normalBody),  // second call: success
		},
	}

	bd := middleware.NewBlockDetector(2, time.Millisecond)
	fetcher := bd.Wrap(seq)

	resp, err := fetcher.Fetch(context.Background(), newJob("https://example.com/"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	// seq was called twice: once blocked, once normal.
	if seq.idx != 1 {
		t.Errorf("expected 2 fetcher calls (1 blocked + 1 success), got idx=%d", seq.idx)
	}
}

// ---------------------------------------------------------------------------
// TestBlockDetector_CustomPattern
// ---------------------------------------------------------------------------

func TestBlockDetector_CustomPattern(t *testing.T) {
	custom := middleware.BlockPattern{
		Name:         "custom-bot-wall",
		BodyContains: []string{"please prove you are human"},
	}

	blockedBody := `<html><body>Please prove you are human.</body></html>`
	inner := &mockFetcher{response: blockResp(200, blockedBody)}
	bd := middleware.NewBlockDetector(1, time.Millisecond, custom)
	fetcher := bd.Wrap(inner)

	resp, err := fetcher.Fetch(context.Background(), newJob("https://example.com/"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	// Custom pattern triggered 1 retry → 2 total calls.
	if inner.CallCount() != 2 {
		t.Errorf("expected 2 calls for custom pattern with 1 retry, got %d", inner.CallCount())
	}
}

// ---------------------------------------------------------------------------
// TestBlockDetector_ContextCancelledDuringBackoff
// ---------------------------------------------------------------------------

func TestBlockDetector_ContextCancelledDuringBackoff(t *testing.T) {
	blockedBody := `<html><body>Access Denied — you are blocked.</body></html>`
	inner := &mockFetcher{response: blockResp(403, blockedBody)}

	// Long base delay so the context will cancel before the retry fires.
	bd := middleware.NewBlockDetector(3, 500*time.Millisecond)
	fetcher := bd.Wrap(inner)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := fetcher.Fetch(ctx, newJob("https://example.com/"))
	if err == nil {
		t.Fatal("expected context cancellation error during backoff")
	}
}

// ---------------------------------------------------------------------------
// TestDefaultBlockPatterns_CoverageCheck
// ---------------------------------------------------------------------------

func TestDefaultBlockPatterns_CoverageCheck(t *testing.T) {
	patterns := middleware.DefaultBlockPatterns()
	if len(patterns) == 0 {
		t.Fatal("expected at least one default block pattern")
	}

	names := make(map[string]bool)
	for _, p := range patterns {
		if p.Name == "" {
			t.Errorf("block pattern has empty Name: %+v", p)
		}
		names[p.Name] = true
	}

	// Ensure key vendors are covered.
	for _, want := range []string{"cloudflare", "rate-limit", "access-denied", "bot-detection"} {
		if !names[want] {
			t.Errorf("default patterns missing expected name %q, got: %v", want, names)
		}
	}
}
