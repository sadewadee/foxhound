package parse

import (
	"math"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// ElementSignature captures the identity of an element for similarity
// matching.  It is designed to be serialised as JSON so that it can be
// persisted alongside an AdaptiveSelector.
type ElementSignature struct {
	Tag       string            `json:"tag"`
	ID        string            `json:"id,omitempty"`
	Classes   []string          `json:"classes,omitempty"`
	Attrs     map[string]string `json:"attrs,omitempty"`
	Text      string            `json:"text,omitempty"`      // truncated to 200 chars
	ParentTag string            `json:"parent_tag,omitempty"`
	Position  int               `json:"position"`            // nth-of-type (1-based)
	Depth     int               `json:"depth"`               // nesting depth from <html>
}

// SimilarMatch pairs an element with its similarity score.
type SimilarMatch struct {
	Element *Element
	Score   float64 // 0.0 (no match) to 1.0 (exact match)
}

// CaptureSignature creates a signature from an element for later matching.
func CaptureSignature(el *Element) *ElementSignature {
	if el == nil || el.sel.Length() == 0 {
		return &ElementSignature{}
	}

	node := el.sel.Get(0)
	sig := &ElementSignature{
		Tag:   el.Tag(),
		Attrs: el.Attrs(),
	}

	// ID
	if id, ok := el.sel.Attr("id"); ok {
		sig.ID = id
	}

	// Classes
	if cls, ok := el.sel.Attr("class"); ok {
		for _, c := range strings.Fields(cls) {
			sig.Classes = append(sig.Classes, c)
		}
	}

	// Text (trimmed, capped at 200 chars)
	text := strings.TrimSpace(el.sel.Text())
	if len(text) > 200 {
		text = text[:200]
	}
	sig.Text = text

	// Parent tag
	parent := el.sel.Parent()
	if parent.Length() > 0 {
		pNode := parent.Get(0)
		if pNode.Type == html.ElementNode {
			sig.ParentTag = pNode.Data
		}
	}

	// Depth: count ancestors up to the root
	sig.Depth = nodeDepth(node)

	// Position: count preceding siblings with the same tag (1-based)
	sig.Position = nthOfType(el.sel)

	return sig
}

// Similarity calculates how similar two element signatures are.
// The returned value is in the range [0.0, 1.0].
//
// Each signal contributes a maximum weight:
//
//	Tag match:            0.15
//	ID match:             0.25  (only counted when at least one sig has an ID)
//	Class Jaccard:        0.15
//	Text similarity:      0.15
//	Parent tag match:     0.10
//	Depth match:          0.10
//	Position proximity:   0.10
//
// The raw score is normalised by the maximum achievable score given the
// signals present in both signatures, so that identical elements always
// return 1.0 regardless of which optional fields are populated.
func Similarity(a, b *ElementSignature) float64 {
	var score, maxScore float64

	// Tag match (always evaluated)
	maxScore += 0.15
	if a.Tag != "" && a.Tag == b.Tag {
		score += 0.15
	}

	// ID match — only contributes when at least one signature carries an ID.
	if a.ID != "" || b.ID != "" {
		maxScore += 0.25
		if a.ID != "" && a.ID == b.ID {
			score += 0.25
		}
	}

	// Class Jaccard similarity (always evaluated)
	maxScore += 0.15
	score += 0.15 * jaccardClasses(a.Classes, b.Classes)

	// Text similarity (only contributes when at least one sig has text)
	if a.Text != "" || b.Text != "" {
		maxScore += 0.15
		score += 0.15 * textSimilarity(a.Text, b.Text)
	}

	// Parent tag (only contributes when at least one sig has a parent tag)
	if a.ParentTag != "" || b.ParentTag != "" {
		maxScore += 0.10
		if a.ParentTag != "" && a.ParentTag == b.ParentTag {
			score += 0.10
		}
	}

	// Depth (always evaluated)
	maxScore += 0.10
	if a.Depth == b.Depth {
		score += 0.10
	}

	// Position proximity (always evaluated)
	maxScore += 0.10
	maxPos := math.Max(float64(a.Position), math.Max(float64(b.Position), 1))
	posScore := 1.0 - math.Abs(float64(a.Position)-float64(b.Position))/maxPos
	score += 0.10 * posScore

	if maxScore == 0 {
		return 0
	}

	// Normalise to [0, 1] based on achievable maximum.
	normalised := score / maxScore
	if normalised > 1.0 {
		normalised = 1.0
	}
	if normalised < 0.0 {
		normalised = 0.0
	}
	return normalised
}

// FindSimilar searches the document for elements most similar to the given
// signature.  Returns matches sorted by score descending, filtered by
// minScore.
func (d *Document) FindSimilar(sig *ElementSignature, minScore float64) []SimilarMatch {
	var matches []SimilarMatch

	d.doc.Find("*").Each(func(_ int, s *goquery.Selection) {
		el := newElement(s, d)
		candidate := CaptureSignature(el)
		score := Similarity(sig, candidate)
		if score >= minScore {
			matches = append(matches, SimilarMatch{Element: el, Score: score})
		}
	})

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})
	return matches
}

// --- helpers ---

// jaccardClasses computes the Jaccard similarity of two string slices treated
// as sets.
func jaccardClasses(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0.0
	}
	setA := make(map[string]bool, len(a))
	for _, c := range a {
		setA[c] = true
	}
	intersection := 0
	union := len(setA)
	for _, c := range b {
		if setA[c] {
			intersection++
		} else {
			union++
		}
	}
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// textSimilarity returns a [0,1] score based on common prefix length relative
// to the longer string.  Identical strings return 1.0, completely different
// strings return 0.0.
func textSimilarity(a, b string) float64 {
	if a == "" && b == "" {
		return 0.0
	}
	if a == b {
		return 1.0
	}
	maxLen := math.Max(float64(len(a)), float64(len(b)))
	if maxLen == 0 {
		return 0.0
	}
	common := 0
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	for i := 0; i < limit; i++ {
		if a[i] == b[i] {
			common++
		} else {
			break
		}
	}
	return float64(common) / maxLen
}

// nodeDepth counts the number of ancestor nodes up to the document root.
func nodeDepth(n *html.Node) int {
	depth := 0
	for n = n.Parent; n != nil; n = n.Parent {
		depth++
	}
	return depth
}

// nthOfType returns the 1-based position of the selection among its siblings
// that share the same tag name.
func nthOfType(sel *goquery.Selection) int {
	if sel.Length() == 0 {
		return 0
	}
	tag := ""
	node := sel.Get(0)
	if node.Type == html.ElementNode {
		tag = node.Data
	}

	position := 1
	prev := sel.Prev()
	for prev.Length() > 0 {
		if prevNode := prev.Get(0); prevNode.Type == html.ElementNode && prevNode.Data == tag {
			position++
		}
		prev = prev.Prev()
	}
	return position
}
