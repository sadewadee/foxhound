package captcha_test

import (
	"strings"
	"testing"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/captcha"
)

// respWithStatus builds a Response with an explicit status code.
func respWithStatus(statusCode int, body string) *foxhound.Response {
	return &foxhound.Response{
		StatusCode: statusCode,
		Body:       []byte(body),
		URL:        "https://example.com/page",
	}
}

// ---------------------------------------------------------------------------
// TestDetect_SoftBlock
// ---------------------------------------------------------------------------

func TestDetect_SoftBlock(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "access denied in small body",
			body: `<html><body>Access Denied</body></html>`,
		},
		{
			name: "permission denied in small body",
			body: `<html><body>Permission denied. Contact the administrator.</body></html>`,
		},
		{
			name: "blocked keyword in small body",
			body: `<html><body>Your request has been blocked by our security system.</body></html>`,
		},
		{
			name: "forbidden keyword in small body",
			body: `<html><body>Forbidden. You do not have permission to access this resource.</body></html>`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// All are status 200 but contain block keywords and body < 10000 bytes.
			r := respWithStatus(200, tc.body)
			got := captcha.Detect(r)
			if got.Type != captcha.CaptchaSoftBlock {
				t.Errorf("expected CaptchaSoftBlock, got %q (body: %s)", got.Type, tc.body)
			}
		})
	}
}

func TestDetect_SoftBlock_LargeBodyNotFlagged(t *testing.T) {
	// A large page that happens to mention "blocked" somewhere should not trigger.
	longPage := strings.Repeat("<p>This is normal content about a traffic blocked road.</p>", 300)
	body := "<html><body>" + longPage + "</body></html>"
	r := respWithStatus(200, body)
	got := captcha.Detect(r)
	// Large body (>10000 bytes) with incidental keyword should not be CaptchaSoftBlock.
	if got.Type == captcha.CaptchaSoftBlock {
		t.Errorf("large body with incidental keyword should not be CaptchaSoftBlock, got %q", got.Type)
	}
}

// ---------------------------------------------------------------------------
// TestDetect_EmptyTrap
// ---------------------------------------------------------------------------

func TestDetect_EmptyTrap(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "tiny JSON body without html",
			body: `{"ok":true}`,
		},
		{
			name: "empty-ish text without html tag",
			body: `Service unavailable.`,
		},
		{
			name: "short body no html marker",
			body: `<title>Error</title>`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.body) >= 500 {
				t.Fatalf("test body must be <500 bytes, got %d", len(tc.body))
			}
			r := respWithStatus(200, tc.body)
			got := captcha.Detect(r)
			if got.Type != captcha.CaptchaEmptyTrap {
				t.Errorf("expected CaptchaEmptyTrap, got %q", got.Type)
			}
		})
	}
}

func TestDetect_EmptyTrap_SmallHTMLNotFlagged(t *testing.T) {
	// Small body but contains <html — legitimate minimal page.
	body := `<html><head></head><body><p>Hi</p></body></html>`
	r := respWithStatus(200, body)
	got := captcha.Detect(r)
	if got.Type == captcha.CaptchaEmptyTrap {
		t.Errorf("small but valid HTML should not be CaptchaEmptyTrap, got %q", got.Type)
	}
}

// ---------------------------------------------------------------------------
// TestDetect_JSChallenge
// ---------------------------------------------------------------------------

func TestDetect_JSChallenge(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "browser verification phrase",
			body: `<html><body><h1>Browser Verification</h1><script>verify();</script></body></html>`,
		},
		{
			name: "security challenge phrase",
			body: `<html><body>Security Challenge - please wait while we verify your browser.</body></html>`,
		},
		{
			name: "akamai challenge combo",
			body: `<html><body>This site uses Akamai. Your request requires a challenge to proceed.</body></html>`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := respWithStatus(200, tc.body)
			got := captcha.Detect(r)
			if got.Type != captcha.CaptchaJSChallenge {
				t.Errorf("expected CaptchaJSChallenge, got %q (body: %s)", got.Type, tc.body)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestDetect_NormalPageNoFalsePositive
// ---------------------------------------------------------------------------

func TestDetect_NormalPageNoFalsePositive(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "typical product page",
			body: `<html><head><title>Buy Widget Pro</title></head><body>
				<h1>Widget Pro</h1>
				<p>The best widget on the market. Price: $29.99</p>
				<button>Add to Cart</button>
			</body></html>`,
		},
		{
			name: "article page with incidental words",
			body: `<html><head><title>Road safety article</title></head><body>
				<h1>When traffic is blocked on the highway</h1>
				<p>Forbidden zones near construction sites require special access.
				The forbidden fruit is always sweeter. Access denied by road closure signs.</p>
				<p>This is a full article with substantial content that makes the body large enough.</p>
				` + strings.Repeat("<p>More normal content here to push the body over 10000 chars.</p>", 200) + `
			</body></html>`,
		},
		{
			name: "normal login page at login URL",
			body: `<html><body>
				<form action="/login" method="POST">
					<input type="text" name="username" />
					<input type="password" name="password" />
					<button type="submit">Sign In</button>
				</form>
			</body></html>`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := respWithStatus(200, tc.body)
			got := captcha.Detect(r)
			// Should not trigger any of our new types.
			switch got.Type {
			case captcha.CaptchaSoftBlock, captcha.CaptchaEmptyTrap, captcha.CaptchaJSChallenge:
				t.Errorf("false positive for normal page %q: got type %q", tc.name, got.Type)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestDetect_NewConstants_Defined
// ---------------------------------------------------------------------------

func TestDetect_NewConstants_Defined(t *testing.T) {
	// Verify the new CaptchaType constants are exported and have expected values.
	if captcha.CaptchaSoftBlock == "" {
		t.Error("CaptchaSoftBlock should not be empty string")
	}
	if captcha.CaptchaEmptyTrap == "" {
		t.Error("CaptchaEmptyTrap should not be empty string")
	}
	if captcha.CaptchaLoginWall == "" {
		t.Error("CaptchaLoginWall should not be empty string")
	}
	if captcha.CaptchaJSChallenge == "" {
		t.Error("CaptchaJSChallenge should not be empty string")
	}
}
