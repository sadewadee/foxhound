package parse_test

import (
	"testing"

	"github.com/sadewadee/foxhound/parse"
)

func TestDocument_AdaptiveAccessors(t *testing.T) {
	doc := newAdaptiveDoc(t, adaptiveOriginalHTML)
	if doc.Adaptive() != nil {
		t.Errorf("default Adaptive() should be nil")
	}
	ae := parse.NewAdaptiveExtractor("")
	doc.SetAdaptive(ae)
	if doc.Adaptive() != ae {
		t.Errorf("Adaptive() returned %v, want %v", doc.Adaptive(), ae)
	}
}
