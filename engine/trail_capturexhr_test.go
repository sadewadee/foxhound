package engine

import (
	"testing"

	foxhound "github.com/sadewadee/foxhound"
)

func TestTrail_CaptureXHR_AttachesMetaAndForcesBrowser(t *testing.T) {
	trail := NewTrail("xhr").
		NoWarmup().
		Navigate("https://example.com/search").
		CaptureXHR(`/api/results`).
		CaptureXHR(`/graphql`)

	jobs := trail.ToJobs()
	if len(jobs) == 0 {
		t.Fatal("expected at least one job")
	}
	for _, j := range jobs {
		if j.FetchMode != foxhound.FetchBrowser {
			t.Fatalf("CaptureXHR must force FetchBrowser, got %v", j.FetchMode)
		}
		raw, ok := j.Meta[captureXHRMetaKey]
		if !ok {
			t.Fatal("expected captureXHR meta on job")
		}
		patterns, ok := raw.([]string)
		if !ok {
			t.Fatalf("meta wrong type: %T", raw)
		}
		if len(patterns) != 2 || patterns[0] != `/api/results` || patterns[1] != `/graphql` {
			t.Fatalf("unexpected patterns: %v", patterns)
		}
	}
}
