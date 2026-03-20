package fetch

import "regexp"

// CapturedExchange holds a captured XHR/fetch request-response pair.
type CapturedExchange struct {
	RequestURL    string            `json:"request_url"`
	RequestMethod string            `json:"request_method"`
	Status        int               `json:"status"`
	Headers       map[string]string `json:"headers,omitempty"`
	Body          []byte            `json:"body,omitempty"`
}

// CapturePattern defines a URL pattern to capture.
type CapturePattern struct {
	Pattern *regexp.Regexp
}

// WithCaptureXHR configures URL patterns for XHR/fetch response capture.
// Captured responses are available in Response.CapturedXHR after fetch.
func WithCaptureXHR(patterns ...*regexp.Regexp) CamoufoxOption {
	return func(f *CamoufoxFetcher) {
		f.capturePatterns = patterns
	}
}
