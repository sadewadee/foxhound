package middleware_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	foxhound "github.com/foxhound-scraper/foxhound"
	"github.com/foxhound-scraper/foxhound/middleware"
)

// ---------------------------------------------------------------------------
// robots.txt parser tests (via middleware behaviour)
// ---------------------------------------------------------------------------

func robotsJob(url string) *foxhound.Job {
	return &foxhound.Job{URL: url}
}

// newRobotsServer starts a test HTTP server that serves robots.txt with the
// supplied content at /robots.txt, and a 200 OK for all other paths.
func newRobotsServer(robotsBody string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, robotsBody)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
}

func TestRobotsTxt_AllowedPath_PassesThrough(t *testing.T) {
	srv := newRobotsServer("User-agent: *\nDisallow: /private/\n")
	defer srv.Close()

	var called bool
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		called = true
		return &foxhound.Response{StatusCode: 200, Job: job}, nil
	})

	mw := middleware.NewRobotsTxt("Foxhound")
	fetcher := mw.Wrap(inner)

	_, err := fetcher.Fetch(context.Background(), robotsJob(srv.URL+"/public/page"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected inner fetcher to be called for an allowed path")
	}
}

func TestRobotsTxt_DisallowedPath_SkipsInner(t *testing.T) {
	srv := newRobotsServer("User-agent: *\nDisallow: /private/\n")
	defer srv.Close()

	var called bool
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		called = true
		return &foxhound.Response{StatusCode: 200, Job: job}, nil
	})

	mw := middleware.NewRobotsTxt("Foxhound")
	fetcher := mw.Wrap(inner)

	resp, err := fetcher.Fetch(context.Background(), robotsJob(srv.URL+"/private/secret"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("inner fetcher must not be called for a disallowed path")
	}
	if resp.StatusCode != 0 {
		t.Errorf("disallowed URL should return StatusCode 0, got %d", resp.StatusCode)
	}
}

func TestRobotsTxt_DisallowedRoot_SkipsInner(t *testing.T) {
	// Disallowing "/" blocks everything.
	srv := newRobotsServer("User-agent: *\nDisallow: /\n")
	defer srv.Close()

	var called bool
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		called = true
		return &foxhound.Response{StatusCode: 200, Job: job}, nil
	})

	mw := middleware.NewRobotsTxt("Foxhound")
	fetcher := mw.Wrap(inner)

	resp, _ := fetcher.Fetch(context.Background(), robotsJob(srv.URL+"/any/page"))
	if called {
		t.Error("inner fetcher must not be called when / is disallowed")
	}
	if resp.StatusCode != 0 {
		t.Errorf("expected StatusCode 0 for disallowed root, got %d", resp.StatusCode)
	}
}

func TestRobotsTxt_EmptyDisallow_AllowsAll(t *testing.T) {
	// An empty Disallow line means allow all.
	srv := newRobotsServer("User-agent: *\nDisallow:\n")
	defer srv.Close()

	var called bool
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		called = true
		return &foxhound.Response{StatusCode: 200, Job: job}, nil
	})

	mw := middleware.NewRobotsTxt("Foxhound")
	fetcher := mw.Wrap(inner)

	_, _ = fetcher.Fetch(context.Background(), robotsJob(srv.URL+"/any/path"))
	if !called {
		t.Error("expected inner fetcher to be called when Disallow is empty")
	}
}

func TestRobotsTxt_FetchFailure_AllowsAll(t *testing.T) {
	// Server returns 500 for robots.txt — middleware should allow all requests.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var called bool
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		called = true
		return &foxhound.Response{StatusCode: 200, Job: job}, nil
	})

	mw := middleware.NewRobotsTxt("Foxhound")
	fetcher := mw.Wrap(inner)

	_, _ = fetcher.Fetch(context.Background(), robotsJob(srv.URL+"/page"))
	if !called {
		t.Error("expected inner fetcher to be called when robots.txt fetch fails")
	}
}

func TestRobotsTxt_RulesCachedPerDomain(t *testing.T) {
	robotsFetchCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			robotsFetchCount++
			fmt.Fprint(w, "User-agent: *\nDisallow: /private/\n")
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		return &foxhound.Response{StatusCode: 200, Job: job}, nil
	})

	mw := middleware.NewRobotsTxt("Foxhound")
	fetcher := mw.Wrap(inner)
	ctx := context.Background()

	// Three requests to the same domain.
	for _, path := range []string{"/page1", "/page2", "/page3"} {
		_, _ = fetcher.Fetch(ctx, robotsJob(srv.URL+path))
	}

	if robotsFetchCount != 1 {
		t.Errorf("robots.txt should be fetched once per domain, got %d fetches", robotsFetchCount)
	}
}

func TestRobotsTxt_MultipleDisallowRules(t *testing.T) {
	srv := newRobotsServer("User-agent: *\nDisallow: /admin/\nDisallow: /api/internal/\n")
	defer srv.Close()

	mw := middleware.NewRobotsTxt("Foxhound")
	ctx := context.Background()

	cases := []struct {
		path    string
		allowed bool
	}{
		{"/public/page", true},
		{"/admin/dashboard", false},
		{"/api/internal/data", false},
		{"/api/external/data", true},
	}

	for _, tc := range cases {
		var innerCalled bool
		trackingInner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
			innerCalled = true
			return &foxhound.Response{StatusCode: 200, Job: job}, nil
		})
		trackingFetcher := mw.Wrap(trackingInner)
		_, _ = trackingFetcher.Fetch(ctx, robotsJob(srv.URL+tc.path))
		if innerCalled != tc.allowed {
			t.Errorf("path %q: expected allowed=%v, got called=%v", tc.path, tc.allowed, innerCalled)
		}
	}
}

func TestRobotsTxt_NoRobotsFile_AllowsAll(t *testing.T) {
	// Server returns 404 for robots.txt — should allow all.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var called bool
	inner := foxhound.FetcherFunc(func(ctx context.Context, job *foxhound.Job) (*foxhound.Response, error) {
		called = true
		return &foxhound.Response{StatusCode: 200, Job: job}, nil
	})

	mw := middleware.NewRobotsTxt("Foxhound")
	fetcher := mw.Wrap(inner)
	_, _ = fetcher.Fetch(context.Background(), robotsJob(srv.URL+"/page"))
	if !called {
		t.Error("expected inner fetcher to be called when robots.txt returns 404")
	}
}
