//go:build !playwright

// camoufox_lifecycle_test.go — tests for WithMaxBrowserRequests option.
// These tests run against the stub build (no playwright tag) and verify the
// option is accepted without error. The full restart behaviour is validated
// by the playwright-tagged tests in camoufox_playwright_lifecycle_test.go.

package fetch_test

import (
	"testing"

	"github.com/foxhound-scraper/foxhound/fetch"
)

func TestWithMaxBrowserRequestsOptionAccepted(t *testing.T) {
	f, err := fetch.NewCamoufox(fetch.WithMaxBrowserRequests(200))
	if err != nil {
		t.Fatalf("NewCamoufox(WithMaxBrowserRequests(200)) error: %v", err)
	}
	if f == nil {
		t.Fatal("NewCamoufox returned nil fetcher")
	}
	_ = f.Close()
}

func TestWithMaxBrowserRequestsZeroAccepted(t *testing.T) {
	// n=0 means disabled (no automatic restart).
	f, err := fetch.NewCamoufox(fetch.WithMaxBrowserRequests(0))
	if err != nil {
		t.Fatalf("NewCamoufox(WithMaxBrowserRequests(0)) error: %v", err)
	}
	if f == nil {
		t.Fatal("NewCamoufox returned nil fetcher")
	}
	_ = f.Close()
}

func TestWithMaxBrowserRequestsDefaultIsPositive(t *testing.T) {
	// A fetcher created without the option should also not fail — the default
	// is applied internally (300). We just verify construction succeeds.
	f, err := fetch.NewCamoufox()
	if err != nil {
		t.Fatalf("NewCamoufox() error: %v", err)
	}
	if f == nil {
		t.Fatal("NewCamoufox returned nil fetcher")
	}
	_ = f.Close()
}
