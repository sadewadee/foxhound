package presets_test

import (
	"strings"
	"testing"

	"github.com/sadewadee/foxhound/fetch/presets"
)

// TestFirefoxLatest_Shape verifies the curated Firefox bundle has all fields
// populated and the JA3 string is at minimum syntactically plausible. We do
// not validate against azuretls here — tls-build tests in fetch/ cover that.
func TestFirefoxLatest_Shape(t *testing.T) {
	b := presets.FirefoxLatest()
	if b.Name == "" {
		t.Error("Name empty")
	}
	if b.Browser != "firefox" {
		t.Errorf("Browser = %q, want %q (foxhound is Firefox/Camoufox-only)", b.Browser, "firefox")
	}
	// JA3 must have 5 comma-separated fields (Version,Ciphers,Extensions,Curves,Formats).
	if got := strings.Count(b.JA3, ","); got < 4 {
		t.Errorf("JA3 has %d commas, want at least 4: %q", got, b.JA3)
	}
}
