package parse

import (
	"regexp"
	"strings"

	foxhound "github.com/sadewadee/foxhound"
)

// XPath provides simplified XPath-like queries on HTML responses.
//
// Only a commonly-needed subset of XPath is supported.  Expressions are
// converted to CSS selectors and delegated to the existing goquery Document,
// so no additional dependencies are required.
//
// Supported syntax:
//
//	//tag                  → tag
//	//tag[@attr]           → tag[attr]
//	//tag[@attr='value']   → tag[attr='value']  (id shorthand: tag#value)
//	//tag/subtag           → tag > subtag        (direct child)
//	//tag//subtag          → tag subtag          (any descendant)
type XPath struct {
	doc *Document
}

// NewXPath creates an XPath from a foxhound Response.
func NewXPath(resp *foxhound.Response) (*XPath, error) {
	doc, err := NewDocument(resp)
	if err != nil {
		return nil, err
	}
	return &XPath{doc: doc}, nil
}

// Select evaluates a simplified XPath expression and returns the trimmed text
// content of the first matching element.  Returns an empty string when no
// element matches.
func (x *XPath) Select(expr string) string {
	css := xpathToCSS(expr)
	return strings.TrimSpace(x.doc.Text(css))
}

// SelectAll evaluates and returns the trimmed text content of all matching
// elements.
func (x *XPath) SelectAll(expr string) []string {
	css := xpathToCSS(expr)
	raw := x.doc.Texts(css)
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		out = append(out, strings.TrimSpace(s))
	}
	return out
}

// SelectAttr evaluates the expression and returns the value of attr on the
// first matching element.  Returns an empty string when no element matches.
func (x *XPath) SelectAttr(expr, attr string) string {
	css := xpathToCSS(expr)
	return x.doc.Attr(css, attr)
}

// ─── XPath → CSS conversion ──────────────────────────────────────────────────

// attrPredRe matches XPath attribute predicates: [@attr] or [@attr='value'].
var attrPredRe = regexp.MustCompile(`\[@(\w+)(?:='([^']*)')?\]`)

// xpathToCSS converts a limited XPath expression to a CSS selector string.
//
// The conversion is performed in two passes:
//  1. Tokenise the expression into (axis, step) pairs where axis is either
//     "descendant" (//) or "child" (/).
//  2. Convert each step from XPath syntax to CSS selector syntax and join
//     with the appropriate CSS combinator (space vs. " > ").
func xpathToCSS(expr string) string {
	expr = strings.TrimSpace(expr)

	// Strip a leading // — it just means "anywhere in document" which is the
	// default for CSS selectors.
	if strings.HasPrefix(expr, "//") {
		expr = expr[2:]
	}

	// Tokenise: walk the expression, splitting on // (descendant) and /
	// (direct-child) while preserving which separator was used.
	type token struct {
		css      string // CSS for this step
		combinator string // combinator *before* this step (" " or " > ")
	}

	var tokens []token
	first := true

	for expr != "" {
		// Find the next axis separator.
		dIdx := strings.Index(expr, "//")
		cIdx := strings.Index(expr, "/")

		var step, remaining, nextCombinator string

		switch {
		case dIdx == -1 && cIdx == -1:
			// No more separators — consume the rest.
			step = expr
			remaining = ""
			nextCombinator = ""
		case dIdx != -1 && (cIdx == -1 || dIdx <= cIdx):
			// "//" comes first (or ties with "/").
			step = expr[:dIdx]
			remaining = expr[dIdx+2:] // skip "//"
			nextCombinator = " "      // descendant combinator for the following token
		default:
			// "/" comes first.
			step = expr[:cIdx]
			remaining = expr[cIdx+1:] // skip "/"
			nextCombinator = " > "    // child combinator for the following token
		}

		expr = remaining

		if step == "" && first {
			// Leading separator after stripping "//"; skip.
			first = false
			continue
		}
		if step == "" {
			continue
		}

		combinator := ""
		if !first {
			// The combinator was recorded on the *previous* token's nextCombinator.
			// We need a different approach: store it on the current token.
			// We'll fix this after the loop — for now store nextCombinator separately.
		}
		_ = combinator
		tokens = append(tokens, token{css: convertXPathStep(step)})
		first = false

		// Store the nextCombinator so the *next* token can pick it up.
		if nextCombinator != "" && len(tokens) > 0 {
			tokens[len(tokens)-1].combinator = nextCombinator
		}
	}

	// Build the CSS selector.  Token i's combinator field holds the combinator
	// that separates token i from token i+1.
	var b strings.Builder
	for i, t := range tokens {
		if i > 0 {
			prev := tokens[i-1]
			if prev.combinator != "" {
				b.WriteString(prev.combinator)
			} else {
				b.WriteString(" ")
			}
		}
		b.WriteString(t.css)
	}
	return b.String()
}

// convertXPathStep converts a single XPath step (tag + optional predicates) to CSS.
func convertXPathStep(step string) string {
	bracketIdx := strings.IndexByte(step, '[')
	if bracketIdx == -1 {
		return step
	}
	tag := step[:bracketIdx]
	predicates := step[bracketIdx:]

	var b strings.Builder
	b.WriteString(tag)

	for _, m := range attrPredRe.FindAllStringSubmatch(predicates, -1) {
		attrName := m[1]
		attrVal := m[2]
		switch {
		case attrName == "id" && attrVal != "":
			b.WriteByte('#')
			b.WriteString(attrVal)
		case attrVal != "":
			b.WriteString("[")
			b.WriteString(attrName)
			b.WriteString("='")
			b.WriteString(attrVal)
			b.WriteString("']")
		default:
			b.WriteString("[")
			b.WriteString(attrName)
			b.WriteString("]")
		}
	}
	return b.String()
}
