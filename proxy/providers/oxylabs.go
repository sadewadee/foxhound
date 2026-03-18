package providers

import (
	"context"
	"fmt"
	"strings"

	"github.com/sadewadee/foxhound/proxy"
)

const oxylabsHost = "pr.oxylabs.io"
const oxylabsPort = "7777"

// Oxylabs provides proxies from the Oxylabs residential/datacenter service.
type Oxylabs struct {
	username string
	password string
	product  string // "residential", "datacenter", "isp"
	country  string // ISO 3166-1 alpha-2 country code
}

// NewOxylabs creates an Oxylabs provider.
// username and password are the Oxylabs account credentials.
// product is one of "residential", "datacenter", or "isp".
// country is an ISO 3166-1 alpha-2 country code (e.g. "US", "DE").
func NewOxylabs(username, password, product, country string) *Oxylabs {
	return &Oxylabs{
		username: username,
		password: password,
		product:  product,
		country:  country,
	}
}

// Proxies returns a single proxy configured for the Oxylabs gateway.
// Oxylabs uses a username format that encodes customer ID and country:
//
//	customer-{username}-cc-{country}
func (o *Oxylabs) Proxies(_ context.Context) ([]*proxy.Proxy, error) {
	// Oxylabs session username format encodes the country in lowercase.
	proxyUsername := fmt.Sprintf("customer-%s-cc-%s", o.username, strings.ToLower(o.country))

	rawURL := fmt.Sprintf("http://%s:%s@%s:%s",
		proxyUsername, o.password, oxylabsHost, oxylabsPort)

	p := &proxy.Proxy{
		URL:      rawURL,
		Protocol: "http",
		Host:     oxylabsHost,
		Port:     oxylabsPort,
		Username: proxyUsername,
		Password: o.password,
		Country:  o.country,
	}
	return []*proxy.Proxy{p}, nil
}
