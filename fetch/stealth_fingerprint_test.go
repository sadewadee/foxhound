package fetch_test

import (
	"testing"

	"github.com/sadewadee/foxhound/fetch"
	"github.com/sadewadee/foxhound/fetch/presets"
)

// TestStealthFetcher_FingerprintOptionsAccepted verifies the public API
// accepts every fingerprint option without panicking, on both default and tls
// builds. The semantic effect (actually changing the wire bytes) is covered by
// stealth_tls_fingerprint_test.go which is gated behind -tags tls.
func TestStealthFetcher_FingerprintOptionsAccepted(t *testing.T) {
	bundle := presets.FirefoxLatest()

	// HTTP/2 fingerprint string captured from real Firefox; foxhound does not
	// expose this in presets (see issue #41 — pairing it with JA3 is broken in
	// azuretls). We pass a literal here just to exercise the option's accept path.
	const firefoxHTTP2 = "1:65536;4:131072;5:16384|12517377|3:0:0:201,5:0:0:101,7:0:0:1,9:0:7:1,11:0:3:1,13:0:0:241|m,p,a,s"

	f := fetch.NewStealth(
		fetch.WithIdentity(testProfile()),
		fetch.WithJA3(bundle.JA3),
		fetch.WithHTTP2Fingerprint(firefoxHTTP2),
		fetch.WithHTTP3Fingerprint("1:65536;6:262144;7:100;51:1;GREASE|m,a,s,p"),
	)
	defer f.Close()
	if f == nil {
		t.Fatal("NewStealth returned nil")
	}
}

// TestStealthFetcher_JA3PoolEmptyIsSafe ensures an empty pool is silently
// accepted rather than panicking on a zero-length rand.IntN call.
func TestStealthFetcher_JA3PoolEmptyIsSafe(t *testing.T) {
	f := fetch.NewStealth(
		fetch.WithIdentity(testProfile()),
		fetch.WithJA3Pool(nil),
		fetch.WithJA3Pool([]string{}),
	)
	defer f.Close()
}

// TestStealthFetcher_JA3PoolPicks verifies a populated pool is accepted.
// foxhound only ships one curated Firefox JA3 in fetch/presets (Camoufox-only
// stance). Pools are useful for callers rotating multiple captured Firefox JA3s.
func TestStealthFetcher_JA3PoolPicks(t *testing.T) {
	pool := []string{presets.FirefoxLatest().JA3}
	f := fetch.NewStealth(
		fetch.WithIdentity(testProfile()),
		fetch.WithJA3Pool(pool),
	)
	defer f.Close()
}

// TestStealthFetcher_IsImpersonating asserts the build-tag accessor returns
// the expected value for the active build. The companion file
// stealth_default_isimpersonating_test.go pins the default-build expectation;
// stealth_tls_isimpersonating_test.go pins the tls-build expectation. Both
// reach this same NewStealth path so a shared sanity check lives here.
func TestStealthFetcher_IsImpersonatingSettable(t *testing.T) {
	f := fetch.NewStealth(fetch.WithIdentity(testProfile()))
	defer f.Close()
	// Just exercise the method — value is build-specific.
	_ = f.IsImpersonating()
}
