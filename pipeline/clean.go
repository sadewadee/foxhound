package pipeline

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	foxhound "github.com/sadewadee/foxhound"
)

var (
	htmlTagRe    = regexp.MustCompile(`<[^>]*>`)
	priceStripRe = regexp.MustCompile(`[^0-9.]`)
)

// dateFormats lists the input layouts tried in order when NormalizeDate is enabled.
var dateFormats = []string{
	"January 2, 2006",
	"Jan 2, 2006",
	"2006-01-02",
	"01/02/2006",
	"02 Jan 2006",
	"02 January 2006",
	"2006/01/02",
}

const isoDate = "2006-01-02"

// Clean performs data cleaning on item fields.
// Each option is applied in order: TrimWhitespace → StripHTML → NormalizePrice → NormalizeDate.
type Clean struct {
	// TrimWhitespace calls strings.TrimSpace on every string field value.
	TrimWhitespace bool
	// StripHTML removes HTML tags (matching <…>) from every string field value.
	StripHTML bool
	// NormalizePrice converts currency strings like "$1,234.56" to float64.
	// The $, €, £ currency prefixes and comma separators are removed before parsing.
	// Non-parseable strings are left unchanged.
	NormalizePrice bool
	// NormalizeDate parses common date strings and rewrites them as "2006-01-02".
	// Unrecognised strings are left unchanged.
	NormalizeDate bool
}

// Process applies the enabled cleaning operations to each string field in item.
// It returns the modified item; it never drops an item or returns an error.
func (c *Clean) Process(_ context.Context, item *foxhound.Item) (*foxhound.Item, error) {
	for key, val := range item.Fields {
		s, ok := val.(string)
		if !ok {
			continue
		}

		if c.TrimWhitespace {
			s = strings.TrimSpace(s)
		}

		if c.StripHTML {
			s = htmlTagRe.ReplaceAllString(s, "")
		}

		if c.NormalizePrice {
			if f, ok := parsePrice(s); ok {
				item.Fields[key] = f
				continue
			}
		}

		if c.NormalizeDate {
			if d, ok := parseDate(s); ok {
				item.Fields[key] = d
				continue
			}
		}

		item.Fields[key] = s
	}
	return item, nil
}

// parsePrice strips currency symbols and commas then parses as float64.
// Returns (value, true) on success, (0, false) if the string is not a valid price.
func parsePrice(s string) (float64, bool) {
	// Remove currency prefixes and whitespace.
	trimmed := strings.TrimSpace(s)
	trimmed = strings.TrimLeft(trimmed, "$€£")
	// Remove comma separators.
	trimmed = strings.ReplaceAll(trimmed, ",", "")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return 0, false
	}
	// Verify only digits and at most one dot remain.
	cleaned := priceStripRe.ReplaceAllString(trimmed, "")
	if cleaned != trimmed {
		return 0, false
	}
	f, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// parseDate tries each known format and returns the ISO date string on success.
func parseDate(s string) (string, bool) {
	for _, layout := range dateFormats {
		t, err := time.Parse(layout, s)
		if err == nil {
			return t.Format(isoDate), true
		}
	}
	return "", false
}
