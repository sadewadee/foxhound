package fetch

import "testing"

func TestInterceptConfig_ShouldBlock_ResourceType(t *testing.T) {
	ic := NewInterceptConfig(
		map[ResourceType]bool{
			ResourceFont:       true,
			ResourceStylesheet: true,
		},
		nil,
	)

	tests := []struct {
		resourceType string
		url          string
		want         bool
	}{
		{"font", "https://example.com/font.woff2", true},
		{"stylesheet", "https://example.com/style.css", true},
		{"document", "https://example.com/page.html", false},
		{"script", "https://example.com/app.js", false},
		{"image", "https://example.com/logo.png", false},
	}

	for _, tc := range tests {
		got := ic.ShouldBlock(tc.resourceType, tc.url)
		if got != tc.want {
			t.Errorf("ShouldBlock(%q, %q) = %v, want %v", tc.resourceType, tc.url, got, tc.want)
		}
	}
}

func TestInterceptConfig_ShouldBlock_Domain(t *testing.T) {
	ic := NewInterceptConfig(
		nil,
		map[string]bool{
			"ads.example.com":    true,
			"tracking.com":       true,
			"analytics.third.io": true,
		},
	)

	tests := []struct {
		resourceType string
		url          string
		want         bool
	}{
		// Exact domain match.
		{"script", "https://ads.example.com/tracker.js", true},
		{"script", "https://tracking.com/pixel", true},
		{"image", "https://analytics.third.io/1x1.gif", true},
		// Subdomain match.
		{"script", "https://sub.tracking.com/beacon.js", true},
		{"script", "https://deep.sub.tracking.com/sdk.js", true},
		// Non-matching domains.
		{"script", "https://example.com/legit.js", false},
		{"document", "https://safe-site.com/", false},
		// Domain that contains the blocked domain as substring but is different.
		{"script", "https://nottracking.com/x.js", false},
	}

	for _, tc := range tests {
		got := ic.ShouldBlock(tc.resourceType, tc.url)
		if got != tc.want {
			t.Errorf("ShouldBlock(%q, %q) = %v, want %v", tc.resourceType, tc.url, got, tc.want)
		}
	}
}

func TestInterceptConfig_ShouldBlock_Combined(t *testing.T) {
	ic := NewInterceptConfig(
		map[ResourceType]bool{ResourceImage: true},
		map[string]bool{"blocked.com": true},
	)

	// Image from allowed domain: blocked by resource type.
	if !ic.ShouldBlock("image", "https://example.com/photo.jpg") {
		t.Error("expected image to be blocked by resource type")
	}

	// Script from blocked domain: blocked by domain.
	if !ic.ShouldBlock("script", "https://blocked.com/track.js") {
		t.Error("expected request to blocked domain to be blocked")
	}

	// Script from allowed domain: not blocked.
	if ic.ShouldBlock("script", "https://example.com/app.js") {
		t.Error("expected script from allowed domain to pass")
	}
}

func TestInterceptConfig_Nil(t *testing.T) {
	var ic *InterceptConfig
	if ic.ShouldBlock("font", "https://example.com/f.woff2") {
		t.Error("nil InterceptConfig should never block")
	}
	if ic.IsActive() {
		t.Error("nil InterceptConfig should not be active")
	}
}

func TestInterceptConfig_IsActive(t *testing.T) {
	empty := NewInterceptConfig(nil, nil)
	if empty.IsActive() {
		t.Error("empty config should not be active")
	}

	withResources := NewInterceptConfig(
		map[ResourceType]bool{ResourceFont: true},
		nil,
	)
	if !withResources.IsActive() {
		t.Error("config with resource types should be active")
	}

	withDomains := NewInterceptConfig(
		nil,
		map[string]bool{"blocked.com": true},
	)
	if !withDomains.IsActive() {
		t.Error("config with domains should be active")
	}
}

func TestExtractHostname(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://example.com/path", "example.com"},
		{"https://sub.example.com:8080/path", "sub.example.com"},
		{"http://localhost:3000/", "localhost"},
		{"not-a-url", ""},
	}

	for _, tc := range tests {
		got := extractHostname(tc.url)
		if got != tc.want {
			t.Errorf("extractHostname(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}
