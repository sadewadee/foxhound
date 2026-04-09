package engine

import (
	"testing"
)

func TestHunt_WithAdaptive_SetsExtractor(t *testing.T) {
	h := NewHunt(HuntConfig{Name: "t"})
	if h.AdaptiveExtractor() != nil {
		t.Errorf("default AdaptiveExtractor should be nil")
	}
	h.WithAdaptive("")
	if h.AdaptiveExtractor() == nil {
		t.Errorf("after WithAdaptive, AdaptiveExtractor should be non-nil")
	}
}

func TestTrail_Adaptive_RecordsRegistrationOnJobs(t *testing.T) {
	tr := NewTrail("t").
		Navigate("https://example.test/page").
		Click("button.go").
		Adaptive("title", "h1.product").
		Adaptive("price", "span.price")

	jobs := tr.ToJobs()
	if len(jobs) == 0 {
		t.Fatal("expected at least one job")
	}
	// Find job with the target URL (warmup may be prepended).
	var target *jobMetaProbe
	for _, j := range jobs {
		if j.URL == "https://example.test/page" {
			target = &jobMetaProbe{meta: j.Meta}
			break
		}
	}
	if target == nil {
		t.Fatal("target job not found in produced jobs")
	}
	raw, ok := target.meta[trailAdaptiveMetaKey]
	if !ok {
		t.Fatal("trail adaptive meta key missing on job")
	}
	regs, ok := raw.([][2]string)
	if !ok {
		t.Fatalf("adaptive meta has wrong type: %T", raw)
	}
	if len(regs) != 2 {
		t.Errorf("expected 2 registrations, got %d", len(regs))
	}
	if regs[0] != ([2]string{"title", "h1.product"}) {
		t.Errorf("first reg = %v", regs[0])
	}
	if regs[1] != ([2]string{"price", "span.price"}) {
		t.Errorf("second reg = %v", regs[1])
	}
}

type jobMetaProbe struct{ meta map[string]any }
