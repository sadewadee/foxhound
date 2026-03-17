package middleware_test

// Extra tests for: autothrottle, cookies, referer, redirect, deltafetch.
// The mockFetcher, okResponse, and newJob helpers are defined in middleware_test.go.

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/middleware"
)

// --- AutoThrottle tests ---

func TestAutoThrottlePassesRequestThrough(t *testing.T) {
	mock := &mockFetcher{response: okResponse(newJob("https://example.com/"))}
	at := middleware.NewAutoThrottle(middleware.AutoThrottleConfig{
		TargetConcurrency: 1,
		InitialDelay:      0,
		MinDelay:          0,
		MaxDelay:          time.Second,
	})
	fetcher := at.Wrap(mock)

	job := newJob("https://example.com/")
	resp, err := fetcher.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAutoThrottleSpikeDelayOn429(t *testing.T) {
	// After a 429, subsequent same-domain requests must observe a delay
	// (delay must have moved toward MaxDelay, so it should be > 0).
	callTimes := make([]time.Time, 0, 3)
	var mu sync.Mutex
	var callCount int

	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		mu.Lock()
		callTimes = append(callTimes, time.Now())
		n := callCount
		callCount++
		mu.Unlock()

		if n == 0 {
			return &foxhound.Response{StatusCode: 429, Headers: make(http.Header), Job: job,
				Duration: 50 * time.Millisecond}, nil
		}
		return okResponse(job), nil
	})

	at := middleware.NewAutoThrottle(middleware.AutoThrottleConfig{
		TargetConcurrency: 1,
		InitialDelay:      0,
		MinDelay:          0,
		MaxDelay:          200 * time.Millisecond,
	})
	fetcher := at.Wrap(inner)

	job := newJob("https://throttle.com/")
	job.Domain = "throttle.com"

	_, _ = fetcher.Fetch(context.Background(), job)
	t1 := time.Now()
	_, _ = fetcher.Fetch(context.Background(), job)
	elapsed := time.Since(t1)

	// After a 429 the delay must be > 0; we expect at least 10 ms.
	if elapsed < 10*time.Millisecond {
		t.Errorf("expected throttle delay after 429, elapsed=%v", elapsed)
	}
}

func TestAutoThrottlePerDomainState(t *testing.T) {
	// Throttle for domainA should not affect domainB.
	var countA, countB int
	var mu sync.Mutex

	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		mu.Lock()
		if job.Domain == "a.com" {
			countA++
		} else {
			countB++
		}
		mu.Unlock()
		return okResponse(job), nil
	})

	at := middleware.NewAutoThrottle(middleware.AutoThrottleConfig{
		TargetConcurrency: 1,
		InitialDelay:      0,
		MinDelay:          0,
		MaxDelay:          time.Second,
	})
	fetcher := at.Wrap(inner)

	jobA := &foxhound.Job{URL: "https://a.com/", Domain: "a.com"}
	jobB := &foxhound.Job{URL: "https://b.com/", Domain: "b.com"}

	_, _ = fetcher.Fetch(context.Background(), jobA)
	_, _ = fetcher.Fetch(context.Background(), jobB)

	mu.Lock()
	defer mu.Unlock()
	if countA != 1 || countB != 1 {
		t.Errorf("expected 1 call each; got countA=%d countB=%d", countA, countB)
	}
}

// --- Cookies tests ---

func TestCookiesPersistsAcrossRequests(t *testing.T) {
	// The inner fetcher sets a cookie on the first response.
	// On the second request, we verify the cookie is present in the request headers.
	var secondRequestCookies string
	var callCount int

	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		callCount++
		if callCount == 1 {
			resp := &foxhound.Response{
				StatusCode: 200,
				Job:        job,
				URL:        job.URL,
				Headers:    http.Header{},
			}
			resp.Headers.Set("Set-Cookie", "session=abc123; Path=/")
			return resp, nil
		}
		// Second call: capture cookies sent.
		secondRequestCookies = job.Headers.Get("Cookie")
		return okResponse(job), nil
	})

	cj := middleware.NewCookies()
	fetcher := cj.Wrap(inner)

	job1 := &foxhound.Job{URL: "https://cookies.com/login", Domain: "cookies.com", Headers: make(http.Header)}
	_, err := fetcher.Fetch(context.Background(), job1)
	if err != nil {
		t.Fatalf("first fetch failed: %v", err)
	}

	job2 := &foxhound.Job{URL: "https://cookies.com/profile", Domain: "cookies.com", Headers: make(http.Header)}
	_, err = fetcher.Fetch(context.Background(), job2)
	if err != nil {
		t.Fatalf("second fetch failed: %v", err)
	}

	if secondRequestCookies == "" {
		t.Error("expected Cookie header on second request, got empty string")
	}
	if secondRequestCookies != "session=abc123" {
		t.Errorf("expected cookie 'session=abc123', got %q", secondRequestCookies)
	}
}

func TestCookiesDoesNotShareBetweenDomains(t *testing.T) {
	// Cookie set by siteA.com must not be sent to siteB.com.
	var siteBCookies string
	var callCount int

	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		callCount++
		if callCount == 1 {
			resp := &foxhound.Response{
				StatusCode: 200,
				Job:        job,
				URL:        job.URL,
				Headers:    http.Header{},
			}
			resp.Headers.Set("Set-Cookie", "secret=yes; Path=/")
			return resp, nil
		}
		siteBCookies = job.Headers.Get("Cookie")
		return okResponse(job), nil
	})

	cj := middleware.NewCookies()
	fetcher := cj.Wrap(inner)

	jobA := &foxhound.Job{URL: "https://siteA.com/", Domain: "siteA.com", Headers: make(http.Header)}
	_, _ = fetcher.Fetch(context.Background(), jobA)

	jobB := &foxhound.Job{URL: "https://siteB.com/", Domain: "siteB.com", Headers: make(http.Header)}
	_, _ = fetcher.Fetch(context.Background(), jobB)

	if siteBCookies != "" {
		t.Errorf("siteB should not receive siteA cookies, got %q", siteBCookies)
	}
}

func TestCookiesInitialisesHeadersIfNil(t *testing.T) {
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		// Must not panic if job.Headers was nil.
		return okResponse(job), nil
	})

	cj := middleware.NewCookies()
	fetcher := cj.Wrap(inner)

	job := &foxhound.Job{URL: "https://example.com/", Domain: "example.com"} // Headers is nil
	_, err := fetcher.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error with nil headers: %v", err)
	}
}

// --- Referer tests ---

func TestRefererFirstRequestUsesGoogleReferer(t *testing.T) {
	var gotReferer string
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		gotReferer = job.Headers.Get("Referer")
		return okResponse(job), nil
	})

	ref := middleware.NewReferer()
	fetcher := ref.Wrap(inner)

	job := &foxhound.Job{URL: "https://target.com/page", Domain: "target.com", Headers: make(http.Header)}
	_, err := fetcher.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First request to a domain should have a google search referer.
	if gotReferer == "" {
		t.Fatal("expected Referer header on first request")
	}
	if gotReferer != "https://www.google.com/search?q=target.com" {
		t.Errorf("expected google referer, got %q", gotReferer)
	}
}

func TestRefererSubsequentRequestUsesPreviousURL(t *testing.T) {
	var secondReferer string
	callCount := 0
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		callCount++
		if callCount == 2 {
			secondReferer = job.Headers.Get("Referer")
		}
		resp := okResponse(job)
		resp.URL = job.URL
		return resp, nil
	})

	ref := middleware.NewReferer()
	fetcher := ref.Wrap(inner)

	job1 := &foxhound.Job{URL: "https://target.com/page1", Domain: "target.com", Headers: make(http.Header)}
	_, _ = fetcher.Fetch(context.Background(), job1)

	job2 := &foxhound.Job{URL: "https://target.com/page2", Domain: "target.com", Headers: make(http.Header)}
	_, err := fetcher.Fetch(context.Background(), job2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if secondReferer != "https://target.com/page1" {
		t.Errorf("expected referer 'https://target.com/page1', got %q", secondReferer)
	}
}

func TestRefererDoesNotOverrideExistingReferer(t *testing.T) {
	var gotReferer string
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		gotReferer = job.Headers.Get("Referer")
		return okResponse(job), nil
	})

	ref := middleware.NewReferer()
	fetcher := ref.Wrap(inner)

	job := &foxhound.Job{
		URL:     "https://target.com/",
		Domain:  "target.com",
		Headers: http.Header{"Referer": []string{"https://manual.com/"}},
	}
	_, _ = fetcher.Fetch(context.Background(), job)

	if gotReferer != "https://manual.com/" {
		t.Errorf("expected pre-set referer to be preserved, got %q", gotReferer)
	}
}

// --- Redirect tests ---

func TestRedirectFollowsRedirect(t *testing.T) {
	var callCount int
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		callCount++
		if callCount == 1 {
			return &foxhound.Response{
				StatusCode: 301,
				Headers:    http.Header{"Location": []string{"https://example.com/final"}},
				Job:        job,
			}, nil
		}
		return &foxhound.Response{StatusCode: 200, Headers: make(http.Header), Job: job, URL: job.URL}, nil
	})

	rd := middleware.NewRedirect(5)
	fetcher := rd.Wrap(inner)

	job := &foxhound.Job{URL: "https://example.com/start", Domain: "example.com", Headers: make(http.Header)}
	resp, err := fetcher.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 after redirect, got %d", resp.StatusCode)
	}
	if callCount != 2 {
		t.Errorf("expected 2 fetches (original + redirect), got %d", callCount)
	}
}

func TestRedirectRespectsMaxLimit(t *testing.T) {
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		return &foxhound.Response{
			StatusCode: 302,
			Headers:    http.Header{"Location": []string{"https://example.com/loop"}},
			Job:        job,
		}, nil
	})

	rd := middleware.NewRedirect(3)
	fetcher := rd.Wrap(inner)

	job := &foxhound.Job{URL: "https://example.com/loop", Domain: "example.com", Headers: make(http.Header)}
	_, err := fetcher.Fetch(context.Background(), job)
	if err == nil {
		t.Fatal("expected error when max redirects exceeded")
	}
}

func TestRedirectAllStatusCodes(t *testing.T) {
	redirectCodes := []int{301, 302, 303, 307, 308}
	for _, code := range redirectCodes {
		t.Run(fmt.Sprintf("status_%d", code), func(t *testing.T) {
			var calls int
			inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
				calls++
				if calls == 1 {
					return &foxhound.Response{
						StatusCode: code,
						Headers:    http.Header{"Location": []string{"https://example.com/dest"}},
						Job:        job,
					}, nil
				}
				return okResponse(job), nil
			})

			rd := middleware.NewRedirect(5)
			fetcher := rd.Wrap(inner)

			job := &foxhound.Job{URL: "https://example.com/src", Domain: "example.com", Headers: make(http.Header)}
			resp, err := fetcher.Fetch(context.Background(), job)
			if err != nil {
				t.Fatalf("status %d: unexpected error: %v", code, err)
			}
			if resp.StatusCode != 200 {
				t.Errorf("status %d: expected 200 after redirect, got %d", code, resp.StatusCode)
			}
		})
	}
}

func TestRedirectNoRedirectPassesThrough(t *testing.T) {
	mock := &mockFetcher{response: okResponse(newJob("https://example.com/"))}
	rd := middleware.NewRedirect(5)
	fetcher := rd.Wrap(mock)

	job := &foxhound.Job{URL: "https://example.com/", Domain: "example.com", Headers: make(http.Header)}
	resp, err := fetcher.Fetch(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if mock.CallCount() != 1 {
		t.Errorf("expected exactly 1 call, got %d", mock.CallCount())
	}
}

// --- DeltaFetch tests ---

func TestDeltaFetchSkipsSeenURL(t *testing.T) {
	store := middleware.NewMemoryDeltaStore()
	df := middleware.NewDeltaFetch(middleware.DeltaSkipSeen, store, 0)

	var count int
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		count++
		return okResponse(job), nil
	})

	fetcher := df.Wrap(inner)
	job := newJob("https://example.com/page")

	// First fetch: should proceed.
	_, _ = fetcher.Fetch(context.Background(), job)
	if count != 1 {
		t.Fatalf("expected 1 fetch, got %d", count)
	}

	// Second fetch: same URL should be skipped.
	_, _ = fetcher.Fetch(context.Background(), job)
	if count != 1 {
		t.Errorf("expected URL to be skipped on second fetch, count=%d", count)
	}
}

func TestDeltaFetchAllowsNewURLs(t *testing.T) {
	store := middleware.NewMemoryDeltaStore()
	df := middleware.NewDeltaFetch(middleware.DeltaSkipSeen, store, 0)

	var count int
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		count++
		return okResponse(job), nil
	})

	fetcher := df.Wrap(inner)

	for i := 0; i < 3; i++ {
		job := &foxhound.Job{URL: fmt.Sprintf("https://example.com/page%d", i)}
		_, _ = fetcher.Fetch(context.Background(), job)
	}

	if count != 3 {
		t.Errorf("expected 3 unique fetches, got %d", count)
	}
}

func TestDeltaFetchSkipRecentWithinTTL(t *testing.T) {
	store := middleware.NewMemoryDeltaStore()
	ttl := time.Hour
	df := middleware.NewDeltaFetch(middleware.DeltaSkipRecent, store, ttl)

	var count int
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		count++
		return okResponse(job), nil
	})

	fetcher := df.Wrap(inner)
	job := newJob("https://example.com/article")

	_, _ = fetcher.Fetch(context.Background(), job) // marks as seen
	_, _ = fetcher.Fetch(context.Background(), job) // within TTL → skip

	if count != 1 {
		t.Errorf("expected skip within TTL, count=%d", count)
	}
}

func TestDeltaFetchRefetchesAfterTTLExpiry(t *testing.T) {
	store := middleware.NewMemoryDeltaStore()
	ttl := 20 * time.Millisecond
	df := middleware.NewDeltaFetch(middleware.DeltaSkipRecent, store, ttl)

	var count int
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		count++
		return okResponse(job), nil
	})

	fetcher := df.Wrap(inner)
	job := newJob("https://example.com/news")

	_, _ = fetcher.Fetch(context.Background(), job)

	time.Sleep(40 * time.Millisecond)

	_, _ = fetcher.Fetch(context.Background(), job)

	if count != 2 {
		t.Errorf("expected refetch after TTL expiry, count=%d", count)
	}
}

func TestDeltaFetchMemoryStoreSeenReturnsFalseForNew(t *testing.T) {
	store := middleware.NewMemoryDeltaStore()
	seen, _ := store.Seen("new-key")
	if seen {
		t.Fatal("expected unseen for fresh store")
	}
}

func TestDeltaFetchMemoryStoreMarkAndSeen(t *testing.T) {
	store := middleware.NewMemoryDeltaStore()
	if err := store.Mark("key1"); err != nil {
		t.Fatalf("Mark failed: %v", err)
	}
	seen, ts := store.Seen("key1")
	if !seen {
		t.Fatal("expected seen after Mark")
	}
	if ts.IsZero() {
		t.Fatal("expected non-zero timestamp after Mark")
	}
}
