package captcha_test

import (
	"testing"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/captcha"
)

func resp(body string) *foxhound.Response {
	return &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(body),
		URL:        "https://example.com/page",
	}
}

func TestDetectNone(t *testing.T) {
	r := resp("<html><body><h1>Hello World</h1></body></html>")
	got := captcha.Detect(r)
	if got.Type != captcha.CaptchaNone {
		t.Errorf("expected CaptchaNone, got %q", got.Type)
	}
}

func TestDetectCloudflareTurnstileByScript(t *testing.T) {
	body := `<html><head>
		<script src="https://challenges.cloudflare.com/turnstile/v0/api.js"></script>
	</head><body>
		<div class="cf-turnstile" data-sitekey="0x4AAAAAAABcd123xyz"></div>
	</body></html>`
	got := captcha.Detect(resp(body))
	if got.Type != captcha.CaptchaCloudflare {
		t.Errorf("expected CaptchaCloudflare, got %q", got.Type)
	}
}

func TestDetectCloudflareTurnstileByCfClass(t *testing.T) {
	body := `<html><body>
		<div class="cf-turnstile" data-sitekey="0x4AAAAAAAXxx999"></div>
	</body></html>`
	got := captcha.Detect(resp(body))
	if got.Type != captcha.CaptchaCloudflare {
		t.Errorf("expected CaptchaCloudflare, got %q", got.Type)
	}
}

func TestDetectCloudflareSiteKeyExtraction(t *testing.T) {
	body := `<html><body>
		<div class="cf-turnstile" data-sitekey="0x4AAAAAAAAbcDEFghij"></div>
	</body></html>`
	got := captcha.Detect(resp(body))
	if got.SiteKey != "0x4AAAAAAAAbcDEFghij" {
		t.Errorf("expected sitekey %q, got %q", "0x4AAAAAAAAbcDEFghij", got.SiteKey)
	}
}

func TestDetectRecaptchaByGoogleScript(t *testing.T) {
	body := `<html><head>
		<script src="https://www.google.com/recaptcha/api.js"></script>
	</head><body>
		<div class="g-recaptcha" data-sitekey="6LcEAbcDEFghijklmno"></div>
	</body></html>`
	got := captcha.Detect(resp(body))
	if got.Type != captcha.CaptchaRecaptcha {
		t.Errorf("expected CaptchaRecaptcha, got %q", got.Type)
	}
}

func TestDetectRecaptchaBygRecaptchaClass(t *testing.T) {
	body := `<html><body>
		<div class="g-recaptcha" data-sitekey="6LcTestKey123"></div>
	</body></html>`
	got := captcha.Detect(resp(body))
	if got.Type != captcha.CaptchaRecaptcha {
		t.Errorf("expected CaptchaRecaptcha, got %q", got.Type)
	}
}

func TestDetectRecaptchaByGrecaptchaJS(t *testing.T) {
	body := `<html><body><script>
		grecaptcha.execute('6LcJSKey999', {action: 'submit'});
	</script></body></html>`
	got := captcha.Detect(resp(body))
	if got.Type != captcha.CaptchaRecaptcha {
		t.Errorf("expected CaptchaRecaptcha, got %q", got.Type)
	}
}

func TestDetectRecaptchaSiteKeyExtraction(t *testing.T) {
	body := `<html><body>
		<div class="g-recaptcha" data-sitekey="6LcExtractMeXXX"></div>
	</body></html>`
	got := captcha.Detect(resp(body))
	if got.SiteKey != "6LcExtractMeXXX" {
		t.Errorf("expected sitekey %q, got %q", "6LcExtractMeXXX", got.SiteKey)
	}
}

func TestDetectHCaptchaByScript(t *testing.T) {
	body := `<html><head>
		<script src="https://js.hcaptcha.com/1/api.js"></script>
	</head><body>
		<div class="h-captcha" data-sitekey="abcd1234-ef56-7890-ghij-klmnopqrstuv"></div>
	</body></html>`
	got := captcha.Detect(resp(body))
	if got.Type != captcha.CaptchaHCaptcha {
		t.Errorf("expected CaptchaHCaptcha, got %q", got.Type)
	}
}

func TestDetectHCaptchaByClass(t *testing.T) {
	body := `<html><body>
		<div class="h-captcha" data-sitekey="hcap-key-xyz"></div>
	</body></html>`
	got := captcha.Detect(resp(body))
	if got.Type != captcha.CaptchaHCaptcha {
		t.Errorf("expected CaptchaHCaptcha, got %q", got.Type)
	}
}

func TestDetectHCaptchaSiteKeyExtraction(t *testing.T) {
	body := `<html><body>
		<div class="h-captcha" data-sitekey="hcap-sitekey-abc123"></div>
	</body></html>`
	got := captcha.Detect(resp(body))
	if got.SiteKey != "hcap-sitekey-abc123" {
		t.Errorf("expected sitekey %q, got %q", "hcap-sitekey-abc123", got.SiteKey)
	}
}

func TestDetectGeeTestByScript(t *testing.T) {
	body := `<html><head>
		<script src="https://static.geetest.com/gt.js"></script>
	</head><body>initGeetest({gt: 'abc123', challenge: 'xyz'});</body></html>`
	got := captcha.Detect(resp(body))
	if got.Type != captcha.CaptchaGeeTest {
		t.Errorf("expected CaptchaGeeTest, got %q", got.Type)
	}
}

func TestDetectGeeTestByGtCaptchaElement(t *testing.T) {
	body := `<html><body>
		<div id="gt_captcha" class="gt_captcha_holder"></div>
	</body></html>`
	got := captcha.Detect(resp(body))
	if got.Type != captcha.CaptchaGeeTest {
		t.Errorf("expected CaptchaGeeTest, got %q", got.Type)
	}
}

func TestDetectPageURLPreserved(t *testing.T) {
	r := &foxhound.Response{
		StatusCode: 403,
		Body:       []byte(`<div class="g-recaptcha" data-sitekey="abc"></div>`),
		URL:        "https://target.example.com/protected",
	}
	got := captcha.Detect(r)
	if got.PageURL != "https://target.example.com/protected" {
		t.Errorf("expected PageURL %q, got %q", "https://target.example.com/protected", got.PageURL)
	}
}

func TestDetectEmptyBodyReturnsNone(t *testing.T) {
	r := &foxhound.Response{Body: []byte{}, URL: "https://example.com"}
	got := captcha.Detect(r)
	if got.Type != captcha.CaptchaNone {
		t.Errorf("expected CaptchaNone for empty body, got %q", got.Type)
	}
}

func TestDetectNilBodyReturnsNone(t *testing.T) {
	r := &foxhound.Response{Body: nil, URL: "https://example.com"}
	got := captcha.Detect(r)
	if got.Type != captcha.CaptchaNone {
		t.Errorf("expected CaptchaNone for nil body, got %q", got.Type)
	}
}
