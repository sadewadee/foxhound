package parse

import (
	"fmt"
	"regexp"

	foxhound "github.com/sadewadee/foxhound"
)

// RegexExtract returns the text of the first match of pattern in the response
// body.  Returns an empty string when there is no match.  Returns an error
// only when pattern is syntactically invalid.
func RegexExtract(resp *foxhound.Response, pattern string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("parse: regex: compile %q: %w", pattern, err)
	}
	m := re.Find(resp.Body)
	if m == nil {
		return "", nil
	}
	return string(m), nil
}

// RegexExtractAll returns the text of every non-overlapping match of pattern
// in the response body.  Returns an empty slice when there are no matches.
// Returns an error only when pattern is syntactically invalid.
func RegexExtractAll(resp *foxhound.Response, pattern string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("parse: regex: compile %q: %w", pattern, err)
	}
	raw := re.FindAll(resp.Body, -1)
	if raw == nil {
		return []string{}, nil
	}
	out := make([]string, len(raw))
	for i, m := range raw {
		out[i] = string(m)
	}
	return out, nil
}

// RegexExtractNamed extracts named capture groups from the first match of
// pattern in the response body.  Returns a map of group name → captured text.
// Returns an empty map when there is no match.  Returns an error only when
// pattern is syntactically invalid.
//
// Example:
//
//	groups, _ := RegexExtractNamed(resp, `Price: (?P<price>\d+\.\d+)`)
//	fmt.Println(groups["price"]) // "29.99"
func RegexExtractNamed(resp *foxhound.Response, pattern string) (map[string]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("parse: regex: compile %q: %w", pattern, err)
	}

	names := re.SubexpNames()
	match := re.FindSubmatch(resp.Body)
	if match == nil {
		return map[string]string{}, nil
	}

	result := make(map[string]string, len(names))
	for i, name := range names {
		if name != "" && i < len(match) {
			result[name] = string(match[i])
		}
	}
	return result, nil
}

// RegexExtractSubmatch returns all submatch groups (including the full match
// at index 0) for the first match of pattern in the response body.  Returns
// an empty slice when there is no match.  Returns an error only when pattern
// is syntactically invalid.
func RegexExtractSubmatch(resp *foxhound.Response, pattern string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("parse: regex: compile %q: %w", pattern, err)
	}

	raw := re.FindSubmatch(resp.Body)
	if raw == nil {
		return []string{}, nil
	}

	out := make([]string, len(raw))
	for i, m := range raw {
		out[i] = string(m)
	}
	return out, nil
}
