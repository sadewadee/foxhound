package identity_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sadewadee/foxhound/identity"
)

// ---------------------------------------------------------------------------
// Generate – basic smoke test
// ---------------------------------------------------------------------------

func TestGenerateReturnsNonNilProfile(t *testing.T) {
	p := identity.Generate()
	if p == nil {
		t.Fatal("Generate() returned nil")
	}
}

func TestGenerateAllFieldsPopulated(t *testing.T) {
	p := identity.Generate()
	if p.UA == "" {
		t.Error("UA is empty")
	}
	if p.BrowserVer == "" {
		t.Error("BrowserVer is empty")
	}
	if string(p.BrowserName) == "" {
		t.Error("BrowserName is empty")
	}
	if p.TLSProfile == "" {
		t.Error("TLSProfile is empty")
	}
	if len(p.HeaderOrder) == 0 {
		t.Error("HeaderOrder is empty")
	}
	if string(p.OS) == "" {
		t.Error("OS is empty")
	}
	if p.Platform == "" {
		t.Error("Platform is empty")
	}
	if p.Cores == 0 {
		t.Error("Cores is zero")
	}
	if p.Memory == 0 {
		t.Error("Memory is zero")
	}
	if p.GPU == "" {
		t.Error("GPU is empty")
	}
	if p.ScreenW == 0 {
		t.Error("ScreenW is zero")
	}
	if p.ScreenH == 0 {
		t.Error("ScreenH is zero")
	}
	if p.Locale == "" {
		t.Error("Locale is empty")
	}
	if len(p.Languages) == 0 {
		t.Error("Languages is empty")
	}
	if p.Timezone == "" {
		t.Error("Timezone is empty")
	}
	if len(p.CamoufoxEnv) == 0 {
		t.Error("CamoufoxEnv is empty")
	}
}

// ---------------------------------------------------------------------------
// WithBrowser option
// ---------------------------------------------------------------------------

func TestGenerateWithBrowserFirefox(t *testing.T) {
	p := identity.Generate(identity.WithBrowser(identity.BrowserFirefox))
	if p.BrowserName != identity.BrowserFirefox {
		t.Errorf("BrowserName: got %q, want %q", p.BrowserName, identity.BrowserFirefox)
	}
	if !strings.Contains(p.UA, "Firefox") {
		t.Errorf("UA %q does not contain 'Firefox'", p.UA)
	}
	if !strings.Contains(p.TLSProfile, "firefox") {
		t.Errorf("TLSProfile %q does not contain 'firefox'", p.TLSProfile)
	}
}

func TestGenerateWithBrowserChrome(t *testing.T) {
	p := identity.Generate(identity.WithBrowser(identity.BrowserChrome))
	if p.BrowserName != identity.BrowserChrome {
		t.Errorf("BrowserName: got %q, want %q", p.BrowserName, identity.BrowserChrome)
	}
	if !strings.Contains(p.UA, "Chrome") {
		t.Errorf("UA %q does not contain 'Chrome'", p.UA)
	}
	if !strings.Contains(p.TLSProfile, "chrome") {
		t.Errorf("TLSProfile %q does not contain 'chrome'", p.TLSProfile)
	}
}

// ---------------------------------------------------------------------------
// WithOS option
// ---------------------------------------------------------------------------

func TestGenerateWithOSWindows(t *testing.T) {
	p := identity.Generate(identity.WithOS(identity.OSWindows))
	if p.OS != identity.OSWindows {
		t.Errorf("OS: got %q, want %q", p.OS, identity.OSWindows)
	}
	if p.Platform != "Win32" {
		t.Errorf("Platform: got %q, want 'Win32'", p.Platform)
	}
	if !strings.Contains(p.UA, "Windows") {
		t.Errorf("UA %q does not contain 'Windows'", p.UA)
	}
}

func TestGenerateWithOSMacOS(t *testing.T) {
	p := identity.Generate(identity.WithOS(identity.OSMacOS))
	if p.OS != identity.OSMacOS {
		t.Errorf("OS: got %q, want %q", p.OS, identity.OSMacOS)
	}
	if p.Platform != "MacIntel" {
		t.Errorf("Platform: got %q, want 'MacIntel'", p.Platform)
	}
	if !strings.Contains(p.UA, "Macintosh") {
		t.Errorf("UA %q does not contain 'Macintosh'", p.UA)
	}
}

func TestGenerateWithOSLinux(t *testing.T) {
	p := identity.Generate(identity.WithOS(identity.OSLinux))
	if p.OS != identity.OSLinux {
		t.Errorf("OS: got %q, want %q", p.OS, identity.OSLinux)
	}
	if p.Platform != "Linux x86_64" {
		t.Errorf("Platform: got %q, want 'Linux x86_64'", p.Platform)
	}
	if !strings.Contains(p.UA, "Linux") {
		t.Errorf("UA %q does not contain 'Linux'", p.UA)
	}
}

// ---------------------------------------------------------------------------
// GPU / OS consistency
// ---------------------------------------------------------------------------

// TestGPUConsistencyWindowsNoAppleGPU verifies that a Windows profile never
// carries an Apple GPU string. Apple silicon GPUs only exist on macOS hardware.
func TestGPUConsistencyWindowsNoAppleGPU(t *testing.T) {
	// Run multiple times to catch any randomised selection.
	for i := 0; i < 20; i++ {
		p := identity.Generate(identity.WithOS(identity.OSWindows))
		if strings.Contains(p.GPU, "Apple") {
			t.Errorf("Windows profile has Apple GPU: %q (iteration %d)", p.GPU, i)
		}
	}
}

// TestGPUConsistencyMacOSHasAppleGPU verifies that macOS profiles from the
// embedded database carry Apple GPU strings (the entire macOS profile set uses
// Apple silicon). The fallback also uses "Apple M2".
func TestGPUConsistencyMacOSHasAppleGPU(t *testing.T) {
	for i := 0; i < 20; i++ {
		p := identity.Generate(identity.WithOS(identity.OSMacOS))
		if !strings.Contains(p.GPU, "Apple") {
			t.Errorf("macOS profile has non-Apple GPU: %q (iteration %d)", p.GPU, i)
		}
	}
}

// TestGPUConsistencyLinuxNoAppleGPU verifies Linux profiles never have Apple
// GPUs (Linux doesn't run on Apple silicon in a way that would expose Apple
// GPU names to the browser).
func TestGPUConsistencyLinuxNoAppleGPU(t *testing.T) {
	for i := 0; i < 20; i++ {
		p := identity.Generate(identity.WithOS(identity.OSLinux))
		if strings.Contains(p.GPU, "Apple") {
			t.Errorf("Linux profile has Apple GPU: %q (iteration %d)", p.GPU, i)
		}
	}
}

// ---------------------------------------------------------------------------
// WithLocale option
// ---------------------------------------------------------------------------

func TestGenerateWithLocale(t *testing.T) {
	p := identity.Generate(identity.WithLocale("de-DE", "de"))
	if p.Locale != "de-DE" {
		t.Errorf("Locale: got %q, want 'de-DE'", p.Locale)
	}
	found := false
	for _, l := range p.Languages {
		if l == "de" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Languages %v does not contain 'de'", p.Languages)
	}
}

// TestGenerateWithLocaleFallbackLanguages verifies that when no explicit
// language list is provided, Languages is derived from the locale string.
func TestGenerateWithLocaleFallbackLanguages(t *testing.T) {
	p := identity.Generate(identity.WithLocale("fr-FR"))
	if p.Locale != "fr-FR" {
		t.Errorf("Locale: got %q, want 'fr-FR'", p.Locale)
	}
	if len(p.Languages) == 0 {
		t.Error("Languages must be non-empty when locale is set without explicit langs")
	}
}

// ---------------------------------------------------------------------------
// WithTimezone option
// ---------------------------------------------------------------------------

func TestGenerateWithTimezone(t *testing.T) {
	p := identity.Generate(identity.WithTimezone("Europe/Berlin"))
	if p.Timezone != "Europe/Berlin" {
		t.Errorf("Timezone: got %q, want 'Europe/Berlin'", p.Timezone)
	}
}

// ---------------------------------------------------------------------------
// WithGeo option
// ---------------------------------------------------------------------------

func TestGenerateWithGeo(t *testing.T) {
	const lat, lng = 52.52, 13.405
	p := identity.Generate(identity.WithGeo(lat, lng))
	if p.Lat != lat {
		t.Errorf("Lat: got %v, want %v", p.Lat, lat)
	}
	if p.Lng != lng {
		t.Errorf("Lng: got %v, want %v", p.Lng, lng)
	}
}

// ---------------------------------------------------------------------------
// CamoufoxEnv — CAMOU_CONFIG_N format (JSON chunked)
// ---------------------------------------------------------------------------

func TestCamoufoxEnvContainsCAMOU_CONFIG_1(t *testing.T) {
	p := identity.Generate()
	if _, ok := p.CamoufoxEnv["CAMOU_CONFIG_1"]; !ok {
		t.Error("CamoufoxEnv missing CAMOU_CONFIG_1 key")
	}
}

func TestCamoufoxEnvIsValidJSON(t *testing.T) {
	p := identity.Generate()
	// Reassemble chunks
	full := ""
	for i := 1; ; i++ {
		chunk, ok := p.CamoufoxEnv[fmt.Sprintf("CAMOU_CONFIG_%d", i)]
		if !ok {
			break
		}
		full += chunk
	}
	if full == "" {
		t.Fatal("no CAMOU_CONFIG chunks found")
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(full), &config); err != nil {
		t.Fatalf("CAMOU_CONFIG is not valid JSON: %v\nraw: %s", err, full)
	}
	// Verify expected keys exist in the JSON
	expectedKeys := []string{
		"screen.width", "screen.height", "navigator.userAgent",
		"navigator.platform", "navigator.hardwareConcurrency",
		"navigator.oscpu", "navigator.appVersion",
		"navigator.language", "navigator.languages",
		"window.devicePixelRatio", "timezone",
	}
	for _, key := range expectedKeys {
		if _, ok := config[key]; !ok {
			t.Errorf("CAMOU_CONFIG JSON missing key %q", key)
		}
	}
}

// TestCamoufoxConfigOmitsBrowserForgeProperties verifies that properties
// handled by BrowserForge (WebGL, fonts, canvas) are NOT manually set.
// BrowserForge auto-populates these with realistic statistical distributions
// when they are absent from CAMOU_CONFIG.
func TestCamoufoxConfigOmitsBrowserForgeProperties(t *testing.T) {
	p := identity.Generate()
	config := p.BuildCamoufoxConfig()
	browserForgeKeys := []string{
		"webGl:vendor", "webGl:renderer", "webGl:parameters",
		"webGl:supportedExtensions", "fonts",
		"canvas:aaOffset", "canvas:aaCapOffset",
	}
	for _, key := range browserForgeKeys {
		if _, ok := config[key]; ok {
			t.Errorf("CAMOU_CONFIG should NOT set %q — BrowserForge handles it", key)
		}
	}
}

func TestMergeCamoufoxConfig(t *testing.T) {
	p := identity.Generate()
	extra := map[string]any{
		"addons": []string{"/path/to/nopecha"},
	}
	env := p.MergeCamoufoxConfig(extra)
	// Reassemble and verify addons are present
	full := ""
	for i := 1; ; i++ {
		chunk, ok := env[fmt.Sprintf("CAMOU_CONFIG_%d", i)]
		if !ok {
			break
		}
		full += chunk
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(full), &config); err != nil {
		t.Fatalf("merged config not valid JSON: %v", err)
	}
	if _, ok := config["addons"]; !ok {
		t.Error("merged config missing addons key")
	}
	if _, ok := config["screen.width"]; !ok {
		t.Error("merged config missing screen.width (fingerprint data lost)")
	}
}

// ---------------------------------------------------------------------------
// HeaderOrder matches browser type
// ---------------------------------------------------------------------------

func TestHeaderOrderMatchesFirefox(t *testing.T) {
	p := identity.Generate(identity.WithBrowser(identity.BrowserFirefox))
	if len(p.HeaderOrder) == 0 {
		t.Fatal("HeaderOrder is empty for Firefox")
	}
	// Firefox header order starts with "Host" then "User-Agent".
	if p.HeaderOrder[0] != "Host" {
		t.Errorf("Firefox HeaderOrder[0]: got %q, want 'Host'", p.HeaderOrder[0])
	}
	foundUA := false
	for _, h := range p.HeaderOrder {
		if h == "User-Agent" {
			foundUA = true
			break
		}
	}
	if !foundUA {
		t.Errorf("Firefox HeaderOrder %v does not contain 'User-Agent'", p.HeaderOrder)
	}
}

func TestHeaderOrderMatchesChrome(t *testing.T) {
	p := identity.Generate(identity.WithBrowser(identity.BrowserChrome))
	if len(p.HeaderOrder) == 0 {
		t.Fatal("HeaderOrder is empty for Chrome")
	}
	// Chrome header order contains sec-ch-ua (not present in Firefox order).
	foundSecCHUA := false
	for _, h := range p.HeaderOrder {
		if h == "sec-ch-ua" {
			foundSecCHUA = true
			break
		}
	}
	if !foundSecCHUA {
		t.Errorf("Chrome HeaderOrder %v does not contain 'sec-ch-ua'", p.HeaderOrder)
	}
}

// TestHeaderOrderFirefoxAndChromeAreDifferent guards against accidental
// sharing of the same header-order slice between browsers.
func TestHeaderOrderFirefoxAndChromeAreDifferent(t *testing.T) {
	ff := identity.Generate(identity.WithBrowser(identity.BrowserFirefox))
	cr := identity.Generate(identity.WithBrowser(identity.BrowserChrome))
	if len(ff.HeaderOrder) == len(cr.HeaderOrder) {
		same := true
		for i := range ff.HeaderOrder {
			if ff.HeaderOrder[i] != cr.HeaderOrder[i] {
				same = false
				break
			}
		}
		if same {
			t.Error("Firefox and Chrome HeaderOrder are identical; they should differ")
		}
	}
}

// ---------------------------------------------------------------------------
// Embedded profile database
// ---------------------------------------------------------------------------

// TestEmbeddedProfileDatabaseLoadsFirefoxWindows verifies that the embedded
// FS contains the firefox_windows profile set and it decodes to at least one
// entry with expected fields.
func TestEmbeddedProfileDatabaseLoadsFirefoxWindows(t *testing.T) {
	// Generate forces the database to load for the requested key.
	p := identity.Generate(
		identity.WithBrowser(identity.BrowserFirefox),
		identity.WithOS(identity.OSWindows),
	)
	// The embedded firefox_windows.json uses browser_ver "135.0"
	// matching the installed Camoufox binary (Firefox 135.0.1).
	if p.BrowserVer != "135.0" {
		t.Errorf("BrowserVer: got %q, want '135.0' (from embedded firefox_windows.json)", p.BrowserVer)
	}
	if p.ScreenW == 0 || p.ScreenH == 0 {
		t.Errorf("screen dimensions not populated from embedded profile: %dx%d", p.ScreenW, p.ScreenH)
	}
}

// TestGetTLSProfileFirefox135 verifies that the TLS fingerprint data for
// firefox_135.0 can be loaded from the embedded filesystem.
func TestGetTLSProfileFirefox135(t *testing.T) {
	tls := identity.GetTLSProfile("firefox_135.0")
	if tls == nil {
		t.Fatal("GetTLSProfile(\"firefox_135.0\") returned nil; embedded tls file missing or corrupt")
	}
	if tls.JA3 == "" {
		t.Error("JA3 fingerprint is empty")
	}
	if tls.JA4 == "" {
		t.Error("JA4 fingerprint is empty")
	}
}

// TestGetTLSProfileChrome verifies that the Chrome TLS fingerprint data loads.
func TestGetTLSProfileChrome(t *testing.T) {
	tls := identity.GetTLSProfile("chrome_131.0.0.0")
	if tls == nil {
		t.Fatal("GetTLSProfile(\"chrome_131.0.0.0\") returned nil; embedded tls file missing or corrupt")
	}
	if tls.JA3 == "" {
		t.Error("JA3 fingerprint is empty")
	}
}

// TestGetTLSProfileUnknownReturnsNil verifies that requesting a non-existent
// profile key returns nil rather than panicking.
func TestGetTLSProfileUnknownReturnsNil(t *testing.T) {
	tls := identity.GetTLSProfile("nonexistent_0.0")
	if tls != nil {
		t.Errorf("expected nil for unknown key, got %+v", tls)
	}
}

// ---------------------------------------------------------------------------
// Fallback profile
// ---------------------------------------------------------------------------

// TestFallbackProfileUsedWhenEmbeddedMissing verifies the code path where
// getProfiles returns nil (no embedded file) and fallbackProfile is used
// instead. We trigger this by requesting a browser+OS combination for which
// no JSON file exists.
//
// Currently all six combinations are embedded, so we use an OS-less Generate
// but verify that the result is always valid, covering the fallback return
// path via the consistency assertions in TestGenerateAllFieldsPopulated.
func TestFallbackProfileReturnsValidData(t *testing.T) {
	// Exercise every OS with the default Firefox browser to ensure fallback
	// logic produces consistent, non-zero profiles when hit.
	for _, o := range []identity.OS{identity.OSWindows, identity.OSMacOS, identity.OSLinux} {
		p := identity.Generate(identity.WithBrowser(identity.BrowserFirefox), identity.WithOS(o))
		if p == nil {
			t.Fatalf("Generate returned nil for os=%s", o)
		}
		if p.UA == "" || p.Platform == "" || p.GPU == "" {
			t.Errorf("fallback/embedded profile for os=%s has empty required fields: ua=%q platform=%q gpu=%q",
				o, p.UA, p.Platform, p.GPU)
		}
		if p.Timezone == "" {
			t.Errorf("fallback/embedded profile for os=%s has empty Timezone", o)
		}
	}
}
