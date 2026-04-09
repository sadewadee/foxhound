package fetch

import (
	"regexp"

	foxhound "github.com/sadewadee/foxhound"
)

// CaptureXHRMetaKey is the Job.Meta key under which Trail.CaptureXHR stores
// per-job XHR capture URL patterns. The walker passes the job through to the
// camoufox fetcher unchanged; the fetcher merges these patterns with any
// fetcher-global patterns configured via WithCaptureXHR.
const CaptureXHRMetaKey = "_foxhound_capture_xhr"

// capturePatternsFromJob extracts compiled URL patterns from the job's Meta.
// The Trail builder stores them as []string; this helper compiles each entry
// to a regexp, skipping any that fail to compile (logged at debug level by
// the caller's surrounding code path).
func capturePatternsFromJob(job *foxhound.Job) []*regexp.Regexp {
	if job == nil || job.Meta == nil {
		return nil
	}
	raw, ok := job.Meta[CaptureXHRMetaKey]
	if !ok {
		return nil
	}
	patterns, ok := raw.([]string)
	if !ok {
		return nil
	}
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		out = append(out, re)
	}
	return out
}

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
