package fetch

// ResourceType represents a browser resource type that can be blocked during
// page navigation. These correspond to the resource types reported by
// playwright's route.request.resource_type.
type ResourceType string

const (
	// ResourceFont matches font file requests (woff, woff2, ttf, otf, eot).
	ResourceFont ResourceType = "font"
	// ResourceImage matches image requests (png, jpg, gif, svg, webp, etc.).
	ResourceImage ResourceType = "image"
	// ResourceMedia matches audio/video requests (mp4, webm, ogg, mp3, etc.).
	ResourceMedia ResourceType = "media"
	// ResourceBeacon matches navigator.sendBeacon() requests.
	ResourceBeacon ResourceType = "beacon"
	// ResourceObject matches plugin requests (Flash, Java applets).
	ResourceObject ResourceType = "object"
	// ResourceImageSet matches <picture> source requests.
	ResourceImageSet ResourceType = "imageset"
	// ResourceTextTrack matches video subtitle/caption track requests.
	ResourceTextTrack ResourceType = "texttrack"
	// ResourceWebSocket matches WebSocket connection requests.
	ResourceWebSocket ResourceType = "websocket"
	// ResourceCSPReport matches Content-Security-Policy violation reports.
	ResourceCSPReport ResourceType = "csp_report"
	// ResourceStylesheet matches CSS stylesheet requests.
	ResourceStylesheet ResourceType = "stylesheet"
)

// DefaultBlockedResources returns the standard set of resource types to block
// for speed optimization. Blocking these typically cuts page load time by
// 30-70% for content-only scraping.
func DefaultBlockedResources() map[ResourceType]bool {
	return map[ResourceType]bool{
		ResourceFont:       true,
		ResourceImage:      true,
		ResourceMedia:      true,
		ResourceBeacon:     true,
		ResourceObject:     true,
		ResourceImageSet:   true,
		ResourceTextTrack:  true,
		ResourceWebSocket:  true,
		ResourceCSPReport:  true,
		ResourceStylesheet: true,
	}
}

// ContentOnlyResources returns a minimal set of resources to block that
// preserves page layout while eliminating heavy binary assets.
// Stylesheets are NOT blocked so the page renders correctly for
// layout-dependent extraction.
func ContentOnlyResources() map[ResourceType]bool {
	return map[ResourceType]bool{
		ResourceFont:      true,
		ResourceImage:     true,
		ResourceMedia:     true,
		ResourceBeacon:    true,
		ResourceObject:    true,
		ResourceImageSet:  true,
		ResourceTextTrack: true,
		ResourceWebSocket: true,
		ResourceCSPReport: true,
	}
}
