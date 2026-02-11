// Package schemaorg provides a format plugin for schema.org JSON-LD.
package schemaorg

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Version is the schema.org version this implementation targets.
const Version = "29.4"

// Format implements the schema.org JSON-LD format.
type Format struct{}

// Ensure Format implements the interfaces
var (
	_ format.Format     = (*Format)(nil)
	_ format.Parser     = (*Format)(nil)
	_ format.Serializer = (*Format)(nil)
)

// Name returns the format identifier.
func (f *Format) Name() string {
	return "schemaorg"
}

// Description returns a human-readable format description.
func (f *Format) Description() string {
	return "schema.org JSON-LD (v" + Version + ")"
}

// Extensions returns file extensions associated with this format.
func (f *Format) Extensions() []string {
	return []string{"jsonld"}
}

// CanParse returns true if the input looks like schema.org JSON-LD.
func (f *Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	if len(peek) == 0 {
		return false
	}

	// Must be JSON
	if peek[0] != '{' && peek[0] != '[' {
		return false
	}

	// Look for schema.org patterns
	schemaOrgPatterns := [][]byte{
		[]byte(`"@context"`),
		[]byte(`"@type"`),
		[]byte(`schema.org`),
		[]byte(`"ScholarlyArticle"`),
		[]byte(`"Dataset"`),
		[]byte(`"Book"`),
		[]byte(`"Person"`),
		[]byte(`"Organization"`),
	}

	matchCount := 0
	for _, pattern := range schemaOrgPatterns {
		if bytes.Contains(peek, pattern) {
			matchCount++
		}
	}

	// If we have @context or @type plus another match, it's likely schema.org
	hasContext := bytes.Contains(peek, []byte(`"@context"`))
	hasType := bytes.Contains(peek, []byte(`"@type"`))

	return (hasContext || hasType) && matchCount >= 2
}

func init() {
	format.Register(&Format{})
}
