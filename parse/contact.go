package parse

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/sadewadee/foxhound"
)

var (
	emailRe = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	phoneRe = regexp.MustCompile(`(?:\+\d{1,3}[\s\-]?)?\(?\d{2,4}\)?[\s\-]?\d{3,4}[\s\-]?\d{3,4}`)
)

// digitPlusFilter strips non-digit, non-plus characters. Package-level to
// avoid closure allocation per phone candidate.
func digitPlusFilter(r rune) rune {
	if r >= '0' && r <= '9' || r == '+' {
		return r
	}
	return -1
}

// imageExtensions are file extensions that indicate an email-like string is
// actually an image filename (e.g. logo@2x.png).
var imageExtensions = []string{".png", ".jpg", ".jpeg", ".gif", ".svg", ".js", ".css", ".webp", ".avif", ".bmp", ".tiff", ".ico"}

// spamDomains are infrastructure/CDN domains whose addresses should not be
// returned as contact emails.
var spamDomains = []string{
	"sentry.io", "wixpress.com", "cloudflare.com",
	"gstatic.com", "googleapis.com", "w3.org",
	"fontawesome.com", "jquery.com", "bootstrapcdn.com",
	"unpkg.com", "cdnjs.cloudflare.com", "github.com",
	"githubusercontent.com",
}

// noReplyPrefixes are local-part prefixes that indicate automated/no-reply addresses.
var noReplyPrefixes = []string{"noreply@", "no-reply@", "donotreply@", "mailer-daemon@"}

// reservedDomains are RFC 2606 reserved domains and common test domains.
var reservedDomains = []string{"example.com", "example.org", "example.net", "test.com"}

// isLikelyEmail returns false for strings that match the email regex but are
// clearly not real contact addresses.
func isLikelyEmail(email string) bool {
	lower := strings.ToLower(email)

	// Reject URL-encoded prefix (e.g. %20info@domain.com).
	if strings.HasPrefix(email, "%") {
		return false
	}

	// Reject image filenames and asset URLs (e.g. logo@2x.png).
	for _, ext := range imageExtensions {
		if strings.HasSuffix(lower, ext) {
			return false
		}
	}

	// Reject no-reply/automated addresses.
	for _, prefix := range noReplyPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}

	// Reject known infrastructure/CDN domains.
	atIdx := strings.LastIndex(lower, "@")
	if atIdx < 0 {
		return false
	}
	domain := lower[atIdx+1:]
	for _, d := range spamDomains {
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return false
		}
	}

	// Reject RFC 2606 reserved domains and common test domains.
	for _, d := range reservedDomains {
		if domain == d {
			return false
		}
	}

	// Reject local parts that are only digits and dots (e.g. 2.0@domain.com).
	localPart := lower[:atIdx]
	allDigitsDots := true
	for _, r := range localPart {
		if r != '.' && (r < '0' || r > '9') {
			allDigitsDots = false
			break
		}
	}
	if allDigitsDots {
		return false
	}

	return true
}

// Regex patterns for phone false-positive detection.
var (
	ipAddrRe  = regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)
	versionRe = regexp.MustCompile(`\d+\.\d+\.\d+`)
	cssDimRe  = regexp.MustCompile(`\d+(?:px|em|rem|%)`)
)

// isLikelyPhone returns false for digit strings that are clearly not real phone
// numbers (all-same digits, sequential runs, suspicious prefixes, IP addresses,
// version numbers, and CSS dimensions).
func isLikelyPhone(cleaned string) bool {
	// Strip leading '+' for pattern checks.
	digits := strings.TrimPrefix(cleaned, "+")
	if len(digits) == 0 {
		return false
	}

	// Reject all-same-digit sequences (e.g. 0000000000).
	allSame := true
	for _, r := range digits {
		if r != rune(digits[0]) {
			allSame = false
			break
		}
	}
	if allSame {
		return false
	}

	// Reject ascending sequential digit runs (e.g. 1234567890, 0123456789).
	ascending := true
	for i := 1; i < len(digits); i++ {
		if digits[i] != digits[i-1]+1 {
			ascending = false
			break
		}
	}
	if ascending {
		return false
	}

	// Reject descending sequential digit runs (e.g. 9876543210).
	descending := true
	for i := 1; i < len(digits); i++ {
		if digits[i-1] != digits[i]+1 {
			descending = false
			break
		}
	}
	if descending {
		return false
	}

	// Reject numbers starting with suspicious prefixes.
	if strings.HasPrefix(digits, "0000") || strings.HasPrefix(digits, "9999") {
		return false
	}

	return true
}

// isLikelyPhoneRaw checks the raw (unstripped) candidate for patterns that
// indicate the string is not a real phone number. Called before digit stripping.
func isLikelyPhoneRaw(raw string) bool {
	// Reject IP addresses (e.g. 192.168.1.100).
	if ipAddrRe.MatchString(raw) {
		return false
	}

	// Reject version numbers (e.g. 2024.01.15, v1.2.3).
	stripped := strings.TrimPrefix(raw, "v")
	stripped = strings.TrimPrefix(stripped, "V")
	if versionRe.MatchString(stripped) {
		return false
	}

	// Reject CSS dimensions (e.g. 100px, 1.5em, 2rem, 50%).
	if cssDimRe.MatchString(raw) {
		return false
	}

	return true
}

// DecodeCFEmail decodes a CloudFlare-obfuscated email string.
// CloudFlare XOR-encodes emails: first 2 hex chars are the key,
// remaining hex pairs are XOR'd with the key.
func DecodeCFEmail(encoded string) string {
	if len(encoded) < 4 || len(encoded)%2 != 0 {
		return ""
	}
	key, err := strconv.ParseInt(encoded[:2], 16, 64)
	if err != nil {
		return ""
	}
	result := make([]byte, 0, (len(encoded)-2)/2)
	for i := 2; i < len(encoded); i += 2 {
		val, err := strconv.ParseInt(encoded[i:i+2], 16, 64)
		if err != nil {
			return ""
		}
		result = append(result, byte(val^key))
	}
	return string(result)
}

// ExtractEmails finds all email addresses in the response, including
// CloudFlare cfemail-obfuscated ones, mailto: links, and plaintext.
func ExtractEmails(resp *foxhound.Response) []string {
	doc, err := NewDocument(resp)
	if err != nil {
		// Fallback to regex on raw body.
		var raw []string
		for _, m := range emailRe.FindAllString(string(resp.Body), -1) {
			if isLikelyEmail(strings.ToLower(strings.TrimSpace(m))) {
				raw = append(raw, m)
			}
		}
		return dedup(raw)
	}

	seen := make(map[string]bool)
	var emails []string
	add := func(e string) {
		e = strings.ToLower(strings.TrimSpace(e))
		// Strip URL-encoded prefix garbage (e.g. %20info@domain.com → info@domain.com).
		if strings.HasPrefix(e, "%") {
			trimmed := strings.TrimLeft(e, "%0123456789abcdef")
			if strings.Contains(trimmed, "@") {
				e = trimmed
			}
		}
		if e == "" || !strings.Contains(e, "@") {
			return
		}
		if !isLikelyEmail(e) {
			return
		}
		if !seen[e] {
			seen[e] = true
			emails = append(emails, e)
		}
	}

	// CloudFlare cfemail decode.
	doc.Each("[data-cfemail]", func(_ int, s *goquery.Selection) {
		if encoded, exists := s.Attr("data-cfemail"); exists {
			add(DecodeCFEmail(encoded))
		}
	})

	// mailto: links.
	doc.Each("a[href^='mailto:']", func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		email := strings.TrimPrefix(href, "mailto:")
		if idx := strings.Index(email, "?"); idx >= 0 {
			email = email[:idx]
		}
		add(email)
	})

	// Plaintext regex on visible text.
	for _, m := range emailRe.FindAllString(doc.Text("body"), -1) {
		add(m)
	}

	return emails
}

// ExtractPhones finds phone numbers in the response, including tel: links
// and plaintext. Numbers are returned as-is (not normalized).
func ExtractPhones(resp *foxhound.Response) []string {
	doc, err := NewDocument(resp)
	if err != nil {
		var raw []string
		for _, m := range phoneRe.FindAllString(string(resp.Body), -1) {
			if !isLikelyPhoneRaw(m) {
				continue
			}
			cleaned := strings.Map(digitPlusFilter, m)
			if len(cleaned) >= 10 && isLikelyPhone(cleaned) {
				raw = append(raw, m)
			}
		}
		return dedup(raw)
	}

	seen := make(map[string]bool)
	var phones []string

	// addPhone adds a candidate phone number. skipPatternCheck bypasses
	// isLikelyPhone for high-confidence sources such as tel: links.
	addPhone := func(p string, skipPatternCheck bool) {
		// Check raw string patterns before stripping (IP, version, CSS dims).
		if !skipPatternCheck && !isLikelyPhoneRaw(p) {
			return
		}
		// Strip non-digit, non-plus characters for dedup key and length check.
		cleaned := strings.Map(digitPlusFilter, p)
		if len(cleaned) < 10 {
			return
		}
		if !skipPatternCheck && !isLikelyPhone(cleaned) {
			return
		}
		if !seen[cleaned] {
			seen[cleaned] = true
			phones = append(phones, p)
		}
	}

	// tel: links — high confidence, skip pattern check.
	doc.Each("a[href^='tel:']", func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		addPhone(strings.TrimPrefix(href, "tel:"), true)
	})

	// Plaintext regex — apply full validation.
	for _, m := range phoneRe.FindAllString(doc.Text("body"), -1) {
		addPhone(m, false)
	}

	return phones
}

func dedup(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	var out []string
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
