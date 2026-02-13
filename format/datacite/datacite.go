// Package datacite provides a format plugin for DataCite metadata.
package datacite

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Version documents the DataCite specification this implementation targets.
const Version = "4.6"

// Format implements the DataCite format.
type Format struct{}

// Ensure Format implements the interfaces
var (
	_ format.Format     = (*Format)(nil)
	_ format.Parser     = (*Format)(nil)
	_ format.Serializer = (*Format)(nil)
)

// Name returns the format identifier.
func (f *Format) Name() string {
	return "datacite"
}

// Description returns a human-readable format description.
func (f *Format) Description() string {
	return "DataCite Metadata Schema (v" + Version + ")"
}

// Extensions returns file extensions associated with this format.
func (f *Format) Extensions() []string {
	return []string{"xml"}
}

// CanParse returns true if the input looks like DataCite XML.
func (f *Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	if len(peek) == 0 {
		return false
	}

	if peek[0] != '<' {
		return false
	}

	patterns := [][]byte{
		[]byte("datacite.org/schema"),
		[]byte("<resource"),
		[]byte("<identifier identifierType"),
	}

	for _, pattern := range patterns {
		if bytes.Contains(peek, pattern) {
			return true
		}
	}

	return false
}

func init() {
	format.Register(&Format{})
}
