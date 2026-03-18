package main

import (
	"strings"
	"testing"
)

// TestParseCurl_BasicGET verifies parsing of a simple GET curl command.
func TestParseCurl_BasicGET(t *testing.T) {
	p := parseCurl("curl https://example.com")
	if p.URL != "https://example.com" {
		t.Errorf("URL = %q, want %q", p.URL, "https://example.com")
	}
	if p.Method != "GET" {
		t.Errorf("Method = %q, want GET", p.Method)
	}
}

// TestParseCurl_WithHeaders verifies header parsing.
func TestParseCurl_WithHeaders(t *testing.T) {
	p := parseCurl(`curl https://example.com -H "Accept: text/html" -H "X-Custom: value"`)
	if p.URL != "https://example.com" {
		t.Errorf("URL = %q", p.URL)
	}
	if p.Headers.Get("Accept") != "text/html" {
		t.Errorf("Accept header = %q", p.Headers.Get("Accept"))
	}
	if p.Headers.Get("X-Custom") != "value" {
		t.Errorf("X-Custom header = %q", p.Headers.Get("X-Custom"))
	}
}

// TestParseCurl_POST verifies POST with -X flag.
func TestParseCurl_POST(t *testing.T) {
	p := parseCurl(`curl -X POST https://api.example.com/data -d '{"key":"val"}'`)
	if p.Method != "POST" {
		t.Errorf("Method = %q, want POST", p.Method)
	}
	if p.Data != `{"key":"val"}` {
		t.Errorf("Data = %q", p.Data)
	}
}

// TestParseCurl_ImplicitPOST verifies that -d implies POST.
func TestParseCurl_ImplicitPOST(t *testing.T) {
	p := parseCurl(`curl https://api.example.com -d "field=value"`)
	if p.Method != "POST" {
		t.Errorf("Method = %q, want POST (implied by -d)", p.Method)
	}
}

// TestParseCurl_UserAgent verifies -A flag.
func TestParseCurl_UserAgent(t *testing.T) {
	p := parseCurl(`curl -A "MyBot/1.0" https://example.com`)
	if p.Headers.Get("User-Agent") != "MyBot/1.0" {
		t.Errorf("User-Agent = %q", p.Headers.Get("User-Agent"))
	}
}

// TestParseCurl_Cookie verifies -b flag.
func TestParseCurl_Cookie(t *testing.T) {
	p := parseCurl(`curl -b "session=abc123" https://example.com`)
	if p.Headers.Get("Cookie") != "session=abc123" {
		t.Errorf("Cookie = %q", p.Headers.Get("Cookie"))
	}
}

// TestParseCurl_Referer verifies -e flag.
func TestParseCurl_Referer(t *testing.T) {
	p := parseCurl(`curl -e "https://google.com" https://example.com`)
	if p.Headers.Get("Referer") != "https://google.com" {
		t.Errorf("Referer = %q", p.Headers.Get("Referer"))
	}
}

// TestParseCurl_HEAD verifies -I flag.
func TestParseCurl_HEAD(t *testing.T) {
	p := parseCurl(`curl -I https://example.com`)
	if p.Method != "HEAD" {
		t.Errorf("Method = %q, want HEAD", p.Method)
	}
}

// TestParseCurl_JSONFlag verifies --json flag.
func TestParseCurl_JSONFlag(t *testing.T) {
	p := parseCurl(`curl --json '{"name":"test"}' https://api.example.com`)
	if p.Method != "POST" {
		t.Errorf("Method = %q, want POST", p.Method)
	}
	if !p.IsJSON {
		t.Error("IsJSON should be true")
	}
	if p.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", p.Headers.Get("Content-Type"))
	}
}

// TestGenerateCode_ContainsEssentials verifies generated code structure.
func TestGenerateCode_ContainsEssentials(t *testing.T) {
	p := parseCurl("curl https://example.com")
	code := generateCode(p)

	essentials := []string{
		"package main",
		"foxhound",
		"engine",
		"fetch",
		"identity",
		"https://example.com",
		"NewHunt",
		"Processor",
	}
	for _, e := range essentials {
		if !strings.Contains(code, e) {
			t.Errorf("generated code missing %q", e)
		}
	}
}

// TestGenerateCode_WithHeaders includes headers in generated code.
func TestGenerateCode_WithHeaders(t *testing.T) {
	p := parseCurl(`curl https://api.example.com -H "Authorization: Bearer token123"`)
	code := generateCode(p)

	if !strings.Contains(code, "Authorization") {
		t.Error("generated code missing Authorization header")
	}
	if !strings.Contains(code, "Bearer token123") {
		t.Error("generated code missing header value")
	}
}

// TestGenerateCode_WithBody includes body in generated code.
func TestGenerateCode_WithBody(t *testing.T) {
	p := parseCurl(`curl -X POST https://api.example.com -d '{"key":"val"}'`)
	code := generateCode(p)

	if !strings.Contains(code, "POST") {
		t.Error("generated code missing POST method")
	}
	if !strings.Contains(code, "Body") {
		t.Error("generated code missing Body field")
	}
}

// TestTokenize_Quotes verifies tokenization with quoted strings.
func TestTokenize_Quotes(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{`hello world`, 2},
		{`"hello world"`, 1},
		{`'hello world'`, 1},
		{`-H "Accept: text/html"`, 2},
		{`-H 'Content-Type: application/json' -d '{"key":"val"}'`, 4},
	}

	for _, tt := range tests {
		tokens := tokenize(tt.input)
		if len(tokens) != tt.want {
			t.Errorf("tokenize(%q) = %d tokens %v, want %d", tt.input, len(tokens), tokens, tt.want)
		}
	}
}

// TestTokenize_EscapedQuotes handles escaped characters.
func TestTokenize_EscapedQuotes(t *testing.T) {
	tokens := tokenize(`"hello \"world\""`)
	if len(tokens) != 1 {
		t.Errorf("expected 1 token, got %d: %v", len(tokens), tokens)
	}
}
