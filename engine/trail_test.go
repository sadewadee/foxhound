package engine

import (
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

func TestTrail_WarmupPrependsHomepage(t *testing.T) {
	trail := NewTrail("warmup-test").
		Navigate("https://www.google.com/maps/search/yoga/").
		Wait("div[role='feed']", 5*time.Second)

	jobs := trail.ToJobs()
	if len(jobs) != 2 {
		t.Fatalf("ToJobs len = %d, want 2", len(jobs))
	}
	if jobs[0].URL != "https://www.google.com/" {
		t.Fatalf("warmup URL = %q, want %q", jobs[0].URL, "https://www.google.com/")
	}
	if jobs[0].FetchMode != foxhound.FetchBrowser {
		t.Fatalf("warmup FetchMode = %v, want FetchBrowser", jobs[0].FetchMode)
	}
	if jobs[1].URL != "https://www.google.com/maps/search/yoga/" {
		t.Fatalf("target URL = %q, want %q", jobs[1].URL, "https://www.google.com/maps/search/yoga/")
	}
}

func TestTrail_WarmupSkippedWithNoWarmup(t *testing.T) {
	trail := NewTrail("no-warmup").
		NoWarmup().
		Navigate("https://example.com/deep/path").
		Click("button.submit")

	jobs := trail.ToJobs()
	if len(jobs) != 1 {
		t.Fatalf("ToJobs len = %d, want 1", len(jobs))
	}
	if jobs[0].URL != "https://example.com/deep/path" {
		t.Fatalf("URL = %q, want target URL", jobs[0].URL)
	}
}

func TestTrail_WarmupSkippedWhenAlreadyHomepage(t *testing.T) {
	trail := NewTrail("homepage").
		Navigate("https://example.com/").
		Click("button")

	jobs := trail.ToJobs()
	if len(jobs) != 1 {
		t.Fatalf("ToJobs len = %d, want 1 (already homepage)", len(jobs))
	}
}

func TestTrail_WarmupSkippedForStaticTrails(t *testing.T) {
	trail := NewTrail("static").
		Navigate("https://example.com/page")

	jobs := trail.ToJobs()
	if len(jobs) != 1 {
		t.Fatalf("ToJobs len = %d, want 1 (static trail, no browser steps)", len(jobs))
	}
}

func TestTrail_WarmupSkippedNoNavigate(t *testing.T) {
	trail := NewTrail("no-nav").
		Click("button")

	jobs := trail.ToJobs()
	if len(jobs) != 0 {
		t.Fatalf("ToJobs len = %d, want 0 (no navigate)", len(jobs))
	}
}

func TestTrail_HomepageURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://www.google.com/maps/search/yoga/", "https://www.google.com/"},
		{"http://example.com:8080/path/to/page", "http://example.com:8080/"},
		{"https://shop.example.co.uk/products?id=1", "https://shop.example.co.uk/"},
		{"not-a-url", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := homepageURL(tt.input)
		if got != tt.want {
			t.Errorf("homepageURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTrail_Collect_ToJobs(t *testing.T) {
	trail := NewTrail("test-collect").
		NoWarmup().
		Navigate("https://example.com/search").
		Wait("div.results", 5*time.Second).
		InfiniteScroll(10).
		Collect("a.result-link", "href")

	jobs := trail.ToJobs()
	if len(jobs) != 1 {
		t.Fatalf("ToJobs len = %d, want 1", len(jobs))
	}

	job := jobs[0]
	if job.FetchMode != foxhound.FetchBrowser {
		t.Fatalf("FetchMode = %v, want FetchBrowser", job.FetchMode)
	}

	// Last step should be Collect (implemented as Evaluate)
	lastStep := job.Steps[len(job.Steps)-1]
	if lastStep.Action != foxhound.JobStepEvaluate {
		t.Fatalf("last step Action = %d, want JobStepEvaluate (%d)", lastStep.Action, foxhound.JobStepEvaluate)
	}
	if lastStep.Selector != "a.result-link" {
		t.Fatalf("Selector = %q, want %q", lastStep.Selector, "a.result-link")
	}
	if lastStep.Value != "href" {
		t.Fatalf("Value = %q, want %q", lastStep.Value, "href")
	}
}
