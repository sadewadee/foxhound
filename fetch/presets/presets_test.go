package presets_test

import (
	"strings"
	"testing"

	"github.com/sadewadee/foxhound/fetch/presets"
)

// TestBundleShapes verifies every curated bundle has all four fields populated
// and the JA3/HTTP2 strings are at minimum syntactically plausible. We don't
// validate against azuretls here — tls-build tests cover that path.
func TestBundleShapes(t *testing.T) {
	for _, b := range presets.All() {
		t.Run(b.Name, func(t *testing.T) {
			if b.Browser == "" {
				t.Errorf("Browser empty for %s", b.Name)
			}
			// JA3 must have 5 comma-separated fields (Version,Ciphers,Extensions,Curves,Formats).
			if got := strings.Count(b.JA3, ","); got < 4 {
				t.Errorf("JA3 has %d commas, want at least 4: %q", got, b.JA3)
			}
			// HTTP/2 fingerprint must have 4 pipe-separated sections.
			if got := strings.Count(b.HTTP2, "|"); got != 3 {
				t.Errorf("HTTP/2 has %d pipes, want 3: %q", got, b.HTTP2)
			}
		})
	}
}

// TestJA3Pool confirms the helper preserves input order and length.
func TestJA3Pool(t *testing.T) {
	bundles := presets.All()
	pool := presets.JA3Pool(bundles)
	if len(pool) != len(bundles) {
		t.Fatalf("len(pool)=%d want %d", len(pool), len(bundles))
	}
	for i, want := range bundles {
		if pool[i] != want.JA3 {
			t.Errorf("pool[%d]=%q want %q", i, pool[i], want.JA3)
		}
	}
}

// TestKnownBrowsers verifies each curated bundle declares one of the three
// browser families azuretls recognises. Anything else would silently fall
// back to azuretls' default at ApplyJa3 time.
func TestKnownBrowsers(t *testing.T) {
	known := map[string]bool{"firefox": true, "chrome": true, "safari": true}
	for _, b := range presets.All() {
		if !known[b.Browser] {
			t.Errorf("bundle %s declares unknown browser %q", b.Name, b.Browser)
		}
	}
}
