package parse_test

import (
	"testing"

	"github.com/foxhound-scraper/foxhound/parse"
)

const regexHTML = `<!DOCTYPE html>
<html>
<body>
  <p>Price: 29.99</p>
  <p>SKU: ABC-123</p>
  <p>Email: user@example.com</p>
  <span>Color: red</span>
  <span>Color: blue</span>
  <span>Color: green</span>
</body>
</html>`

func TestRegexExtract_Found(t *testing.T) {
	resp := newHTMLResponse(regexHTML)

	result, err := parse.RegexExtract(resp, `Price: (\d+\.\d+)`)
	if err != nil {
		t.Fatalf("RegexExtract: %v", err)
	}
	if result != "Price: 29.99" {
		t.Errorf("RegexExtract: got %q, want %q", result, "Price: 29.99")
	}
}

func TestRegexExtract_NotFound(t *testing.T) {
	resp := newHTMLResponse(regexHTML)

	result, err := parse.RegexExtract(resp, `Nonexistent: \d+`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("RegexExtract: expected empty string, got %q", result)
	}
}

func TestRegexExtract_InvalidPattern(t *testing.T) {
	resp := newHTMLResponse(regexHTML)

	_, err := parse.RegexExtract(resp, `[invalid`)
	if err == nil {
		t.Fatal("expected error for invalid regex pattern, got nil")
	}
}

func TestRegexExtractAll_MultipleMatches(t *testing.T) {
	resp := newHTMLResponse(regexHTML)

	results, err := parse.RegexExtractAll(resp, `Color: \w+`)
	if err != nil {
		t.Fatalf("RegexExtractAll: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("RegexExtractAll: got %d matches, want 3", len(results))
	}
	want := []string{"Color: red", "Color: blue", "Color: green"}
	for i, w := range want {
		if results[i] != w {
			t.Errorf("results[%d]: got %q, want %q", i, results[i], w)
		}
	}
}

func TestRegexExtractAll_NoMatches(t *testing.T) {
	resp := newHTMLResponse(regexHTML)

	results, err := parse.RegexExtractAll(resp, `Nonexistent: \d+`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty slice, got %v", results)
	}
}

func TestRegexExtractNamed_SingleGroup(t *testing.T) {
	resp := newHTMLResponse(regexHTML)

	groups, err := parse.RegexExtractNamed(resp, `Price: (?P<price>\d+\.\d+)`)
	if err != nil {
		t.Fatalf("RegexExtractNamed: %v", err)
	}
	if groups["price"] != "29.99" {
		t.Errorf("named group 'price': got %q, want %q", groups["price"], "29.99")
	}
}

func TestRegexExtractNamed_MultipleGroups(t *testing.T) {
	resp := newHTMLResponse(regexHTML)

	groups, err := parse.RegexExtractNamed(resp, `SKU: (?P<prefix>[A-Z]+)-(?P<number>\d+)`)
	if err != nil {
		t.Fatalf("RegexExtractNamed: %v", err)
	}
	if groups["prefix"] != "ABC" {
		t.Errorf("named group 'prefix': got %q, want %q", groups["prefix"], "ABC")
	}
	if groups["number"] != "123" {
		t.Errorf("named group 'number': got %q, want %q", groups["number"], "123")
	}
}

func TestRegexExtractNamed_NoMatch(t *testing.T) {
	resp := newHTMLResponse(regexHTML)

	groups, err := parse.RegexExtractNamed(resp, `Zip: (?P<zip>\d{5})`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected empty map, got %v", groups)
	}
}

func TestRegexExtractSubmatch_Found(t *testing.T) {
	resp := newHTMLResponse(regexHTML)

	submatches, err := parse.RegexExtractSubmatch(resp, `Price: (\d+)\.(\d+)`)
	if err != nil {
		t.Fatalf("RegexExtractSubmatch: %v", err)
	}
	// submatches[0] = full match, [1] = first group, [2] = second group
	if len(submatches) < 3 {
		t.Fatalf("expected at least 3 submatches, got %d", len(submatches))
	}
	if submatches[1] != "29" {
		t.Errorf("submatch[1]: got %q, want %q", submatches[1], "29")
	}
	if submatches[2] != "99" {
		t.Errorf("submatch[2]: got %q, want %q", submatches[2], "99")
	}
}

func TestRegexExtractSubmatch_NotFound(t *testing.T) {
	resp := newHTMLResponse(regexHTML)

	submatches, err := parse.RegexExtractSubmatch(resp, `Zip: (\d{5})`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(submatches) != 0 {
		t.Errorf("expected empty slice, got %v", submatches)
	}
}
