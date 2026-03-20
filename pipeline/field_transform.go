package pipeline

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	foxhound "github.com/sadewadee/foxhound"
)

// FieldTransform defines a transformation to apply to a single item field.
type FieldTransform struct {
	// Field is the source field name.
	Field string
	// RegexFind is the pattern to match (empty = skip regex).
	RegexFind string
	// RegexReplace is the replacement string (supports $1, $2, etc).
	RegexReplace string
	// RenameTo renames the field (empty = keep original name).
	RenameTo string
	// CoerceTo converts the field value: "int", "float", "bool", "string".
	CoerceTo string
}

// FieldTransformPipeline applies a list of field transformations to each item.
type FieldTransformPipeline struct {
	transforms []FieldTransform
	compiled   map[string]*regexp.Regexp // cached compiled regexps
}

// NewFieldTransformPipeline creates a pipeline from a list of transforms.
func NewFieldTransformPipeline(transforms []FieldTransform) *FieldTransformPipeline {
	compiled := make(map[string]*regexp.Regexp)
	for _, t := range transforms {
		if t.RegexFind != "" {
			if re, err := regexp.Compile(t.RegexFind); err == nil {
				compiled[t.Field+":"+t.RegexFind] = re
			}
		}
	}
	return &FieldTransformPipeline{transforms: transforms, compiled: compiled}
}

// Process applies all transforms to the item.
func (p *FieldTransformPipeline) Process(_ context.Context, item *foxhound.Item) (*foxhound.Item, error) {
	if item == nil {
		return nil, nil
	}

	for _, t := range p.transforms {
		val, ok := item.Fields[t.Field]
		if !ok {
			continue
		}

		// Regex replace.
		if t.RegexFind != "" {
			key := t.Field + ":" + t.RegexFind
			if re, exists := p.compiled[key]; exists {
				s := fmt.Sprintf("%v", val)
				val = re.ReplaceAllString(s, t.RegexReplace)
				item.Fields[t.Field] = val
			}
		}

		// Type coercion.
		if t.CoerceTo != "" {
			item.Fields[t.Field] = coerce(val, t.CoerceTo)
		}

		// Rename.
		if t.RenameTo != "" && t.RenameTo != t.Field {
			item.Fields[t.RenameTo] = item.Fields[t.Field]
			delete(item.Fields, t.Field)
		}
	}

	return item, nil
}

// coerce converts a value to the target type.
func coerce(val any, target string) any {
	s := fmt.Sprintf("%v", val)
	switch target {
	case "int":
		// Strip non-numeric chars except minus.
		cleaned := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' || r == '-' {
				return r
			}
			return -1
		}, s)
		if v, err := strconv.Atoi(cleaned); err == nil {
			return v
		}
		return 0
	case "float":
		cleaned := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' || r == '.' || r == '-' {
				return r
			}
			return -1
		}, s)
		if v, err := strconv.ParseFloat(cleaned, 64); err == nil {
			return v
		}
		return 0.0
	case "bool":
		s = strings.ToLower(s)
		return s == "true" || s == "1" || s == "yes"
	case "string":
		return s
	default:
		return val
	}
}
