package providers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sadewadee/foxhound/proxy/providers"
)

// --- BrightData ---

func TestBrightDataProxiesReturnsProxies(t *testing.T) {
	bd := providers.NewBrightData("customer-abc123-zone-residential_proxy", "residential", "US")
	proxies, err := bd.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proxies) == 0 {
		t.Fatal("expected at least one proxy from BrightData")
	}
}

func TestBrightDataProxyURLFormat(t *testing.T) {
	bd := providers.NewBrightData("customer-myid-zone-res_proxy", "residential", "US")
	proxies, err := bd.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proxies) == 0 {
		t.Fatal("expected at least one proxy")
	}
	p := proxies[0]
	if !strings.Contains(p.URL, "brd.superproxy.io") {
		t.Errorf("expected URL to contain brd.superproxy.io, got %q", p.URL)
	}
	if p.Protocol != "http" {
		t.Errorf("expected protocol http, got %q", p.Protocol)
	}
	if p.Host != "brd.superproxy.io" {
		t.Errorf("expected host brd.superproxy.io, got %q", p.Host)
	}
	if p.Port != "22225" {
		t.Errorf("expected port 22225, got %q", p.Port)
	}
}

func TestBrightDataProxyHasCredentials(t *testing.T) {
	bd := providers.NewBrightData("customer-myid-zone-res_proxy:secretpass", "residential", "US")
	proxies, err := bd.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proxies) == 0 {
		t.Fatal("expected at least one proxy")
	}
	p := proxies[0]
	if p.Username == "" {
		t.Error("expected non-empty username")
	}
	if p.Password == "" {
		t.Error("expected non-empty password")
	}
}

func TestBrightDataProxyCountrySet(t *testing.T) {
	bd := providers.NewBrightData("customer-myid-zone-res_proxy", "residential", "DE")
	proxies, err := bd.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proxies) == 0 {
		t.Fatal("expected at least one proxy")
	}
	if proxies[0].Country != "DE" {
		t.Errorf("expected country DE, got %q", proxies[0].Country)
	}
}

// --- Oxylabs ---

func TestOxylabsProxiesReturnsProxies(t *testing.T) {
	ox := providers.NewOxylabs("myuser", "mypass", "residential", "US")
	proxies, err := ox.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proxies) == 0 {
		t.Fatal("expected at least one proxy from Oxylabs")
	}
}

func TestOxylabsProxyURLFormat(t *testing.T) {
	ox := providers.NewOxylabs("myuser", "mypass", "residential", "US")
	proxies, err := ox.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := proxies[0]
	if !strings.Contains(p.URL, "pr.oxylabs.io") {
		t.Errorf("expected URL to contain pr.oxylabs.io, got %q", p.URL)
	}
	if p.Protocol != "http" {
		t.Errorf("expected protocol http, got %q", p.Protocol)
	}
	if p.Host != "pr.oxylabs.io" {
		t.Errorf("expected host pr.oxylabs.io, got %q", p.Host)
	}
	if p.Port != "7777" {
		t.Errorf("expected port 7777, got %q", p.Port)
	}
}

func TestOxylabsProxyUsernameContainsCustomerPrefix(t *testing.T) {
	ox := providers.NewOxylabs("john", "secret", "residential", "FR")
	proxies, err := ox.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := proxies[0]
	if !strings.HasPrefix(p.Username, "customer-") {
		t.Errorf("expected username to start with customer-, got %q", p.Username)
	}
}

func TestOxylabsProxyCountryInUsername(t *testing.T) {
	ox := providers.NewOxylabs("john", "secret", "residential", "JP")
	proxies, err := ox.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := proxies[0]
	if !strings.Contains(p.Username, "jp") && !strings.Contains(p.Username, "JP") {
		t.Errorf("expected country code in username, got %q", p.Username)
	}
}

func TestOxylabsProxyCountryFieldSet(t *testing.T) {
	ox := providers.NewOxylabs("john", "secret", "residential", "BR")
	proxies, err := ox.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proxies[0].Country != "BR" {
		t.Errorf("expected country BR, got %q", proxies[0].Country)
	}
}

// --- Smartproxy ---

func TestSmartproxyProxiesReturnsProxies(t *testing.T) {
	sp := providers.NewSmartproxy("spuser", "sppass", "US")
	proxies, err := sp.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proxies) == 0 {
		t.Fatal("expected at least one proxy from Smartproxy")
	}
}

func TestSmartproxyProxyURLFormat(t *testing.T) {
	sp := providers.NewSmartproxy("spuser", "sppass", "US")
	proxies, err := sp.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := proxies[0]
	if !strings.Contains(p.URL, "gate.smartproxy.com") {
		t.Errorf("expected URL to contain gate.smartproxy.com, got %q", p.URL)
	}
	if p.Protocol != "http" {
		t.Errorf("expected protocol http, got %q", p.Protocol)
	}
}

func TestSmartproxyProxyHasCredentials(t *testing.T) {
	sp := providers.NewSmartproxy("spuser", "sppass", "GB")
	proxies, err := sp.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := proxies[0]
	if p.Username == "" {
		t.Error("expected non-empty username")
	}
	if p.Password != "sppass" {
		t.Errorf("expected password %q, got %q", "sppass", p.Password)
	}
}

func TestSmartproxyProxyCountryFieldSet(t *testing.T) {
	sp := providers.NewSmartproxy("spuser", "sppass", "AU")
	proxies, err := sp.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proxies[0].Country != "AU" {
		t.Errorf("expected country AU, got %q", proxies[0].Country)
	}
}

func TestSmartproxyProxyPortIsValid(t *testing.T) {
	sp := providers.NewSmartproxy("spuser", "sppass", "US")
	proxies, err := sp.Proxies(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := proxies[0]
	if p.Port == "" {
		t.Error("expected non-empty port")
	}
}
