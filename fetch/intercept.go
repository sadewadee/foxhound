package fetch

import (
	"net/url"
	"strings"
)

// InterceptConfig configures what to block during browser navigation.
// It combines resource-type filtering and domain-level blocking into a
// single check that is called for every network request via the browser's
// route handler.
type InterceptConfig struct {
	// BlockedResourceTypes maps resource types (e.g. "font", "stylesheet")
	// to true for those that should be aborted.
	BlockedResourceTypes map[ResourceType]bool
	// BlockedDomains maps domain names to true. Subdomains are also matched:
	// "example.com" blocks "sub.example.com".
	BlockedDomains map[string]bool
}

// NewInterceptConfig creates an InterceptConfig with the given resource types
// and domains to block.
func NewInterceptConfig(resourceTypes map[ResourceType]bool, domains map[string]bool) *InterceptConfig {
	if resourceTypes == nil {
		resourceTypes = make(map[ResourceType]bool)
	}
	if domains == nil {
		domains = make(map[string]bool)
	}
	return &InterceptConfig{
		BlockedResourceTypes: resourceTypes,
		BlockedDomains:       domains,
	}
}

// ShouldBlock returns true if the given resource type and URL should be
// blocked based on this configuration.
func (ic *InterceptConfig) ShouldBlock(resourceType string, requestURL string) bool {
	if ic == nil {
		return false
	}

	// Check resource type.
	if ic.BlockedResourceTypes[ResourceType(resourceType)] {
		return true
	}

	// Check domain blocking.
	if len(ic.BlockedDomains) > 0 {
		hostname := extractHostname(requestURL)
		if hostname != "" {
			for domain := range ic.BlockedDomains {
				if hostname == domain || strings.HasSuffix(hostname, "."+domain) {
					return true
				}
			}
		}
	}

	return false
}

// IsActive returns true if any blocking rules are configured.
func (ic *InterceptConfig) IsActive() bool {
	if ic == nil {
		return false
	}
	return len(ic.BlockedResourceTypes) > 0 || len(ic.BlockedDomains) > 0
}

// extractHostname returns the hostname from a URL string, or empty string on
// parse failure.
func extractHostname(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
