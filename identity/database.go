package identity

import (
	"embed"
	"encoding/json"
	"sync"
)

//go:embed data/profiles/*.json
var profilesFS embed.FS

//go:embed data/tls/*.json
var tlsFS embed.FS

//go:embed data/headers/*.json
var headersFS embed.FS

// deviceProfile is a single device profile from the embedded database.
type deviceProfile struct {
	BrowserVer string   `json:"browser_ver"`
	OSVersion  string   `json:"os_version"`
	Cores      int      `json:"cores"`
	Memory     float64  `json:"memory"`
	GPU        string   `json:"gpu"`
	ScreenW    int      `json:"screen_w"`
	ScreenH    int      `json:"screen_h"`
	ColorDepth int      `json:"color_depth"`
	PixelRatio float64  `json:"pixel_ratio"`
	Languages  []string `json:"languages"`
	Timezone   string   `json:"timezone"`
	Locale     string   `json:"locale"`
	Lat        float64  `json:"lat"`
	Lng        float64  `json:"lng"`
}

// TLSProfileData holds JA3/JA4 and HTTP/2 fingerprint data for a browser.
type TLSProfileData struct {
	JA3   string        `json:"ja3"`
	JA4   string        `json:"ja4"`
	HTTP2 HTTP2Settings `json:"http2"`
}

// HTTP2Settings holds HTTP/2 connection settings for fingerprinting.
type HTTP2Settings struct {
	HeaderTableSize      uint32 `json:"header_table_size"`
	EnablePush           bool   `json:"enable_push"`
	MaxConcurrentStreams uint32 `json:"max_concurrent_streams"`
	InitialWindowSize    uint32 `json:"initial_window_size"`
	MaxFrameSize         uint32 `json:"max_frame_size"`
	MaxHeaderListSize    uint32 `json:"max_header_list_size"`
}

var (
	profileCache = make(map[string][]deviceProfile)
	profileMu    sync.RWMutex

	tlsCache = make(map[string]*TLSProfileData)
	tlsMu    sync.RWMutex
)

// getProfiles returns device profiles for the given key (e.g., "firefox_windows").
func getProfiles(key string) []deviceProfile {
	profileMu.RLock()
	if cached, ok := profileCache[key]; ok {
		profileMu.RUnlock()
		return cached
	}
	profileMu.RUnlock()

	profileMu.Lock()
	defer profileMu.Unlock()

	// Double-check after acquiring write lock
	if cached, ok := profileCache[key]; ok {
		return cached
	}

	filename := "data/profiles/" + key + ".json"
	data, err := profilesFS.ReadFile(filename)
	if err != nil {
		return nil
	}

	var profiles []deviceProfile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return nil
	}

	profileCache[key] = profiles
	return profiles
}

// GetTLSProfile returns the TLS fingerprint data for a browser version.
func GetTLSProfile(key string) *TLSProfileData {
	tlsMu.RLock()
	if cached, ok := tlsCache[key]; ok {
		tlsMu.RUnlock()
		return cached
	}
	tlsMu.RUnlock()

	tlsMu.Lock()
	defer tlsMu.Unlock()

	if cached, ok := tlsCache[key]; ok {
		return cached
	}

	filename := "data/tls/" + key + ".json"
	data, err := tlsFS.ReadFile(filename)
	if err != nil {
		return nil
	}

	var profile TLSProfileData
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil
	}

	tlsCache[key] = &profile
	return &profile
}

// fallbackProfile returns a hardcoded profile when the embedded database
// doesn't have a match. This ensures we always have a consistent profile.
func fallbackProfile(browser Browser, o OS) deviceProfile {
	dp := deviceProfile{
		ColorDepth: 24,
		Languages:  []string{"en-US", "en"},
		Timezone:   "America/New_York",
		Locale:     "en-US",
		Lat:        40.7128,
		Lng:        -74.0060,
	}

	switch browser {
	case BrowserFirefox:
		dp.BrowserVer = "135.0"
	case BrowserChrome:
		dp.BrowserVer = "131.0.0.0"
	}

	switch o {
	case OSWindows:
		dp.OSVersion = "10.0"
		dp.Cores = 8
		dp.Memory = 16
		dp.GPU = "Intel(R) UHD Graphics 630"
		dp.ScreenW = 1920
		dp.ScreenH = 1080
		dp.PixelRatio = 1.0
	case OSMacOS:
		dp.OSVersion = "14_0"
		dp.Cores = 10
		dp.Memory = 16
		dp.GPU = "Apple M2"
		dp.ScreenW = 2560
		dp.ScreenH = 1600
		dp.PixelRatio = 2.0
	case OSLinux:
		dp.OSVersion = "6.1"
		dp.Cores = 8
		dp.Memory = 16
		dp.GPU = "Mesa Intel(R) UHD Graphics 630"
		dp.ScreenW = 1920
		dp.ScreenH = 1080
		dp.PixelRatio = 1.0
	}

	return dp
}
