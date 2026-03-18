//go:build !playwright

// camoufox_behavior_test.go — unit tests for the session-persistence option
// and the WithPersistSession constructor option added to CamoufoxFetcher.
//
// These tests run WITHOUT the playwright build tag. They test only the
// exported option constructors and struct-level fields — no browser is
// launched.
//
// RED phase: these fail until WithPersistSession is added to camoufox_stub.go
// (or camoufox_playwright.go under the playwright tag).

package fetch_test

import (
	"testing"

	"github.com/sadewadee/foxhound/fetch"
)

// TestWithPersistSession_OptionExists verifies that the WithPersistSession
// functional option can be constructed without a compile error.
// The actual session-reuse behaviour is tested under the playwright tag.
func TestWithPersistSession_OptionExists(t *testing.T) {
	opt := fetch.WithPersistSession(true)
	if opt == nil {
		t.Error("WithPersistSession(true) must not return nil")
	}

	optFalse := fetch.WithPersistSession(false)
	if optFalse == nil {
		t.Error("WithPersistSession(false) must not return nil")
	}
}
