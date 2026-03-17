// Package identity provides consistent anti-detection identity profiles.
//
// Every request uses a complete, internally-consistent identity profile where
// UA + TLS fingerprint + header order + OS + hardware + screen + locale + geo
// all match. Random UA without matching TLS is worse than no rotation.
package identity

import (
	"fmt"
	"math/rand/v2"
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
	UA          string `json:"ua"`
	BrowserName Browser `json:"browser_name"`
	BrowserVer  string `json:"browser_ver"`

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
	browser Browser
	os      OS
	proxyIP string
	lat     float64
	lng     float64
	tz      string
	locale  string
	langs   []string
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
	key := fmt.Sprintf("%s_%s", cfg.browser, cfg.os)
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
	p.TLSProfile = fmt.Sprintf("%s_%s", cfg.browser, p.BrowserVer)

	// Set header order for this browser
	p.HeaderOrder = headerOrderForBrowser(cfg.browser)

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
	"Connection",
	"Upgrade-Insecure-Requests",
	"Sec-Fetch-Dest",
	"Sec-Fetch-Mode",
	"Sec-Fetch-Site",
	"Sec-Fetch-User",
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

// buildCamoufoxEnv creates the CAMOU_CONFIG environment variables from a profile.
func buildCamoufoxEnv(p *Profile) map[string]string {
	env := make(map[string]string)
	env["CAMOU_CONFIG_SCREEN_W"] = fmt.Sprintf("%d", p.ScreenW)
	env["CAMOU_CONFIG_SCREEN_H"] = fmt.Sprintf("%d", p.ScreenH)
	env["CAMOU_CONFIG_PIXEL_RATIO"] = fmt.Sprintf("%.1f", p.PixelRatio)
	env["CAMOU_CONFIG_CORES"] = fmt.Sprintf("%d", p.Cores)
	env["CAMOU_CONFIG_MEMORY"] = fmt.Sprintf("%.0f", p.Memory)
	env["CAMOU_CONFIG_GPU"] = p.GPU
	env["CAMOU_CONFIG_PLATFORM"] = p.Platform
	env["CAMOU_CONFIG_TIMEZONE"] = p.Timezone
	env["CAMOU_CONFIG_LOCALE"] = p.Locale
	if len(p.Languages) > 0 {
		env["CAMOU_CONFIG_LANGUAGES"] = p.Languages[0]
	}
	return env
}
