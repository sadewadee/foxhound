// Package identity provides consistent anti-detection identity profiles.
//
// Every request uses a complete, internally-consistent identity profile where
// UA + TLS fingerprint + header order + OS + hardware + screen + locale + geo
// all match. Random UA without matching TLS is worse than no rotation.
package identity

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
)

// Browser represents a supported browser type.
type Browser string

const (
	BrowserFirefox Browser = "firefox"
	BrowserChrome  Browser = "chrome"
)

// OS represents a supported operating system.
type OS string

const (
	OSWindows OS = "windows"
	OSMacOS   OS = "macos"
	OSLinux   OS = "linux"
)

// Profile contains a complete, internally-consistent identity for a scraping session.
// All attributes are guaranteed to match each other: UA matches browser+OS,
// TLS profile matches browser version, header order matches browser, GPU matches OS,
// screen resolution is common for OS, timezone matches proxy geo, etc.
type Profile struct {
	// Browser identification
	UA          string  `json:"ua"`
	BrowserName Browser `json:"browser_name"`
	BrowserVer  string  `json:"browser_ver"`

	// TLS fingerprint profile that MATCHES the browser
	TLSProfile string `json:"tls_profile"`

	// Header ordering specific to this browser version
	HeaderOrder []string `json:"header_order"`

	// Operating system
	OS        OS     `json:"os"`
	OSVersion string `json:"os_version"`
	Platform  string `json:"platform"`

	// Hardware (consistent with OS)
	Cores  int     `json:"cores"`
	Memory float64 `json:"memory"`
	GPU    string  `json:"gpu"`

	// Screen (common resolution for this OS)
	ScreenW    int     `json:"screen_w"`
	ScreenH    int     `json:"screen_h"`
	ColorDepth int     `json:"color_depth"`
	PixelRatio float64 `json:"pixel_ratio"`

	// Locale (matched to proxy geo)
	Languages []string `json:"languages"`
	Timezone  string   `json:"timezone"`
	Locale    string   `json:"locale"`

	// Geo (derived from proxy IP)
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`

	// Camoufox environment configuration
	CamoufoxEnv map[string]string `json:"camoufox_env,omitempty"`
}

// Option is a functional option for configuring identity generation.
type Option func(*generateConfig)

type generateConfig struct {
	browser     Browser
	os          OS
	proxyIP     string
	country     string
	lat         float64
	lng         float64
	tz          string
	locale      string
	langs       []string
	geoResolver GeoResolver
}

// WithBrowser constrains identity generation to a specific browser.
func WithBrowser(b Browser) Option {
	return func(c *generateConfig) {
		c.browser = b
	}
}

// WithOS constrains identity generation to a specific operating system.
func WithOS(o OS) Option {
	return func(c *generateConfig) {
		c.os = o
	}
}

// WithProxy sets the proxy IP for geo-matching of timezone, locale, and languages.
func WithProxy(ip string) Option {
	return func(c *generateConfig) {
		c.proxyIP = ip
	}
}

// WithGeo sets explicit geo coordinates for the identity.
func WithGeo(lat, lng float64) Option {
	return func(c *generateConfig) {
		c.lat = lat
		c.lng = lng
	}
}

// WithTimezone sets an explicit timezone for the identity.
func WithTimezone(tz string) Option {
	return func(c *generateConfig) {
		c.tz = tz
	}
}

// WithCountry constrains the identity to a specific country by looking up the
// built-in geo table (or the configured GeoResolver).  It is a convenience
// alternative to WithProxy when the caller knows the country directly.
// If the country code is unknown the option is silently ignored.
func WithCountry(code string) Option {
	return func(c *generateConfig) {
		c.country = strings.ToUpper(code)
	}
}

// WithGeoResolver plugs in a custom GeoResolver so callers can use a real
// MaxMind database or any external geo service instead of the built-in table.
// The resolver is called during Generate when WithProxy or WithCountry is set.
func WithGeoResolver(r GeoResolver) Option {
	return func(c *generateConfig) {
		c.geoResolver = r
	}
}

// WithLocale sets an explicit locale and languages for the identity.
func WithLocale(locale string, langs ...string) Option {
	return func(c *generateConfig) {
		c.locale = locale
		c.langs = langs
	}
}

// Generate creates a new consistent identity profile. All attributes are guaranteed
// to be internally consistent. Use functional options to constrain the generation.
func Generate(opts ...Option) *Profile {
	cfg := &generateConfig{
		browser: BrowserFirefox,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Pick random OS if not specified
	if cfg.os == "" {
		oses := []OS{OSWindows, OSMacOS, OSLinux}
		cfg.os = oses[rand.IntN(len(oses))]
	}

	// Build the profile key to look up from embedded database
	key := string(cfg.browser) + "_" + string(cfg.os)
	profiles := getProfiles(key)

	var base deviceProfile
	if len(profiles) > 0 {
		base = profiles[rand.IntN(len(profiles))]
	} else {
		base = fallbackProfile(cfg.browser, cfg.os)
	}

	// Build the consistent profile
	p := &Profile{
		BrowserName: cfg.browser,
		BrowserVer:  base.BrowserVer,
		OS:          cfg.os,
		OSVersion:   base.OSVersion,
		Platform:    platformForOS(cfg.os),
		Cores:       base.Cores,
		Memory:      base.Memory,
		GPU:         base.GPU,
		ScreenW:     base.ScreenW,
		ScreenH:     base.ScreenH,
		ColorDepth:  base.ColorDepth,
		PixelRatio:  base.PixelRatio,
	}

	// Set UA to match browser + OS + version exactly
	p.UA = buildUA(cfg.browser, p.BrowserVer, cfg.os, p.OSVersion)

	// Set TLS profile to match browser version
	p.TLSProfile = string(cfg.browser) + "_" + p.BrowserVer

	// Set header order for this browser
	p.HeaderOrder = headerOrderForBrowser(cfg.browser)

	// Resolve geo from proxy IP or country code when not explicitly overridden.
	applyGeoToConfig(cfg)

	// Set locale/geo from proxy or defaults
	if cfg.locale != "" {
		p.Locale = cfg.locale
		p.Languages = cfg.langs
		if len(p.Languages) == 0 {
			p.Languages = []string{cfg.locale, cfg.locale[:2]}
		}
	} else {
		p.Locale = base.Locale
		p.Languages = base.Languages
	}

	if cfg.tz != "" {
		p.Timezone = cfg.tz
	} else {
		p.Timezone = base.Timezone
	}

	if cfg.lat != 0 || cfg.lng != 0 {
		p.Lat = cfg.lat
		p.Lng = cfg.lng
	} else {
		p.Lat = base.Lat
		p.Lng = base.Lng
	}

	// Build Camoufox environment vars
	p.CamoufoxEnv = buildCamoufoxEnv(p)

	return p
}

// platformForOS returns the navigator.platform value for the given OS.
func platformForOS(o OS) string {
	switch o {
	case OSWindows:
		return "Win32"
	case OSMacOS:
		return "MacIntel"
	case OSLinux:
		return "Linux x86_64"
	default:
		return "Win32"
	}
}

// buildUA constructs a User-Agent string matching the browser, version, and OS.
func buildUA(browser Browser, ver string, o OS, osVer string) string {
	var osPart string
	switch o {
	case OSWindows:
		osPart = fmt.Sprintf("Windows NT %s; Win64; x64", osVer)
	case OSMacOS:
		osPart = fmt.Sprintf("Macintosh; Intel Mac OS X %s", osVer)
	case OSLinux:
		osPart = "X11; Linux x86_64"
	default:
		osPart = "Windows NT 10.0; Win64; x64"
	}

	switch browser {
	case BrowserFirefox:
		return fmt.Sprintf("Mozilla/5.0 (%s; rv:%s) Gecko/20100101 Firefox/%s", osPart, ver, ver)
	case BrowserChrome:
		return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", osPart, ver)
	default:
		return fmt.Sprintf("Mozilla/5.0 (%s; rv:%s) Gecko/20100101 Firefox/%s", osPart, ver, ver)
	}
}

// headerOrderForBrowser returns the canonical header ordering for the given browser.
func headerOrderForBrowser(browser Browser) []string {
	switch browser {
	case BrowserFirefox:
		return firefoxHeaderOrder
	case BrowserChrome:
		return chromeHeaderOrder
	default:
		return firefoxHeaderOrder
	}
}

var firefoxHeaderOrder = []string{
	"Host",
	"User-Agent",
	"Accept",
	"Accept-Language",
	"Accept-Encoding",
	"DNT",
	"Upgrade-Insecure-Requests",
	"Sec-Fetch-Dest",
	"Sec-Fetch-Mode",
	"Sec-Fetch-Site",
	"Sec-Fetch-User",
	"TE",
	"Priority",
	"Pragma",
	"Cache-Control",
}

var chromeHeaderOrder = []string{
	"Host",
	"Connection",
	"sec-ch-ua",
	"sec-ch-ua-mobile",
	"sec-ch-ua-platform",
	"Upgrade-Insecure-Requests",
	"User-Agent",
	"Accept",
	"Sec-Fetch-Site",
	"Sec-Fetch-Mode",
	"Sec-Fetch-User",
	"Sec-Fetch-Dest",
	"Accept-Encoding",
	"Accept-Language",
	"Priority",
}

// BuildCamoufoxConfig builds the Camoufox fingerprint configuration as a
// flat map suitable for JSON-encoding into CAMOU_CONFIG_N env vars.
//
// Camoufox expects all fingerprint overrides in a single JSON object split
// across CAMOU_CONFIG_1, CAMOU_CONFIG_2, ... environment variables (max ~2000
// chars each due to Windows env-var limits). The JSON uses dot-path keys like
// "screen.width", "navigator.userAgent", etc.
//
// IMPORTANT: Only set properties we want to CONTROL (identity consistency).
// BrowserForge auto-populates unset properties with realistic statistical
// distributions. Do NOT manually set: webGl:*, fonts, canvas:*, voices,
// shaderPrecisionFormats — BrowserForge handles these better than we can.
func (p *Profile) BuildCamoufoxConfig() map[string]any {
	config := map[string]any{
		// Navigator — our identity (must match exactly)
		"navigator.userAgent":            p.UA,
		"navigator.platform":             p.Platform,
		"navigator.hardwareConcurrency":  p.Cores,
		"navigator.oscpu":                buildOscpu(p),
		"navigator.appVersion":           buildAppVersion(p),
		"navigator.doNotTrack":           "unspecified",
		"navigator.globalPrivacyControl": false,

		// Screen — from our profile
		"screen.width":       p.ScreenW,
		"screen.height":      p.ScreenH,
		"screen.availWidth":  p.ScreenW,
		"screen.availHeight": screenAvailHeight(p),
		"screen.colorDepth":  p.ColorDepth,
		"screen.pixelDepth":  p.ColorDepth,

		// Window — derive from screen
		"window.outerWidth":       p.ScreenW,
		"window.outerHeight":      p.ScreenH,
		"window.devicePixelRatio": p.PixelRatio,
		"window.screenX":          0,
		"window.screenY":          0,
		"window.history.length":   1,

		// Geo/Locale
		"timezone": p.Timezone,
	}

	// Navigator language (primary + list)
	if len(p.Languages) > 0 {
		config["navigator.language"] = p.Languages[0]
		config["navigator.languages"] = p.Languages
	}

	// Locale for Camoufox C++ level
	if len(p.Languages) > 0 {
		config["locale:language"] = extractLang(p.Languages[0])
		if region := extractRegion(p.Languages[0]); region != "" {
			config["locale:region"] = region
		}
	}

	// Geolocation
	if p.Lat != 0 || p.Lng != 0 {
		config["geolocation:latitude"] = p.Lat
		config["geolocation:longitude"] = p.Lng
	}

	// DO NOT add:
	// - webGl:* (BrowserForge generates realistic WebGL params automatically)
	// - fonts (Camoufox bundles 200-600 OS-specific fonts automatically)
	// - canvas:* (Camoufox has built-in canvas anti-fingerprinting)
	// - voices (auto-generated by BrowserForge)
	// - shaderPrecisionFormats (auto-generated by BrowserForge)

	return config
}

// screenAvailHeight returns screen.availHeight accounting for OS taskbar.
func screenAvailHeight(p *Profile) int {
	switch p.OS {
	case OSWindows:
		return p.ScreenH - 40 // Windows taskbar
	case OSMacOS:
		return p.ScreenH - 25 // macOS menu bar
	case OSLinux:
		return p.ScreenH - 28 // GNOME/KDE panel
	default:
		return p.ScreenH - 40
	}
}

// extractLang extracts the language code from a locale string (e.g. "en-US" -> "en").
func extractLang(locale string) string {
	if parts := strings.SplitN(locale, "-", 2); len(parts) > 0 {
		return parts[0]
	}
	return locale
}

// extractRegion extracts the region code from a locale string (e.g. "en-US" -> "US").
func extractRegion(locale string) string {
	if parts := strings.SplitN(locale, "-", 2); len(parts) == 2 {
		return parts[1]
	}
	return ""
}

// BuildCamoufoxEnv marshals the Camoufox config to JSON and chunks it into
// CAMOU_CONFIG_1, CAMOU_CONFIG_2, ... environment variables.
func (p *Profile) BuildCamoufoxEnv() map[string]string {
	config := p.BuildCamoufoxConfig()
	configJSON, err := json.Marshal(config)
	if err != nil {
		// Fallback: return empty map (should never happen with valid profiles)
		return map[string]string{}
	}
	return chunkCamoufoxConfig(string(configJSON))
}

// MergeCamoufoxConfig merges additional config (e.g. addons) into the
// profile's Camoufox config, re-marshals to JSON, and returns chunked
// CAMOU_CONFIG_N env vars. This is used by the browser launcher to combine
// fingerprint config with addon config in a single CAMOU_CONFIG blob.
func (p *Profile) MergeCamoufoxConfig(extra map[string]any) map[string]string {
	config := p.BuildCamoufoxConfig()
	for k, v := range extra {
		config[k] = v
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return map[string]string{}
	}
	return chunkCamoufoxConfig(string(configJSON))
}

// chunkCamoufoxConfig splits a JSON string into CAMOU_CONFIG_1, CAMOU_CONFIG_2, ...
// env vars. Each chunk is at most 2000 bytes (Windows env var limit).
func chunkCamoufoxConfig(jsonStr string) map[string]string {
	const chunkSize = 2000
	result := make(map[string]string)
	for i := 0; i < len(jsonStr); i += chunkSize {
		end := i + chunkSize
		if end > len(jsonStr) {
			end = len(jsonStr)
		}
		key := fmt.Sprintf("CAMOU_CONFIG_%d", (i/chunkSize)+1)
		result[key] = jsonStr[i:end]
	}
	return result
}

// buildCamoufoxEnv is the internal wrapper that populates Profile.CamoufoxEnv.
// It delegates to BuildCamoufoxEnv which produces proper CAMOU_CONFIG_N vars.
func buildCamoufoxEnv(p *Profile) map[string]string {
	return p.BuildCamoufoxEnv()
}

// buildOscpu returns the navigator.oscpu value matching the profile's OS.
func buildOscpu(p *Profile) string {
	switch p.OS {
	case OSWindows:
		return "Windows NT " + p.OSVersion
	case OSMacOS:
		// macOS uses underscore in version for oscpu (e.g. "Intel Mac OS X 14_0")
		return "Intel Mac OS X " + p.OSVersion
	case OSLinux:
		return "Linux x86_64"
	default:
		return "Windows NT 10.0"
	}
}

// buildAppVersion returns the navigator.appVersion value.
// Firefox always starts with "5.0 (" followed by the oscpu string.
func buildAppVersion(p *Profile) string {
	return "5.0 (" + buildOscpu(p) + ")"
}

// NOTE: addWebGLConfig, addFontConfig, and OS font lists have been REMOVED.
// BrowserForge auto-generates realistic WebGL params, fonts, and canvas noise.
// Manually setting these was worse than letting BrowserForge handle them because
// our static values could be fingerprinted as a consistent cluster.

// AddWebRTCConfig adds WebRTC IP override to the config when a proxy exit IP
// is known. This prevents WebRTC from leaking the real IP address.
func AddWebRTCConfig(config map[string]any, proxyExitIP string) {
	if proxyExitIP != "" {
		config["webrtc:ipv4"] = proxyExitIP
		config["webrtc:ipv6"] = ""
	}
}
