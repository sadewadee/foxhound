package foxhound

import (
	"context"
	"net/http"
	"testing"
)

type sessionStubFetcher struct {
	lastJob   *Job
	returning *Response
}

func (f *sessionStubFetcher) Fetch(_ context.Context, job *Job) (*Response, error) {
	f.lastJob = job
	return f.returning, nil
}
func (f *sessionStubFetcher) Close() error { return nil }

func TestSession_GetUsesFetcher(t *testing.T) {
	stub := &sessionStubFetcher{returning: &Response{StatusCode: 200, URL: "https://example.com"}}
	s := NewSession(WithSessionFetcher(stub))
	defer s.Close()

	resp, err := s.Get(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if stub.lastJob == nil || stub.lastJob.URL != "https://example.com" {
		t.Fatalf("fetcher not called with expected job")
	}
}

func TestSession_NoFetcherErrors(t *testing.T) {
	s := NewSession()
	if _, err := s.Get(context.Background(), "https://example.com"); err == nil {
		t.Fatal("expected error when no fetcher configured")
	}
}

func TestSession_CookiePersistence(t *testing.T) {
	stub := &sessionStubFetcher{
		returning: &Response{
			StatusCode: 200,
			URL:        "https://example.com",
			Cookies:    []*http.Cookie{{Name: "sid", Value: "abc123"}},
		},
	}
	s := NewSession(WithSessionFetcher(stub))
	defer s.Close()

	if _, err := s.Get(context.Background(), "https://example.com"); err != nil {
		t.Fatalf("first get: %v", err)
	}

	cookies := s.CookiesFor("https://example.com")
	if len(cookies) != 1 || cookies[0].Name != "sid" || cookies[0].Value != "abc123" {
		t.Fatalf("cookie not persisted: %+v", cookies)
	}

	// Second call should inject the cookie into Job.Headers.
	if _, err := s.Get(context.Background(), "https://example.com/next"); err != nil {
		t.Fatalf("second get: %v", err)
	}
	if stub.lastJob.Headers.Get("Cookie") == "" {
		t.Fatal("expected Cookie header injected on second call")
	}
}

func TestSession_Options(t *testing.T) {
	s := NewSession(
		WithSessionProxy("http://proxy.example:8080"),
		WithSessionIdentity("identity-token"),
	)
	if s.ProxyURL() != "http://proxy.example:8080" {
		t.Fatalf("proxy url: %s", s.ProxyURL())
	}
	if s.Identity() != "identity-token" {
		t.Fatalf("identity: %v", s.Identity())
	}
	s.SetName("idx")
	if s.Name() != "idx" {
		t.Fatalf("name: %s", s.Name())
	}
}
