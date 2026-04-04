package fetch

import (
	"context"
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

// smartMockFetcher is a test double for foxhound.Fetcher used by SmartFetcher tests.
type smartMockFetcher struct {
	fetchFn func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error)
}

func (m *smartMockFetcher) Fetch(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
	return m.fetchFn(ctx, job)
}

func (m *smartMockFetcher) Close() error { return nil }

func TestSmartFetcher_CautiousTimeoutDoesNotKillBrowserEscalation(t *testing.T) {
	staticCalled := false
	browserCalled := false

	static := &smartMockFetcher{fetchFn: func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		staticCalled = true
		// Simulate: static returns a blocked response
		return &foxhound.Response{StatusCode: 403, Body: []byte("blocked")}, nil
	}}

	browser := &smartMockFetcher{fetchFn: func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		browserCalled = true
		// Verify context is NOT expired
		select {
		case <-ctx.Done():
			t.Fatal("browser received expired context")
		default:
		}
		return &foxhound.Response{StatusCode: 200, Body: []byte("ok")}, nil
	}}

	scorer := NewDomainScorer(DefaultDomainScoreConfig())
	// Push domain into cautious zone
	scorer.RecordStatic("test.com", true)
	scorer.RecordStatic("test.com", true)

	f := NewSmart(static, browser,
		WithDomainScorer(scorer),
		WithCautiousTimeout(100*time.Millisecond),
		WithBlockDetector(&DefaultBlockDetector{}),
	)

	job := &foxhound.Job{URL: "https://test.com/page", Domain: "test.com"}
	resp, err := f.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !staticCalled || !browserCalled {
		t.Fatalf("static=%v browser=%v, both should be true", staticCalled, browserCalled)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 from browser, got %d", resp.StatusCode)
	}
}

func TestSmartFetcher_StaticErrorEscalatesToBrowser(t *testing.T) {
	browser := &smartMockFetcher{fetchFn: func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		return &foxhound.Response{StatusCode: 200, Body: []byte("ok")}, nil
	}}

	static := &smartMockFetcher{fetchFn: func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		return nil, context.DeadlineExceeded
	}}

	f := NewSmart(static, browser, WithBlockDetector(&DefaultBlockDetector{}))

	job := &foxhound.Job{URL: "https://test.com/page"}
	resp, err := f.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("expected browser fallback, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
