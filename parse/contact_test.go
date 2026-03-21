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
		{"simple", "info@example.com"},
		{"subdomain", "support@mail.example.org"},
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
	html := `<html><body><a href="mailto:contact@example.com">Email us</a></body></html>`
	resp := makeResp(html)
	got := parse.ExtractEmails(resp)
	if !containsStr(got, "contact@example.com") {
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
		<p>INFO@Example.COM</p>
		<p>info@example.com</p>
	</body></html>`
	resp := makeResp(html)
	got := parse.ExtractEmails(resp)
	count := 0
	for _, v := range got {
		if v == "info@example.com" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 occurrence of info@example.com, got %d (all: %v)", count, got)
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
