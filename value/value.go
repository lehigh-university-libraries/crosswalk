// Package value provides primitives for extracting and serializing
// values from various metadata formats.
//
// These helpers solve common problems:
//   - Type coercion (string "123" → int)
//   - Null/empty handling
//   - Multi-value normalization
//   - Markup stripping
//   - Date parsing with precision
package value

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// =============================================================================
// TEXT VALUES
// =============================================================================

// Text extracts a string from various representations.
// Handles: string, []byte, fmt.Stringer, json.Number, numeric types, nil
func Text(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	case json.Number:
		return val.String()
	case fmt.Stringer:
		return val.String()
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case float32:
		if val == float32(int32(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(float64(val), 'f', -1, 32)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", val)
	}
}

// TextOr extracts a string with a default for empty/nil values.
func TextOr(v any, defaultVal string) string {
	s := Text(v)
	if s == "" {
		return defaultVal
	}
	return s
}

// TextOption configures text extraction behavior.
type TextOption func(*textConfig)

type textConfig struct {
	delimiter          string
	stripHTML          bool
	trimSpace          bool
	collapseWhitespace bool
}

// WithDelimiter splits delimited strings into slices.
func WithDelimiter(sep string) TextOption {
	return func(c *textConfig) {
		c.delimiter = sep
	}
}

// WithStripHTML removes HTML tags from text.
func WithStripHTML() TextOption {
	return func(c *textConfig) {
		c.stripHTML = true
	}
}

// WithTrimSpace trims leading/trailing whitespace.
func WithTrimSpace() TextOption {
	return func(c *textConfig) {
		c.trimSpace = true
	}
}

// WithCollapseWhitespace normalizes whitespace to single spaces.
func WithCollapseWhitespace() TextOption {
	return func(c *textConfig) {
		c.collapseWhitespace = true
	}
}

var (
	htmlTagRegex    = regexp.MustCompile(`<[^>]*>`)
	multiSpaceRegex = regexp.MustCompile(`\s+`)
)

func applyTextOptions(s string, cfg *textConfig) string {
	if cfg.stripHTML {
		s = htmlTagRegex.ReplaceAllString(s, "")
		s = html.UnescapeString(s)
	}
	if cfg.collapseWhitespace {
		s = multiSpaceRegex.ReplaceAllString(s, " ")
	}
	if cfg.trimSpace {
		s = strings.TrimSpace(s)
	}
	return s
}

// TextSlice normalizes a value to []string.
// Handles: string, []string, []any, delimited strings, nil
func TextSlice(v any, opts ...TextOption) []string {
	cfg := &textConfig{trimSpace: true}
	for _, opt := range opts {
		opt(cfg)
	}

	if v == nil {
		return nil
	}

	var result []string

	switch val := v.(type) {
	case []string:
		result = val
	case []any:
		result = make([]string, 0, len(val))
		for _, item := range val {
			if s := Text(item); s != "" {
				result = append(result, s)
			}
		}
	case string:
		if cfg.delimiter != "" && strings.Contains(val, cfg.delimiter) {
			parts := strings.Split(val, cfg.delimiter)
			result = make([]string, 0, len(parts))
			for _, p := range parts {
				if s := strings.TrimSpace(p); s != "" {
					result = append(result, s)
				}
			}
		} else if val != "" {
			result = []string{val}
		}
	default:
		if s := Text(v); s != "" {
			if cfg.delimiter != "" && strings.Contains(s, cfg.delimiter) {
				return TextSlice(s, opts...)
			}
			result = []string{s}
		}
	}

	// Apply options to each string
	for i, s := range result {
		result[i] = applyTextOptions(s, cfg)
	}

	// Filter empty strings after processing
	filtered := result[:0]
	for _, s := range result {
		if s != "" {
			filtered = append(filtered, s)
		}
	}

	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

// =============================================================================
// NUMERIC VALUES
// =============================================================================

// Int extracts an integer from various representations.
// Handles: int, float64, string ("123"), json.Number, nil (→ 0)
func Int(v any) int {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case int32:
		return int(val)
	case float64:
		return int(val)
	case float32:
		return int(val)
	case json.Number:
		i, _ := val.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(val))
		return i
	case bool:
		if val {
			return 1
		}
		return 0
	default:
		return 0
	}
}

// IntOr extracts an integer with a default for unparseable values.
func IntOr(v any, defaultVal int) int {
	if v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case int, int64, int32, float64, float32, json.Number:
		return Int(v)
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
			return i
		}
		return defaultVal
	default:
		return defaultVal
	}
}

// Float extracts a float64 from various representations.
func Float(v any) float64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case int32:
		return float64(val)
	case json.Number:
		f, _ := val.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(val), 64)
		return f
	default:
		return 0
	}
}

// =============================================================================
// BOOLEAN VALUES
// =============================================================================

// Bool extracts a boolean from various representations.
// Handles: bool, int (0/1), string ("true"/"false"/"1"/"0"/"yes"/"no"), nil
func Bool(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case int:
		return val != 0
	case int64:
		return val != 0
	case float64:
		return val != 0
	case json.Number:
		i, _ := val.Int64()
		return i != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(val))
		return s == "true" || s == "1" || s == "yes" || s == "on"
	default:
		return false
	}
}
