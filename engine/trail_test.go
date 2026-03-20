package engine

import (
	"testing"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

func TestTrail_Collect_ToJobs(t *testing.T) {
	trail := NewTrail("test-collect").
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
