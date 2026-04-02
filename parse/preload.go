package parse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	foxhound "github.com/sadewadee/foxhound"
)

// PreloadedData contains all detected JavaScript preloaded data from a page.
type PreloadedData struct {
	Framework string         // "nextjs", "nuxt", "react", "vue", "angular", "unknown"
	Variables map[string]any // all detected window.__VAR__ values
	NextData  map[string]any // shortcut to __NEXT_DATA__.props.pageProps (nil if not Next.js)
}

// wellKnownVars lists the window variables commonly used by frameworks to
// preload data into server-rendered pages.
var wellKnownVars = []string{
	"__NEXT_DATA__",
	"__NUXT__",
	"__INITIAL_STATE__",
	"__APP_STATE__",
	"__APOLLO_STATE__",
	"__RELAY_STORE__",
	"__PRELOADED_STATE__",
	"__REDUX_STATE__",
}

// ExtractWindowVar extracts a named window variable from the page.
// Strategy 1: look for <script id="__VARNAME__" type="application/json">.
// Strategy 2: regex for window.{varName} = {...} with balanced-brace extraction.
// Returns nil, nil if not found.
func ExtractWindowVar(resp *foxhound.Response, varName string) (any, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, err
	}

	// Strategy 1: <script id="__VARNAME__" type="application/json">
	scriptSel := fmt.Sprintf("script#%s[type='application/json']", varName)
	script := doc.Find(scriptSel).First()
	if script.Length() > 0 {
		raw := strings.TrimSpace(script.Text())
		if raw != "" {
			var data any
			if err := json.Unmarshal([]byte(raw), &data); err == nil {
				return data, nil
			}
		}
	}

	// Strategy 2: regex window.{varName} = {...} in script blocks.
	body := string(resp.Body)
	pattern := regexp.MustCompile(`window\.` + regexp.QuoteMeta(varName) + `\s*=\s*`)
	loc := pattern.FindStringIndex(body)
	if loc != nil {
		// Find the opening brace after the match.
		rest := body[loc[1]:]
		braceIdx := strings.IndexByte(rest, '{')
		if braceIdx >= 0 {
			jsonStr := extractBalancedJSON(rest, braceIdx)
			if jsonStr != "" {
				var data any
				if err := json.Unmarshal([]byte(jsonStr), &data); err == nil {
					return data, nil
				}
			}
		}
	}

	return nil, nil
}

// ExtractInlineJSON finds var {varPattern} = {...} or {varPattern} = {...} in
// <script> blocks and extracts the JSON object using balanced-brace matching.
// Returns nil, nil if not found.
func ExtractInlineJSON(resp *foxhound.Response, varPattern string) (any, error) {
	body := string(resp.Body)

	// Try "var <pattern> = {" and "<pattern> = {" forms.
	patterns := []string{
		`(?:var\s+)` + regexp.QuoteMeta(varPattern) + `\s*=\s*`,
		regexp.QuoteMeta(varPattern) + `\s*=\s*`,
	}

	for _, p := range patterns {
		re := regexp.MustCompile(p)
		loc := re.FindStringIndex(body)
		if loc == nil {
			continue
		}
		rest := body[loc[1]:]
		braceIdx := strings.IndexByte(rest, '{')
		if braceIdx < 0 {
			continue
		}
		jsonStr := extractBalancedJSON(rest, braceIdx)
		if jsonStr == "" {
			continue
		}
		var data any
		if err := json.Unmarshal([]byte(jsonStr), &data); err == nil {
			return data, nil
		}
	}

	return nil, nil
}

// ExtractPreloadedData auto-detects all preloaded data on the page by checking
// well-known window variables. It also detects the framework and provides a
// shortcut to Next.js pageProps.
func ExtractPreloadedData(resp *foxhound.Response) (*PreloadedData, error) {
	pd := &PreloadedData{
		Framework: DetectFramework(resp),
		Variables: make(map[string]any),
	}

	for _, varName := range wellKnownVars {
		val, err := ExtractWindowVar(resp, varName)
		if err != nil {
			return nil, err
		}
		if val != nil {
			pd.Variables[varName] = val
		}
	}

	// Shortcut for Next.js pageProps.
	if nextData, ok := pd.Variables["__NEXT_DATA__"]; ok {
		if m, ok := nextData.(map[string]any); ok {
			if props, ok := m["props"].(map[string]any); ok {
				if pageProps, ok := props["pageProps"].(map[string]any); ok {
					pd.NextData = pageProps
				}
			}
		}
	}

	return pd, nil
}

// DetectFramework inspects the response for framework-specific markers and
// returns the framework name: "nextjs", "nuxt", "react", "vue", "angular",
// or "unknown".
func DetectFramework(resp *foxhound.Response) string {
	body := string(resp.Body)

	// Check for Next.js: __NEXT_DATA__ script tag or window assignment.
	if strings.Contains(body, "__NEXT_DATA__") {
		return "nextjs"
	}

	// Check for Nuxt: __NUXT__ or __NUXT_DATA__.
	if strings.Contains(body, "__NUXT__") || strings.Contains(body, "__NUXT_DATA__") {
		return "nuxt"
	}

	// Check for React: data-reactroot attribute.
	if strings.Contains(body, "data-reactroot") {
		return "react"
	}

	// Check for Vue: data-v- attribute prefix.
	if strings.Contains(body, "data-v-") {
		return "vue"
	}

	// Check for Angular: ng-version attribute.
	if strings.Contains(body, "ng-version") {
		return "angular"
	}

	return "unknown"
}

// extractBalancedJSON finds a JSON object starting at position start in s.
// start should point to the opening '{'. Returns the JSON string including
// braces, or "" if unbalanced.
func extractBalancedJSON(s string, start int) string {
	if start >= len(s) || s[start] != '{' {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		if escaped {
			escaped = false
			continue
		}
		ch := s[i]
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return "" // unbalanced
}
