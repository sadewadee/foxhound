// Package captcha provides CAPTCHA detection and solving for the Foxhound
// scraping framework.
package captcha

import (
	"regexp"
	"strings"

	foxhound "github.com/sadewadee/foxhound"
)

// CaptchaType identifies the type of CAPTCHA detected.
type CaptchaType string

const (
	// CaptchaNone means no CAPTCHA was detected.
	CaptchaNone CaptchaType = ""
	// CaptchaCloudflare is a Cloudflare Turnstile challenge.
	CaptchaCloudflare CaptchaType = "cloudflare_turnstile"
	// CaptchaRecaptcha is a Google reCAPTCHA challenge.
	CaptchaRecaptcha CaptchaType = "recaptcha"
	// CaptchaHCaptcha is an hCaptcha challenge.
	CaptchaHCaptcha CaptchaType = "hcaptcha"
	// CaptchaGeeTest is a GeeTest challenge.
	CaptchaGeeTest CaptchaType = "geetest"
	// CaptchaUnknown is an unrecognised CAPTCHA challenge.
	CaptchaUnknown CaptchaType = "unknown"
)

// DetectResult describes a detected CAPTCHA.
type DetectResult struct {
	// Type is the kind of CAPTCHA found.
	Type CaptchaType
	// SiteKey is the site key extracted from the page (may be empty).
	SiteKey string
	// PageURL is the URL of the page that triggered detection.
	PageURL string
}

// siteKeyRe matches data-sitekey="<value>" or data-sitekey='<value>'.
var siteKeyRe = regexp.MustCompile(`data-sitekey=["']([^"']+)["']`)

// Detect analyses a Response to determine whether it contains a CAPTCHA
// challenge and, if so, which kind. It also attempts to extract the site key.
func Detect(resp *foxhound.Response) *DetectResult {
	result := &DetectResult{
		Type:    CaptchaNone,
		PageURL: resp.URL,
	}

	if len(resp.Body) == 0 {
		return result
	}

	body := string(resp.Body)
	lower := strings.ToLower(body)

	switch {
	case isTurnstile(lower):
		result.Type = CaptchaCloudflare
		result.SiteKey = extractSiteKey(body)

	case isRecaptcha(lower):
		result.Type = CaptchaRecaptcha
		result.SiteKey = extractSiteKey(body)

	case isHCaptcha(lower):
		result.Type = CaptchaHCaptcha
		result.SiteKey = extractSiteKey(body)

	case isGeeTest(lower):
		result.Type = CaptchaGeeTest
	}

	return result
}

// isTurnstile returns true when the page contains Cloudflare Turnstile markers.
func isTurnstile(lower string) bool {
	return strings.Contains(lower, "challenges.cloudflare.com/turnstile") ||
		strings.Contains(lower, "cf-turnstile")
}

// isRecaptcha returns true when the page contains Google reCAPTCHA markers.
func isRecaptcha(lower string) bool {
	return strings.Contains(lower, "google.com/recaptcha") ||
		strings.Contains(lower, "g-recaptcha") ||
		strings.Contains(lower, "grecaptcha")
}

// isHCaptcha returns true when the page contains hCaptcha markers.
func isHCaptcha(lower string) bool {
	return strings.Contains(lower, "hcaptcha.com") ||
		strings.Contains(lower, "h-captcha")
}

// isGeeTest returns true when the page contains GeeTest markers.
func isGeeTest(lower string) bool {
	return strings.Contains(lower, "geetest.com") ||
		strings.Contains(lower, "gt_captcha")
}

// extractSiteKey attempts to pull the value of data-sitekey from the page body.
// Returns an empty string if none is found.
func extractSiteKey(body string) string {
	m := siteKeyRe.FindStringSubmatch(body)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}
