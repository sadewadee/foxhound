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
	// CaptchaSoftBlock is a 200 OK response whose body signals "access denied".
	CaptchaSoftBlock CaptchaType = "soft_block"
	// CaptchaEmptyTrap is a 200 OK response with suspiciously minimal content.
	CaptchaEmptyTrap CaptchaType = "empty_trap"
	// CaptchaLoginWall is a redirect to a login page used to gate content.
	CaptchaLoginWall CaptchaType = "login_wall"
	// CaptchaJSChallenge is a JS-only challenge page (e.g. Akamai) that has no
	// CAPTCHA widget but blocks until the browser executes challenge JS.
	CaptchaJSChallenge CaptchaType = "js_challenge"
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

	case isCloudflareJSChallenge(lower):
		// Cloudflare JS challenge is not a traditional CAPTCHA but it is a
		// block that requires challenge resolution before content is accessible.
		result.Type = CaptchaCloudflare

	case isHCaptcha(lower):
		result.Type = CaptchaHCaptcha
		result.SiteKey = extractSiteKey(body)

	case isRecaptcha(lower):
		result.Type = CaptchaRecaptcha
		result.SiteKey = extractSiteKey(body)

	case isGeeTest(lower):
		result.Type = CaptchaGeeTest
	}

	// Return early when a known CAPTCHA widget was already identified.
	if result.Type != CaptchaNone {
		return result
	}

	// --- Content-aware block detection ---

	// JS challenge (Akamai-style): no CAPTCHA widget but JS challenge page.
	if isJSChallenge(lower) {
		result.Type = CaptchaJSChallenge
		return result
	}

	// Soft block: 200 OK but body explicitly says "access denied" in a small page.
	if resp.StatusCode == 200 {
		softBlockPatterns := []string{"access denied", "permission denied", "blocked", "forbidden"}
		for _, p := range softBlockPatterns {
			if strings.Contains(lower, p) && len(resp.Body) < 10000 {
				result.Type = CaptchaSoftBlock
				return result
			}
		}
	}

	// Empty trap: 200 OK but body is suspiciously small and lacks <html.
	if resp.StatusCode == 200 && len(resp.Body) < 500 && !strings.Contains(lower, "<html") {
		result.Type = CaptchaEmptyTrap
		return result
	}

	return result
}

// isTurnstile returns true when the page contains Cloudflare Turnstile markers.
func isTurnstile(lower string) bool {
	return strings.Contains(lower, "challenges.cloudflare.com/turnstile") ||
		strings.Contains(lower, "cf-turnstile")
}

// isCloudflareJSChallenge returns true when the page is a Cloudflare JS
// challenge interstitial ("Checking your browser" / "Just a moment").
// This is distinct from Turnstile — it requires no widget interaction; the
// browser solves it automatically via JS execution.
func isCloudflareJSChallenge(lower string) bool {
	return (strings.Contains(lower, "checking your browser") ||
		strings.Contains(lower, "just a moment")) &&
		strings.Contains(lower, "cloudflare")
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

// isJSChallenge returns true when the page is a JavaScript-only challenge
// (e.g. Akamai Bot Manager) that blocks access until the browser executes
// challenge code but presents no traditional CAPTCHA widget.
func isJSChallenge(lower string) bool {
	return strings.Contains(lower, "browser verification") ||
		strings.Contains(lower, "security challenge") ||
		(strings.Contains(lower, "akamai") && strings.Contains(lower, "challenge"))
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
