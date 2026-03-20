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
	var result []byte
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
		return dedup(emailRe.FindAllString(string(resp.Body), -1))
	}

	seen := make(map[string]bool)
	var emails []string
	add := func(e string) {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" && strings.Contains(e, "@") && !seen[e] {
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
		return dedup(phoneRe.FindAllString(string(resp.Body), -1))
	}

	seen := make(map[string]bool)
	var phones []string
	add := func(p string) {
		// Strip whitespace for dedup.
		cleaned := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' || r == '+' {
				return r
			}
			return -1
		}, p)
		if len(cleaned) >= 7 && !seen[cleaned] {
			seen[cleaned] = true
			phones = append(phones, p)
		}
	}

	// tel: links.
	doc.Each("a[href^='tel:']", func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		add(strings.TrimPrefix(href, "tel:"))
	})

	// Plaintext regex.
	for _, m := range phoneRe.FindAllString(doc.Text("body"), -1) {
		add(m)
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
