// Package csv provides a format plugin for CSV metadata.
package csv

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Format implements the CSV format.
type Format struct{}

// Ensure Format implements the interfaces
var (
	_ format.Format     = (*Format)(nil)
	_ format.Parser     = (*Format)(nil)
	_ format.Serializer = (*Format)(nil)
)

// Name returns the format identifier.
func (f *Format) Name() string {
	return "csv"
}

// Description returns a human-readable format description.
func (f *Format) Description() string {
	return "Comma-separated values (CSV) metadata"
}

// Extensions returns file extensions associated with this format.
func (f *Format) Extensions() []string {
	return []string{"csv", "tsv"}
}

// CanParse returns true if the input looks like CSV data.
func (f *Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	if len(peek) == 0 {
		return false
	}

	// CSV typically starts with text, not { or [
	if peek[0] == '{' || peek[0] == '[' || peek[0] == '<' {
		return false
	}

	// Look for common CSV patterns
	// - Contains commas
	// - Has newlines with consistent structure
	// - Doesn't look like other formats

	hasComma := bytes.Contains(peek, []byte(","))
	hasTab := bytes.Contains(peek, []byte("\t"))
	hasNewline := bytes.Contains(peek, []byte("\n"))

	// If it has delimiters and newlines, it's probably CSV
	return (hasComma || hasTab) && hasNewline
}

func init() {
	format.Register(&Format{})
}
