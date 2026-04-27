// Package presets bundles known-good (JA3, HTTP/2) fingerprint pairs that can
// be passed to fetch.WithJA3 / fetch.WithHTTP2Fingerprint.
//
// Pairs here are representative captures from real browsers — recent enough to
// blend with current traffic, but anti-bot vendors update their classifiers
// faster than this file is updated. For high-stakes targets, capture your own
// from https://tls.peet.ws and pass via fetch.WithJA3 directly.
//
// All values use the format documented by azuretls-client:
//
//	JA3:    SSLVersion,Ciphers,Extensions,EllipticCurves,EllipticCurvePointFormats
//	HTTP/2: <SETTINGS>|<WINDOW_UPDATE>|<PRIORITY>|<PSEUDO_HEADER>
package presets

// Bundle is a (JA3, HTTP/2) pair captured from a real browser. The Browser
// field is the azuretls family ("firefox", "chrome", "safari") and is what
// azuretls.Session.ApplyJa3 inherits its non-JA3 defaults from.
type Bundle struct {
	Name    string
	Browser string
	JA3     string
	HTTP2   string
}

// FirefoxLatest returns a representative Firefox 135 fingerprint pair.
func FirefoxLatest() Bundle {
	return Bundle{
		Name:    "firefox-135",
		Browser: "firefox",
		JA3:     "771,4865-4867-4866-49195-49199-52393-52392-49196-49200-49162-49161-49171-49172-156-157-47-53,0-23-65281-10-11-35-16-5-34-51-43-13-45-28-65037,29-23-24-25-256-257,0",
		HTTP2:   "1:65536;4:131072;5:16384|12517377|3:0:0:201,5:0:0:101,7:0:0:1,9:0:7:1,11:0:3:1,13:0:0:241|m,p,a,s",
	}
}

// ChromeLatest returns a representative Chrome 131 fingerprint pair.
func ChromeLatest() Bundle {
	return Bundle{
		Name:    "chrome-131",
		Browser: "chrome",
		JA3:     "771,4865-4866-4867-49195-49199-49196-49200-52393-52392-49171-49172-156-157-47-53,0-5-10-11-13-16-18-23-27-35-43-45-51-17513-65281-65037,29-23-24,0",
		HTTP2:   "1:65536;2:0;3:1000;4:6291456;6:262144|15663105|0|m,a,s,p",
	}
}

// SafariLatest returns a representative Safari 17 (iOS/macOS) fingerprint pair.
func SafariLatest() Bundle {
	return Bundle{
		Name:    "safari-17",
		Browser: "safari",
		JA3:     "771,4865-4866-4867-49196-49195-52393-49200-49199-52392-49162-49161-49172-49171-157-156-53-47-49160-49170-10,0-23-65281-10-11-16-5-13-18-51-45-43-27-21,29-23-24-25,0",
		HTTP2:   "2:0;3:100;4:2097152;8:1;9:1|10485760|0|m,s,p,a",
	}
}

// All returns every curated bundle. Useful as input to fetch.WithJA3Pool when
// you want the broadest possible JA3 spread per fetcher recycle.
func All() []Bundle {
	return []Bundle{FirefoxLatest(), ChromeLatest(), SafariLatest()}
}

// JA3Pool returns just the JA3 strings from the supplied bundles, in input
// order. Convenience for fetch.WithJA3Pool(presets.JA3Pool(presets.All())).
func JA3Pool(bundles []Bundle) []string {
	out := make([]string, 0, len(bundles))
	for _, b := range bundles {
		out = append(out, b.JA3)
	}
	return out
}
