package middleware_test

import (
	"context"
	"testing"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/middleware"
)

// TestDedup_DontFilter_BypassesDedup verifies that jobs with DontFilter=true
// are not deduplicated even when the same URL has been seen before.
func TestDedup_DontFilter_BypassesDedup(t *testing.T) {
	dedup := middleware.NewDedup()

	var fetchCount int
	stub := foxhound.FetcherFunc(func(_ context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		fetchCount++
		return &foxhound.Response{StatusCode: 200, Job: job}, nil
	})

	wrapped := dedup.Wrap(stub)
	ctx := context.Background()

	// First fetch — should go through.
	job1 := &foxhound.Job{URL: "https://example.com/page", DontFilter: false}
	resp1, err := wrapped.Fetch(ctx, job1)
	if err != nil {
		t.Fatalf("fetch 1: %v", err)
	}
	if resp1.StatusCode != 200 {
		t.Errorf("fetch 1: status = %d, want 200", resp1.StatusCode)
	}

	// Second fetch with same URL — should be deduplicated.
	job2 := &foxhound.Job{URL: "https://example.com/page", DontFilter: false}
	resp2, _ := wrapped.Fetch(ctx, job2)
	if resp2.StatusCode != 0 {
		t.Errorf("fetch 2: status = %d, want 0 (deduplicated)", resp2.StatusCode)
	}

	// Third fetch with DontFilter=true — should bypass dedup.
	job3 := &foxhound.Job{URL: "https://example.com/page", DontFilter: true}
	resp3, err := wrapped.Fetch(ctx, job3)
	if err != nil {
		t.Fatalf("fetch 3: %v", err)
	}
	if resp3.StatusCode != 200 {
		t.Errorf("fetch 3: status = %d, want 200 (DontFilter bypasses dedup)", resp3.StatusCode)
	}

	// Should have 2 real fetches (fetch 1 and fetch 3), fetch 2 was deduped.
	if fetchCount != 2 {
		t.Errorf("fetchCount = %d, want 2", fetchCount)
	}
}

// TestDedup_DontFilter_MultipleFetches verifies DontFilter allows repeated
// fetches of the same URL.
func TestDedup_DontFilter_MultipleFetches(t *testing.T) {
	dedup := middleware.NewDedup()

	var fetchCount int
	stub := foxhound.FetcherFunc(func(_ context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		fetchCount++
		return &foxhound.Response{StatusCode: 200, Job: job}, nil
	})

	wrapped := dedup.Wrap(stub)
	ctx := context.Background()

	// Fetch the same URL 5 times with DontFilter=true.
	for i := 0; i < 5; i++ {
		job := &foxhound.Job{URL: "https://example.com/monitor", DontFilter: true}
		resp, err := wrapped.Fetch(ctx, job)
		if err != nil {
			t.Fatalf("fetch %d: %v", i, err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("fetch %d: status = %d, want 200", i, resp.StatusCode)
		}
	}

	if fetchCount != 5 {
		t.Errorf("fetchCount = %d, want 5 (all should bypass dedup)", fetchCount)
	}
}
