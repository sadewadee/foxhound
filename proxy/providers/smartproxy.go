package providers

import (
	"context"
	"fmt"
	"strings"

	"github.com/foxhound-scraper/foxhound/proxy"
)

const smartproxyHost = "gate.smartproxy.com"
const smartproxyPort = "7000"

// Smartproxy provides proxies from the Smartproxy residential service.
type Smartproxy struct {
	username string
	password string
	country  string // ISO 3166-1 alpha-2 country code
}

// NewSmartproxy creates a Smartproxy provider.
// username and password are the Smartproxy account credentials.
// country is an ISO 3166-1 alpha-2 country code (e.g. "US", "AU").
func NewSmartproxy(username, password, country string) *Smartproxy {
	return &Smartproxy{
		username: username,
		password: password,
		country:  country,
	}
}

// Proxies returns a single proxy configured for the Smartproxy gateway.
// When a country is specified the username encodes the country code:
//
//	user.{username}-cc-{country}
//
// If no country is provided the bare username is used.
func (s *Smartproxy) Proxies(_ context.Context) ([]*proxy.Proxy, error) {
	proxyUsername := s.username
	if s.country != "" {
		proxyUsername = fmt.Sprintf("user.%s-cc-%s", s.username, strings.ToLower(s.country))
	}

	rawURL := fmt.Sprintf("http://%s:%s@%s:%s",
		proxyUsername, s.password, smartproxyHost, smartproxyPort)

	p := &proxy.Proxy{
		URL:      rawURL,
		Protocol: "http",
		Host:     smartproxyHost,
		Port:     smartproxyPort,
		Username: proxyUsername,
		Password: s.password,
		Country:  s.country,
	}
	return []*proxy.Proxy{p}, nil
}
