package parse_test

import (
	"strings"
	"testing"

	foxhound "github.com/sadewadee/foxhound"
	"github.com/sadewadee/foxhound/parse"
)

// makeResp is a helper that builds a minimal foxhound.Response from an HTML string.
func makeResp(html string) *foxhound.Response {
	return &foxhound.Response{
		StatusCode: 200,
		Body:       []byte(html),
		URL:        "http://example.com",
	}
}

// containsStr checks whether s appears in the slice.
func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// ─── ExtractEmails ────────────────────────────────────────────────────────────

// TestExtractEmails_RealAddresses verifies that genuine contact emails are kept.
func TestExtractEmails_RealAddresses(t *testing.T) {
	cases := []struct {
		name  string
		email string
	}{
		{"simple", "info@acmecorp.com"},
		{"subdomain", "support@mail.acmecorp.org"},
		{"plus-tag", "user.name+tag@domain.co.uk"},
		{"hyphen-domain", "hello@my-company.io"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html := `<html><body><p>Contact: ` + tc.email + `</p></body></html>`
			resp := makeResp(html)
			got := parse.ExtractEmails(resp)
			if !containsStr(got, tc.email) {
				t.Errorf("expected %q in result %v", tc.email, got)
			}
		})
	}
}

// TestExtractEmails_MailtoLink verifies that mailto: hrefs are extracted.
func TestExtractEmails_MailtoLink(t *testing.T) {
	html := `<html><body><a href="mailto:contact@acmecorp.com">Email us</a></body></html>`
	resp := makeResp(html)
	got := parse.ExtractEmails(resp)
	if !containsStr(got, "contact@acmecorp.com") {
		t.Errorf("mailto link not extracted, got %v", got)
	}
}

// TestExtractEmails_ImageFilenameRejected ensures asset filenames are filtered.
func TestExtractEmails_ImageFilenameRejected(t *testing.T) {
	cases := []string{
		"logo@2x.png",
		"icon@3x.jpg",
		"sprite@1x.svg",
		"bundle@hash.js",
		"style@ver.css",
		"image@hd.webp",
	}
	for _, fake := range cases {
		t.Run(fake, func(t *testing.T) {
			html := `<html><body><p>` + fake + `</p></body></html>`
			resp := makeResp(html)
			got := parse.ExtractEmails(resp)
			if containsStr(got, fake) {
				t.Errorf("false positive %q should have been rejected, got %v", fake, got)
			}
		})
	}
}

// TestExtractEmails_SpamDomainsRejected ensures known infrastructure domains are filtered.
func TestExtractEmails_SpamDomainsRejected(t *testing.T) {
	cases := []string{
		"tracking@sentry.io",
		"error@wixpress.com",
		"cdn@cloudflare.com",
		"static@gstatic.com",
		"api@googleapis.com",
		"schema@w3.org",
	}
	for _, fake := range cases {
		t.Run(fake, func(t *testing.T) {
			html := `<html><body><p>` + fake + `</p></body></html>`
			resp := makeResp(html)
			got := parse.ExtractEmails(resp)
			if containsStr(got, fake) {
				t.Errorf("infrastructure email %q should have been rejected, got %v", fake, got)
			}
		})
	}
}

// TestExtractEmails_URLEncodedPrefixRejected ensures %xx-prefixed garbage is filtered.
func TestExtractEmails_URLEncodedPrefixRejected(t *testing.T) {
	// The regex will pick up "%20info@domain.com" from plain text — it must be dropped.
	html := `<html><body><p>%20info@domain.com</p></body></html>`
	resp := makeResp(html)
	got := parse.ExtractEmails(resp)
	for _, v := range got {
		if strings.HasPrefix(v, "%") {
			t.Errorf("URL-encoded prefix survived filtering: %q (all: %v)", v, got)
		}
	}
}

// TestExtractEmails_DigitsOnlyLocalRejected ensures numeric-only local parts are filtered.
func TestExtractEmails_DigitsOnlyLocalRejected(t *testing.T) {
	cases := []string{
		"2.0@domain.com",
		"123@example.com",
	}
	for _, fake := range cases {
		t.Run(fake, func(t *testing.T) {
			html := `<html><body><p>` + fake + `</p></body></html>`
			resp := makeResp(html)
			got := parse.ExtractEmails(resp)
			if containsStr(got, fake) {
				t.Errorf("digits-only local part %q should have been rejected, got %v", fake, got)
			}
		})
	}
}

// TestExtractEmails_DeduplicatesCaseInsensitive checks that duplicates are collapsed.
func TestExtractEmails_DeduplicatesCaseInsensitive(t *testing.T) {
	html := `<html><body>
		<p>INFO@Acmecorp.COM</p>
		<p>info@acmecorp.com</p>
	</body></html>`
	resp := makeResp(html)
	got := parse.ExtractEmails(resp)
	count := 0
	for _, v := range got {
		if v == "info@acmecorp.com" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 occurrence of info@acmecorp.com, got %d (all: %v)", count, got)
	}
}

// ─── ExtractPhones ────────────────────────────────────────────────────────────

// TestExtractPhones_RealNumbers verifies that genuine phone numbers are kept.
func TestExtractPhones_RealNumbers(t *testing.T) {
	cases := []struct {
		name    string
		html    string
		contain string
	}{
		{
			"indonesian-mobile",
			`<html><body><p>+62 877 6075 4858</p></body></html>`,
			"+62 877 6075 4858",
		},
		{
			"us-format",
			`<html><body><p>(021) 555-1234 ext 0</p></body></html>`,
			"(021) 555-1234",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := makeResp(tc.html)
			got := parse.ExtractPhones(resp)
			if !containsStr(got, tc.contain) {
				t.Errorf("expected %q in result %v", tc.contain, got)
			}
		})
	}
}

// TestExtractPhones_TelLinkExtracted verifies tel: href extraction.
func TestExtractPhones_TelLinkExtracted(t *testing.T) {
	html := `<html><body><a href="tel:+6287760754858">Call us</a></body></html>`
	resp := makeResp(html)
	got := parse.ExtractPhones(resp)
	if !containsStr(got, "+6287760754858") {
		t.Errorf("tel link not extracted, got %v", got)
	}
}

// TestExtractPhones_AllSameDigitsRejected ensures 0000000000-style inputs are filtered.
func TestExtractPhones_AllSameDigitsRejected(t *testing.T) {
	// Need to embed in tel: link to guarantee regex match despite short/same digits.
	// Use plaintext with enough digits for the regex but still all-same.
	html := `<html><body><p>0000 0000 0000</p></body></html>`
	resp := makeResp(html)
	got := parse.ExtractPhones(resp)
	for _, v := range got {
		cleaned := ""
		for _, r := range v {
			if r >= '0' && r <= '9' {
				cleaned += string(r)
			}
		}
		allSame := true
		for _, r := range cleaned {
			if r != rune(cleaned[0]) {
				allSame = false
				break
			}
		}
		if allSame && len(cleaned) >= 10 {
			t.Errorf("all-same digit number %q should have been rejected", v)
		}
	}
}

// TestExtractPhones_SequentialDigitsRejected ensures strictly-ascending digit
// runs (0123456789) are filtered as placeholder/test numbers.
func TestExtractPhones_SequentialDigitsRejected(t *testing.T) {
	// "0123456789" is the canonical 0→9 sequential placeholder that should be
	// rejected. The phoneRe regex matches it as a 10-digit number.
	html := `<html><body><p>0123456789</p></body></html>`
	resp := makeResp(html)
	got := parse.ExtractPhones(resp)
	if containsStr(got, "0123456789") {
		t.Errorf("sequential digits %q should have been rejected, got %v", "0123456789", got)
	}
}

// TestExtractPhones_ShortNumberRejected ensures sub-10-digit strings are filtered.
func TestExtractPhones_ShortNumberRejected(t *testing.T) {
	// "6336530" has 7 digits — previously accepted, must now be rejected.
	html := `<html><body><p>6336530</p></body></html>`
	resp := makeResp(html)
	got := parse.ExtractPhones(resp)
	if containsStr(got, "6336530") {
		t.Errorf("short number 6336530 should have been rejected, got %v", got)
	}
}

// TestExtractPhones_DeduplicatesNumbers checks that the same number isn't returned twice.
func TestExtractPhones_DeduplicatesNumbers(t *testing.T) {
	html := `<html><body>
		<a href="tel:+6287760754858">Call</a>
		<p>+62 877 6075 4858</p>
	</body></html>`
	resp := makeResp(html)
	got := parse.ExtractPhones(resp)
	if len(got) > 1 {
		t.Errorf("expected dedup to produce 1 result, got %d: %v", len(got), got)
	}
}

// ─── Issue #33: False positive fixes ─────────────────────────────────────────

// TestExtractEmails_AvifIcoImageRejected ensures .avif and .ico image filenames are filtered.
func TestExtractEmails_AvifIcoImageRejected(t *testing.T) {
	cases := []string{
		"photo@hd.avif",
		"favicon@2x.ico",
	}
	for _, fake := range cases {
		t.Run(fake, func(t *testing.T) {
			html := `<html><body><p>` + fake + `</p></body></html>`
			resp := makeResp(html)
			got := parse.ExtractEmails(resp)
			if containsStr(got, fake) {
				t.Errorf("image filename %q should have been rejected, got %v", fake, got)
			}
		})
	}
}

// TestExtractEmails_NoReplyRejected ensures noreply/no-reply addresses are filtered.
func TestExtractEmails_NoReplyRejected(t *testing.T) {
	cases := []string{
		"noreply@company.com",
		"no-reply@company.com",
		"donotreply@company.com",
		"mailer-daemon@company.com",
	}
	for _, fake := range cases {
		t.Run(fake, func(t *testing.T) {
			html := `<html><body><p>` + fake + `</p></body></html>`
			resp := makeResp(html)
			got := parse.ExtractEmails(resp)
			if containsStr(got, fake) {
				t.Errorf("no-reply address %q should have been rejected, got %v", fake, got)
			}
		})
	}
}

// TestExtractEmails_ReservedDomainRejected ensures RFC 2606 reserved domains are filtered.
func TestExtractEmails_ReservedDomainRejected(t *testing.T) {
	cases := []string{
		"test@example.com",
		"user@example.org",
		"admin@example.net",
		"foo@test.com",
	}
	for _, fake := range cases {
		t.Run(fake, func(t *testing.T) {
			html := `<html><body><p>` + fake + `</p></body></html>`
			resp := makeResp(html)
			got := parse.ExtractEmails(resp)
			if containsStr(got, fake) {
				t.Errorf("reserved domain address %q should have been rejected, got %v", fake, got)
			}
		})
	}
}

// TestExtractPhones_IPAddressRejected ensures IP-address-like strings are not treated as phones.
func TestExtractPhones_IPAddressRejected(t *testing.T) {
	html := `<html><body><p>192.168.1.100</p></body></html>`
	resp := makeResp(html)
	got := parse.ExtractPhones(resp)
	for _, v := range got {
		cleaned := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, v)
		if cleaned == "1921681100" {
			t.Errorf("IP address %q should have been rejected, got %v", v, got)
		}
	}
}

// TestExtractPhones_VersionNumberRejected ensures version strings are not treated as phones.
func TestExtractPhones_VersionNumberRejected(t *testing.T) {
	html := `<html><body><p>2024.01.15</p></body></html>`
	resp := makeResp(html)
	got := parse.ExtractPhones(resp)
	for _, v := range got {
		if strings.Contains(v, "2024") && strings.Contains(v, "01") && strings.Contains(v, "15") {
			t.Errorf("version number %q should have been rejected, got %v", v, got)
		}
	}
}

// TestExtractPhones_DescendingSequentialRejected ensures descending runs like 9876543210 are filtered.
func TestExtractPhones_DescendingSequentialRejected(t *testing.T) {
	html := `<html><body><p>9876543210</p></body></html>`
	resp := makeResp(html)
	got := parse.ExtractPhones(resp)
	if containsStr(got, "9876543210") {
		t.Errorf("descending sequential digits %q should have been rejected, got %v", "9876543210", got)
	}
}

// TestExtractPhones_CSSDimensionRejected ensures CSS dimension values are not treated as phones.
func TestExtractPhones_CSSDimensionRejected(t *testing.T) {
	html := `<html><body><p>width: 1920px height: 1080px</p></body></html>`
	resp := makeResp(html)
	got := parse.ExtractPhones(resp)
	for _, v := range got {
		if strings.Contains(v, "1920") || strings.Contains(v, "1080") {
			t.Errorf("CSS dimension %q should have been rejected, got %v", v, got)
		}
	}
}
