package identity

import (
	"fmt"
	"log/slog"
	"strings"
)

// GeoInfo contains geographic information derived from an IP address or country code.
type GeoInfo struct {
	Country   string   // ISO 3166-1 alpha-2 (e.g., "US")
	City      string
	Timezone  string   // IANA timezone (e.g., "America/New_York")
	Locale    string   // e.g., "en-US"
	Languages []string // e.g., ["en-US", "en"]
	Lat       float64
	Lng       float64
}

// builtinGeoTable maps ISO 3166-1 alpha-2 country codes to default GeoInfo.
// Coordinates are representative city centres, not country centroids.
// All timezones are IANA tz database identifiers.
var builtinGeoTable = map[string]GeoInfo{
	"US": {Country: "US", City: "New York", Timezone: "America/New_York", Locale: "en-US", Languages: []string{"en-US", "en"}, Lat: 40.7128, Lng: -74.0060},
	"GB": {Country: "GB", City: "London", Timezone: "Europe/London", Locale: "en-GB", Languages: []string{"en-GB", "en"}, Lat: 51.5074, Lng: -0.1278},
	"DE": {Country: "DE", City: "Berlin", Timezone: "Europe/Berlin", Locale: "de-DE", Languages: []string{"de-DE", "de"}, Lat: 52.5200, Lng: 13.4050},
	"FR": {Country: "FR", City: "Paris", Timezone: "Europe/Paris", Locale: "fr-FR", Languages: []string{"fr-FR", "fr"}, Lat: 48.8566, Lng: 2.3522},
	"JP": {Country: "JP", City: "Tokyo", Timezone: "Asia/Tokyo", Locale: "ja-JP", Languages: []string{"ja-JP", "ja"}, Lat: 35.6762, Lng: 139.6503},
	"NL": {Country: "NL", City: "Amsterdam", Timezone: "Europe/Amsterdam", Locale: "nl-NL", Languages: []string{"nl-NL", "nl", "en"}, Lat: 52.3676, Lng: 4.9041},
	"AU": {Country: "AU", City: "Sydney", Timezone: "Australia/Sydney", Locale: "en-AU", Languages: []string{"en-AU", "en"}, Lat: -33.8688, Lng: 151.2093},
	"CA": {Country: "CA", City: "Toronto", Timezone: "America/Toronto", Locale: "en-CA", Languages: []string{"en-CA", "en", "fr-CA"}, Lat: 43.6532, Lng: -79.3832},
	"BR": {Country: "BR", City: "São Paulo", Timezone: "America/Sao_Paulo", Locale: "pt-BR", Languages: []string{"pt-BR", "pt"}, Lat: -23.5505, Lng: -46.6333},
	"IN": {Country: "IN", City: "Mumbai", Timezone: "Asia/Kolkata", Locale: "en-IN", Languages: []string{"en-IN", "hi-IN", "en"}, Lat: 19.0760, Lng: 72.8777},
	"KR": {Country: "KR", City: "Seoul", Timezone: "Asia/Seoul", Locale: "ko-KR", Languages: []string{"ko-KR", "ko"}, Lat: 37.5665, Lng: 126.9780},
	"SG": {Country: "SG", City: "Singapore", Timezone: "Asia/Singapore", Locale: "en-SG", Languages: []string{"en-SG", "en", "zh-SG"}, Lat: 1.3521, Lng: 103.8198},
	"IT": {Country: "IT", City: "Rome", Timezone: "Europe/Rome", Locale: "it-IT", Languages: []string{"it-IT", "it"}, Lat: 41.9028, Lng: 12.4964},
	"ES": {Country: "ES", City: "Madrid", Timezone: "Europe/Madrid", Locale: "es-ES", Languages: []string{"es-ES", "es"}, Lat: 40.4168, Lng: -3.7038},
	"SE": {Country: "SE", City: "Stockholm", Timezone: "Europe/Stockholm", Locale: "sv-SE", Languages: []string{"sv-SE", "sv", "en"}, Lat: 59.3293, Lng: 18.0686},
	"NO": {Country: "NO", City: "Oslo", Timezone: "Europe/Oslo", Locale: "nb-NO", Languages: []string{"nb-NO", "no", "en"}, Lat: 59.9139, Lng: 10.7522},
	"PL": {Country: "PL", City: "Warsaw", Timezone: "Europe/Warsaw", Locale: "pl-PL", Languages: []string{"pl-PL", "pl"}, Lat: 52.2297, Lng: 21.0122},
	"RU": {Country: "RU", City: "Moscow", Timezone: "Europe/Moscow", Locale: "ru-RU", Languages: []string{"ru-RU", "ru"}, Lat: 55.7558, Lng: 37.6173},
	"MX": {Country: "MX", City: "Mexico City", Timezone: "America/Mexico_City", Locale: "es-MX", Languages: []string{"es-MX", "es"}, Lat: 19.4326, Lng: -99.1332},
	"AR": {Country: "AR", City: "Buenos Aires", Timezone: "America/Argentina/Buenos_Aires", Locale: "es-AR", Languages: []string{"es-AR", "es"}, Lat: -34.6037, Lng: -58.3816},
	"ID": {Country: "ID", City: "Jakarta", Timezone: "Asia/Jakarta", Locale: "id-ID", Languages: []string{"id-ID", "id"}, Lat: -6.2088, Lng: 106.8456},
	"TH": {Country: "TH", City: "Bangkok", Timezone: "Asia/Bangkok", Locale: "th-TH", Languages: []string{"th-TH", "th"}, Lat: 13.7563, Lng: 100.5018},
	"PH": {Country: "PH", City: "Manila", Timezone: "Asia/Manila", Locale: "en-PH", Languages: []string{"en-PH", "fil-PH", "en"}, Lat: 14.5995, Lng: 120.9842},
	"VN": {Country: "VN", City: "Ho Chi Minh City", Timezone: "Asia/Ho_Chi_Minh", Locale: "vi-VN", Languages: []string{"vi-VN", "vi"}, Lat: 10.8231, Lng: 106.6297},
	"ZA": {Country: "ZA", City: "Johannesburg", Timezone: "Africa/Johannesburg", Locale: "en-ZA", Languages: []string{"en-ZA", "af-ZA", "en"}, Lat: -26.2041, Lng: 28.0473},
}

// LookupCountry returns the built-in GeoInfo for a country code.
// The second return value is false when the code is not in the table.
func LookupCountry(countryCode string) (GeoInfo, bool) {
	if countryCode == "" {
		return GeoInfo{}, false
	}
	info, ok := builtinGeoTable[strings.ToUpper(countryCode)]
	return info, ok
}

// GeoResolver resolves IP addresses (or tagged strings) to GeoInfo.
// Implement this interface to plug in a real MaxMind database or any
// external geo service.
type GeoResolver interface {
	Resolve(ip string) (*GeoInfo, error)
}

// BuiltinResolver uses the built-in country table.
//
// Because the builtin resolver has no IP→country database it only handles
// proxy metadata strings in the form "country:<CODE>" (e.g., "country:DE").
// Plain IP addresses return an error — use a real GeoResolver (e.g., MaxMind)
// for those.
type BuiltinResolver struct{}

// Resolve parses the "country:<CODE>" tag injected by proxy providers and
// returns the matching GeoInfo from the built-in table.
// Any other format (plain IP, CIDR, etc.) returns a descriptive error.
func (r *BuiltinResolver) Resolve(ip string) (*GeoInfo, error) {
	const prefix = "country:"
	if !strings.HasPrefix(ip, prefix) {
		return nil, fmt.Errorf("identity/geo: BuiltinResolver cannot resolve %q — "+
			"no IP database available; use \"country:CODE\" format or provide a GeoResolver", ip)
	}
	code := strings.ToUpper(strings.TrimPrefix(ip, prefix))
	info, ok := builtinGeoTable[code]
	if !ok {
		return nil, fmt.Errorf("identity/geo: country code %q not found in built-in table", code)
	}
	clone := info // return a copy so callers cannot mutate the table
	return &clone, nil
}

// applyGeoToConfig uses a GeoResolver to populate missing geo fields in cfg.
// Fields already set explicitly (locale, tz, lat/lng) are never overwritten.
// Errors from the resolver are logged at WARN level and silently ignored so
// that Generate never fails due to geo lookup failures.
func applyGeoToConfig(cfg *generateConfig) {
	if cfg.geoResolver == nil && cfg.proxyIP == "" && cfg.country == "" {
		return // nothing to do
	}

	// Build the resolver input: explicit country wins, then proxy IP.
	input := cfg.proxyIP
	if cfg.country != "" {
		input = "country:" + cfg.country
	}
	if input == "" {
		return
	}

	// Select resolver: caller-supplied first, then builtin.
	resolver := cfg.geoResolver
	if resolver == nil {
		resolver = &BuiltinResolver{}
	}

	info, err := resolver.Resolve(input)
	if err != nil {
		slog.Warn("identity/geo: geo resolution failed, using profile defaults",
			"input", input,
			"err", err,
		)
		return
	}
	if info == nil {
		return
	}

	// Only apply fields that were not set explicitly by the caller.
	if cfg.locale == "" {
		cfg.locale = info.Locale
		if len(cfg.langs) == 0 {
			cfg.langs = info.Languages
		}
	}
	if cfg.tz == "" {
		cfg.tz = info.Timezone
	}
	if cfg.lat == 0 && cfg.lng == 0 {
		cfg.lat = info.Lat
		cfg.lng = info.Lng
	}
}
