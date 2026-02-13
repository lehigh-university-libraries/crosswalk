// Package arxiv provides a format plugin for arXiv metadata XML.
package arxiv

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Version is the arXiv schema version this implementation targets.
const Version = "1.0"

// Format implements the arXiv metadata format.
type Format struct{}

// Ensure Format implements the interfaces
var (
	_ format.Format     = (*Format)(nil)
	_ format.Parser     = (*Format)(nil)
	_ format.Serializer = (*Format)(nil)
)

// Name returns the format identifier.
func (f *Format) Name() string {
	return "arxiv"
}

// Description returns a human-readable format description.
func (f *Format) Description() string {
	return "arXiv metadata XML (v" + Version + ")"
}

// Extensions returns file extensions associated with this format.
func (f *Format) Extensions() []string {
	return []string{"xml"}
}

// CanParse returns true if the input looks like arXiv XML.
func (f *Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	if len(peek) == 0 {
		return false
	}

	// Must be XML
	if peek[0] != '<' {
		return false
	}

	// Look for arXiv-specific patterns across all three schema variants
	arxivPatterns := [][]byte{
		[]byte(`arXivRecord`),                   // XSD 1.0
		[]byte(`http://arXiv.org/arXivRecord`),  // XSD 1.0 namespace
		[]byte(`http://arxiv.org/OAI/arXiv/`),   // OAI-PMH namespace
		[]byte(`http://arxiv.org/schemas/atom`), // Atom API namespace
	}

	for _, pattern := range arxivPatterns {
		if bytes.Contains(peek, pattern) {
			return true
		}
	}

	return false
}

func init() {
	format.Register(&Format{})
}
