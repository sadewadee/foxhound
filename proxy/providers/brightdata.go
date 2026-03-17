// Package providers contains third-party proxy provider adapters that implement
// the proxy.Provider interface.
package providers

import (
	"context"
	"fmt"
	"strings"

	"github.com/foxhound-scraper/foxhound/proxy"
)

const brightDataHost = "brd.superproxy.io"
const brightDataPort = "22225"

// BrightData provides proxies from the Bright Data (Luminati) service.
//
// The apiKey field carries the full credential string in the format:
//
//	customer-{customer_id}-zone-{zone}:password
//
// or just:
//
//	customer-{customer_id}-zone-{zone}
type BrightData struct {
	apiKey  string
	product string // "residential", "datacenter", "isp"
	country string // ISO 3166-1 alpha-2 country code
}

// NewBrightData creates a BrightData provider.
// apiKey is the full credential string (username or username:password).
// product is one of "residential", "datacenter", or "isp".
// country is an ISO 3166-1 alpha-2 country code (e.g. "US", "DE").
func NewBrightData(apiKey, product, country string) *BrightData {
	return &BrightData{
		apiKey:  apiKey,
		product: product,
		country: country,
	}
}

// Proxies returns a single proxy configured for the Bright Data gateway.
// Bright Data exposes a single gateway endpoint; rotation is handled server-side.
func (b *BrightData) Proxies(_ context.Context) ([]*proxy.Proxy, error) {
	username, password := splitCredentials(b.apiKey)

	rawURL := fmt.Sprintf("http://%s:%s@%s:%s",
		username, password, brightDataHost, brightDataPort)

	p := &proxy.Proxy{
		URL:      rawURL,
		Protocol: "http",
		Host:     brightDataHost,
		Port:     brightDataPort,
		Username: username,
		Password: password,
		Country:  b.country,
	}
	return []*proxy.Proxy{p}, nil
}

// splitCredentials splits "user:pass" into its components.
// If there is no ":" the entire string is the username with an empty password.
func splitCredentials(apiKey string) (username, password string) {
	parts := strings.SplitN(apiKey, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return apiKey, ""
}
