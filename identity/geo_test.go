package identity_test

import (
	"testing"

	"github.com/sadewadee/foxhound/identity"
)

// ---------------------------------------------------------------------------
// LookupCountry
// ---------------------------------------------------------------------------

func TestLookupCountryKnownCode(t *testing.T) {
	info, ok := identity.LookupCountry("US")
	if !ok {
		t.Fatal("LookupCountry(\"US\") returned ok=false; expected ok=true")
	}
	if info.Country != "US" {
		t.Errorf("Country: got %q, want \"US\"", info.Country)
	}
	if info.Timezone == "" {
		t.Error("Timezone must not be empty for US")
	}
	if info.Locale == "" {
		t.Error("Locale must not be empty for US")
	}
	if len(info.Languages) == 0 {
		t.Error("Languages must not be empty for US")
	}
}

func TestLookupCountryUnknownCodeReturnsFalse(t *testing.T) {
	_, ok := identity.LookupCountry("ZZ")
	if ok {
		t.Error("LookupCountry(\"ZZ\") returned ok=true for unknown code; expected false")
	}
}

func TestLookupCountryEmptyCodeReturnsFalse(t *testing.T) {
	_, ok := identity.LookupCountry("")
	if ok {
		t.Error("LookupCountry(\"\") should return ok=false")
	}
}

// TestLookupCountryCoverage verifies the table has a reasonable number of
// countries and that every entry has the mandatory fields filled in.
func TestLookupCountryCoverage(t *testing.T) {
	requiredCodes := []string{
		"US", "GB", "DE", "FR", "JP",
		"NL", "AU", "CA", "BR", "IN",
		"KR", "SG", "IT", "ES", "SE",
		"NO", "PL", "RU", "MX", "AR",
		"ID", "TH", "PH", "VN", "ZA",
	}
	for _, code := range requiredCodes {
		info, ok := identity.LookupCountry(code)
		if !ok {
			t.Errorf("LookupCountry(%q) not found; add it to builtinGeoTable", code)
			continue
		}
		if info.Timezone == "" {
			t.Errorf("code %s: Timezone empty", code)
		}
		if info.Locale == "" {
			t.Errorf("code %s: Locale empty", code)
		}
		if len(info.Languages) == 0 {
			t.Errorf("code %s: Languages empty", code)
		}
		if info.Lat == 0 && info.Lng == 0 {
			t.Errorf("code %s: both Lat and Lng are zero (null island)", code)
		}
	}
}

func TestLookupCountryCoordinatesPlausible(t *testing.T) {
	tests := []struct {
		code    string
		minLat  float64
		maxLat  float64
		minLng  float64
		maxLng  float64
	}{
		// US: continental USA bounding box (very rough)
		{"US", 24, 50, -125, -66},
		// JP: Japan
		{"JP", 24, 46, 122, 146},
		// GB: UK
		{"GB", 49, 61, -8, 2},
	}
	for _, tt := range tests {
		info, ok := identity.LookupCountry(tt.code)
		if !ok {
			t.Fatalf("country %s not found", tt.code)
		}
		if info.Lat < tt.minLat || info.Lat > tt.maxLat {
			t.Errorf("%s: Lat %.4f out of expected range [%.1f, %.1f]",
				tt.code, info.Lat, tt.minLat, tt.maxLat)
		}
		if info.Lng < tt.minLng || info.Lng > tt.maxLng {
			t.Errorf("%s: Lng %.4f out of expected range [%.1f, %.1f]",
				tt.code, info.Lng, tt.minLng, tt.maxLng)
		}
	}
}

// ---------------------------------------------------------------------------
// BuiltinResolver
// ---------------------------------------------------------------------------

func TestBuiltinResolverKnownCountryIP(t *testing.T) {
	// The builtin resolver does not do real IP→country lookup; it accepts a
	// "country:<CODE>" tagged string from proxy metadata.
	r := &identity.BuiltinResolver{}
	info, err := r.Resolve("country:DE")
	if err != nil {
		t.Fatalf("Resolve(\"country:DE\") error: %v", err)
	}
	if info == nil {
		t.Fatal("Resolve returned nil info")
	}
	if info.Country != "DE" {
		t.Errorf("Country: got %q, want \"DE\"", info.Country)
	}
	if info.Timezone != "Europe/Berlin" {
		t.Errorf("Timezone: got %q, want \"Europe/Berlin\"", info.Timezone)
	}
}

func TestBuiltinResolverUnknownTagReturnsError(t *testing.T) {
	r := &identity.BuiltinResolver{}
	_, err := r.Resolve("country:ZZ")
	if err == nil {
		t.Error("expected an error for unknown country code, got nil")
	}
}

func TestBuiltinResolverPlainIPReturnsError(t *testing.T) {
	// Plain IPs are not resolved by the builtin resolver (no database).
	r := &identity.BuiltinResolver{}
	_, err := r.Resolve("1.2.3.4")
	if err == nil {
		t.Error("expected an error for plain IP (no database), got nil")
	}
}

// ---------------------------------------------------------------------------
// WithCountry option
// ---------------------------------------------------------------------------

func TestWithCountryAppliesGeoToProfile(t *testing.T) {
	p := identity.Generate(identity.WithCountry("JP"))
	if p.Timezone != "Asia/Tokyo" {
		t.Errorf("Timezone: got %q, want \"Asia/Tokyo\"", p.Timezone)
	}
	if p.Locale != "ja-JP" {
		t.Errorf("Locale: got %q, want \"ja-JP\"", p.Locale)
	}
	if len(p.Languages) == 0 || p.Languages[0] != "ja-JP" {
		t.Errorf("Languages[0]: got %v, want \"ja-JP\"", p.Languages)
	}
	if p.Lat == 0 && p.Lng == 0 {
		t.Error("Lat/Lng should be non-zero for JP")
	}
}

func TestWithCountryUnknownCodeNoEffect(t *testing.T) {
	// An unrecognised code must not panic and must produce a valid profile.
	p := identity.Generate(identity.WithCountry("ZZ"))
	if p == nil {
		t.Fatal("Generate returned nil for unknown country code")
	}
	if p.Timezone == "" {
		t.Error("Timezone must not be empty even for unknown country")
	}
}

// TestWithCountryDoesNotOverrideExplicitLocale verifies that explicitly setting
// locale after WithCountry wins (options are applied in order).
func TestWithCountryDoesNotOverrideExplicitLocale(t *testing.T) {
	p := identity.Generate(
		identity.WithCountry("JP"),
		identity.WithLocale("en-US", "en"),
	)
	if p.Locale != "en-US" {
		t.Errorf("explicit WithLocale should win; got %q, want \"en-US\"", p.Locale)
	}
}

// ---------------------------------------------------------------------------
// WithGeoResolver option
// ---------------------------------------------------------------------------

// mockResolver is a test-only GeoResolver that returns a fixed GeoInfo.
type mockResolver struct {
	info *identity.GeoInfo
	err  error
}

func (m *mockResolver) Resolve(_ string) (*identity.GeoInfo, error) {
	return m.info, m.err
}

func TestWithGeoResolverIsUsedDuringGenerate(t *testing.T) {
	fixed := &identity.GeoInfo{
		Country:   "SG",
		City:      "Singapore",
		Timezone:  "Asia/Singapore",
		Locale:    "en-SG",
		Languages: []string{"en-SG", "en"},
		Lat:       1.3521,
		Lng:       103.8198,
	}
	p := identity.Generate(
		identity.WithProxy("1.2.3.4"),
		identity.WithGeoResolver(&mockResolver{info: fixed}),
	)
	if p.Timezone != "Asia/Singapore" {
		t.Errorf("Timezone: got %q, want \"Asia/Singapore\"", p.Timezone)
	}
	if p.Locale != "en-SG" {
		t.Errorf("Locale: got %q, want \"en-SG\"", p.Locale)
	}
}

// TestWithGeoResolverErrorFallsBackToDefaults verifies that when a resolver
// returns an error, Generate does not panic and still produces a valid profile.
func TestWithGeoResolverErrorFallsBackToDefaults(t *testing.T) {
	r := &mockResolver{err: &testResolveError{}}
	p := identity.Generate(
		identity.WithProxy("1.2.3.4"),
		identity.WithGeoResolver(r),
	)
	if p == nil {
		t.Fatal("Generate must not return nil when resolver errors")
	}
	if p.Timezone == "" {
		t.Error("Timezone must not be empty after resolver error")
	}
}

type testResolveError struct{}

func (e *testResolveError) Error() string { return "mock resolve error" }
