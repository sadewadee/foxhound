//go:build !tls

package fetch_test

import (
	"testing"

	"github.com/sadewadee/foxhound/fetch"
)

// TestStealthFetcher_IsImpersonating_DefaultBuild pins the contract: the
// default build returns false so consumer fail-fast checks like
//
//	if !f.IsImpersonating() { log.Fatal(...) }
//
// behave correctly. If this test ever fails on the default build, somebody
// has accidentally swapped the build-tag polarity.
func TestStealthFetcher_IsImpersonating_DefaultBuild(t *testing.T) {
	f := fetch.NewStealth()
	defer f.Close()
	if f.IsImpersonating() {
		t.Fatal("default build must return IsImpersonating() == false; got true (build tag mixup?)")
	}
}
