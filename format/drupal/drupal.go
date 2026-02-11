package drupal

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Format implements the Drupal entity JSON format.
type Format struct{}

// Ensure Format implements the interfaces
var (
	_ format.Format     = (*Format)(nil)
	_ format.Parser     = (*Format)(nil)
	_ format.Serializer = (*Format)(nil)
)

// Name returns the format identifier.
func (f *Format) Name() string {
	return "drupal"
}

// Description returns a human-readable format description.
func (f *Format) Description() string {
	return "Drupal entity JSON (Islandora/Drupal content exports)"
}

// Extensions returns file extensions associated with this format.
func (f *Format) Extensions() []string {
	return []string{"json"}
}

// CanParse returns true if the input looks like Drupal entity JSON.
func (f *Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	if len(peek) == 0 {
		return false
	}

	// Must start with { or [
	if peek[0] != '{' && peek[0] != '[' {
		return false
	}

	// Look for common Drupal entity patterns
	drupalPatterns := [][]byte{
		[]byte(`"nid"`),
		[]byte(`"uuid"`),
		[]byte(`"type"`),
		[]byte(`"field_`),
		[]byte(`"target_id"`),
		[]byte(`"target_type"`),
	}

	matchCount := 0
	for _, pattern := range drupalPatterns {
		if bytes.Contains(peek, pattern) {
			matchCount++
		}
	}

	// If we match 2+ patterns, it's likely Drupal JSON
	return matchCount >= 2
}

func init() {
	format.Register(&Format{})
}
