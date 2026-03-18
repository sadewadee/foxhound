package fetch

import "testing"

func TestDefaultBlockedResources(t *testing.T) {
	blocked := DefaultBlockedResources()
	if len(blocked) != 10 {
		t.Errorf("expected 10 blocked resource types, got %d", len(blocked))
	}

	expected := []ResourceType{
		ResourceFont, ResourceImage, ResourceMedia, ResourceBeacon,
		ResourceObject, ResourceImageSet, ResourceTextTrack,
		ResourceWebSocket, ResourceCSPReport, ResourceStylesheet,
	}
	for _, rt := range expected {
		if !blocked[rt] {
			t.Errorf("expected %q to be in DefaultBlockedResources", rt)
		}
	}
}

func TestContentOnlyResources(t *testing.T) {
	blocked := ContentOnlyResources()
	if len(blocked) != 9 {
		t.Errorf("expected 9 content-only resource types, got %d", len(blocked))
	}

	// Stylesheet should NOT be blocked in content-only mode.
	if blocked[ResourceStylesheet] {
		t.Error("stylesheet should not be blocked in ContentOnlyResources")
	}

	// These should still be blocked.
	mustBlock := []ResourceType{
		ResourceFont, ResourceImage, ResourceMedia, ResourceBeacon,
	}
	for _, rt := range mustBlock {
		if !blocked[rt] {
			t.Errorf("expected %q to be blocked in ContentOnlyResources", rt)
		}
	}
}

func TestResourceTypeString(t *testing.T) {
	tests := []struct {
		rt   ResourceType
		want string
	}{
		{ResourceFont, "font"},
		{ResourceStylesheet, "stylesheet"},
		{ResourceCSPReport, "csp_report"},
	}
	for _, tc := range tests {
		if string(tc.rt) != tc.want {
			t.Errorf("ResourceType(%q) = %q, want %q", tc.rt, string(tc.rt), tc.want)
		}
	}
}
