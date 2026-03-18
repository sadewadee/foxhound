package parse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	htmlPkg "golang.org/x/net/html"
)

// reWhitespace matches one or more whitespace characters; compiled once.
var reWhitespace = regexp.MustCompile(`\s+`)

// selectorSegment holds a tag name and its 1-based nth-of-type position,
// used when building CSS and XPath selector strings.
type selectorSegment struct {
	tag string
	pos int
}

// Re applies the regular-expression pattern to the element's text content and
// returns all matches. Returns nil for an invalid pattern.
func (e *Element) Re(pattern string) []string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	return re.FindAllString(e.Text(), -1)
}

// ReFirst returns the first regex match on the element's text content,
// or "" if there is no match. Returns "" for an invalid pattern.
func (e *Element) ReFirst(pattern string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}
	return re.FindString(e.Text())
}

// FindAncestor walks up the DOM tree and returns the first ancestor element
// where fn returns true. Returns nil if no ancestor satisfies fn.
func (e *Element) FindAncestor(fn func(*Element) bool) *Element {
	current := e.Parent()
	for current != nil {
		if fn(current) {
			return current
		}
		current = current.Parent()
	}
	return nil
}

// Ancestors returns all ancestor elements from the direct parent up to the
// root, in nearest-first order.
func (e *Element) Ancestors() []*Element {
	var result []*Element
	current := e.Parent()
	for current != nil {
		result = append(result, current)
		current = current.Parent()
	}
	return result
}

// BelowElements returns all descendant elements (direct and indirect children)
// in document order. Text nodes and non-element nodes are excluded.
func (e *Element) BelowElements() []*Element {
	return selectionToElements(e.sel.Find("*"), e.doc)
}

// JSON parses the element's text content as JSON and returns a
// map[string]any. Returns an error when the text is not valid JSON.
func (e *Element) JSON() (map[string]any, error) {
	var result map[string]any
	text := strings.TrimSpace(e.sel.Text())
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("parse: element JSON: %w", err)
	}
	return result, nil
}

// Clean returns the element's text content with leading/trailing whitespace
// trimmed and internal runs of whitespace collapsed to a single space.
func (e *Element) Clean() string {
	return reWhitespace.ReplaceAllString(strings.TrimSpace(e.sel.Text()), " ")
}

// GetAllText returns all text content of the element including all nested
// descendants, concatenated via goquery's Text() method.
func (e *Element) GetAllText() string {
	return e.sel.Text()
}

// Prettify returns the outer HTML representation of the element.
// For a full indented rendering the outer HTML is returned as-is; full
// pretty-printing requires a dedicated HTML formatter not included here.
func (e *Element) Prettify() string {
	return e.OuterHTML()
}

// Path returns the tag names from the document root down to this element,
// e.g. ["html", "body", "div", "h1"].
func (e *Element) Path() []string {
	// Build ancestors list (nearest-first), then reverse to get root-first.
	ancestors := e.Ancestors()
	path := make([]string, 0, len(ancestors)+1)
	for i := len(ancestors) - 1; i >= 0; i-- {
		if tag := ancestors[i].Tag(); tag != "" {
			path = append(path, tag)
		}
	}
	if tag := e.Tag(); tag != "" {
		path = append(path, tag)
	}
	return path
}

// InnerHTML returns the HTML content inside the element (the element's
// children serialised to HTML, excluding the element's own opening/closing
// tags). Equivalent to Element.HTML().
func (e *Element) InnerHTML() string {
	html, _ := e.sel.Html()
	return html
}

// OuterHTML returns the full HTML of the element including its own tags.
func (e *Element) OuterHTML() string {
	node := e.sel.Get(0)
	if node == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := htmlPkg.Render(&buf, node); err != nil {
		return ""
	}
	return buf.String()
}

// GenerateCSSSelector generates a CSS selector that uniquely identifies this
// element within its document. When the element has an id attribute, "#id" is
// returned directly. Otherwise, a path of "tag:nth-of-type(n)" segments
// separated by " > " is built from the nearest ancestor with an id (or the
// root) down to this element.
func (e *Element) GenerateCSSSelector() string {
	// Short-circuit: element itself has an id.
	if id := e.Attr("id"); id != "" {
		return "#" + id
	}

	// Walk upward collecting path segments until we hit an element with an id
	// or run out of ancestors.
	var segments []selectorSegment
	current := e
	for current != nil {
		tag := current.Tag()
		if tag == "" || tag == "html" {
			break
		}
		// If this ancestor has an id, anchor to it and stop.
		if id := current.Attr("id"); id != "" && current != e {
			parts := make([]string, 0, len(segments)+1)
			parts = append(parts, "#"+id)
			for i := len(segments) - 1; i >= 0; i-- {
				parts = append(parts, fmt.Sprintf("%s:nth-of-type(%d)", segments[i].tag, segments[i].pos))
			}
			return strings.Join(parts, " > ")
		}
		segments = append(segments, selectorSegment{tag: tag, pos: nthOfType(current.sel)})
		current = current.Parent()
	}

	// Build selector from root downward.
	parts := make([]string, 0, len(segments))
	for i := len(segments) - 1; i >= 0; i-- {
		parts = append(parts, fmt.Sprintf("%s:nth-of-type(%d)", segments[i].tag, segments[i].pos))
	}
	return strings.Join(parts, " > ")
}

// GenerateXPathSelector generates a unique XPath expression for this element,
// using positional predicates at each level.
// Example: /html[1]/body[1]/div[2]/h1[1]
func (e *Element) GenerateXPathSelector() string {
	var segments []selectorSegment
	current := e
	for current != nil {
		tag := current.Tag()
		if tag == "" {
			break
		}
		segments = append(segments, selectorSegment{tag: tag, pos: nthOfType(current.sel)})
		current = current.Parent()
	}
	// segments are in child→root order; reverse to root→child.
	var parts []string
	for i := len(segments) - 1; i >= 0; i-- {
		s := segments[i]
		parts = append(parts, fmt.Sprintf("%s[%d]", s.tag, s.pos))
	}
	return "/" + strings.Join(parts, "/")
}
