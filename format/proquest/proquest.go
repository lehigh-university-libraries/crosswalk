// Package proquest provides a format plugin for ProQuest ETD (Electronic Theses and Dissertations).
package proquest

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Version documents the ProQuest ETD specification this implementation targets.
const Version = "1.0"

// Format implements the ProQuest ETD format.
type Format struct{}

// Ensure Format implements the interfaces
var (
	_ format.Format     = (*Format)(nil)
	_ format.Serializer = (*Format)(nil)
)

// Name returns the format identifier.
func (f *Format) Name() string {
	return "proquest"
}

// Description returns a human-readable format description.
func (f *Format) Description() string {
	return "ProQuest ETD (Electronic Theses and Dissertations)"
}

// Extensions returns file extensions associated with this format.
func (f *Format) Extensions() []string {
	return []string{"xml"}
}

// CanParse returns true if the input looks like ProQuest ETD XML.
func (f *Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	if len(peek) == 0 {
		return false
	}

	if peek[0] != '<' {
		return false
	}

	patterns := [][]byte{
		[]byte("DISS_submission"),
		[]byte("DISS_authorship"),
		[]byte("DISS_description"),
		[]byte("DISS_content"),
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
