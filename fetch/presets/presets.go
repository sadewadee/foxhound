// Package presets ships a single curated Firefox JA3 fingerprint that the
// stealth fetcher applies automatically when an identity profile is configured.
//
// Foxhound is Camoufox-only at the browser layer (Camoufox is a Firefox fork,
// see CLAUDE.md "Camoufox only" principle). The static fetcher mirrors that
// stance: only Firefox is impersonated at the TLS layer, keeping every layer
// internally consistent — UA, header order, JA3, and Camoufox C++-level
// fingerprint all agree on Firefox.
//
// Direct callers should not need this package — fetch.WithIdentity reads from
// it internally. It is exported only so advanced consumers can capture their
// own Firefox JA3 from https://tls.peet.ws and pass it via fetch.WithJA3 when
// the curated value lags real Firefox.
//
// HTTP/2 fingerprint is intentionally NOT exposed. azuretls.Session.ApplyHTTP2
// bypasses browser-aware HTTP/2 defaults (defaultHeaderPriorities,
// defaultStreamPriorities) and produces a half-Firefox/half-generic
// fingerprint that deep validators reject — see issue #41. Letting azuretls's
// own initHTTP2(browser) populate the HTTP/2 layer keeps it consistent with
// the JA3-derived TLS layer.
//
// JA3 format: SSLVersion,Ciphers,Extensions,EllipticCurves,EllipticCurvePointFormats
package presets

// Bundle is a captured-from-real-Firefox fingerprint. The Browser field is the
// azuretls family ("firefox") and is the navigator argument azuretls.Session.
// ApplyJa3 inherits its non-JA3 defaults from.
type Bundle struct {
	Name    string
	Browser string
	JA3     string
}

// FirefoxLatest returns the curated Firefox 135 JA3 fingerprint. Foxhound's
// static fetcher applies this automatically when WithIdentity is set with a
// Firefox profile (the only browser foxhound supports). Refresh from
// https://tls.peet.ws when newer Firefox releases drift the ClientHello.
func FirefoxLatest() Bundle {
	return Bundle{
		Name:    "firefox-135",
		Browser: "firefox",
		JA3:     "771,4865-4867-4866-49195-49199-52393-52392-49196-49200-49162-49161-49171-49172-156-157-47-53,0-23-65281-10-11-35-16-5-34-51-43-13-45-28-65037,29-23-24-25-256-257,0",
	}
}
