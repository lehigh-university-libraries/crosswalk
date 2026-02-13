// Package dublincore provides a format plugin for Dublin Core metadata.
package dublincore

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Version documents the Dublin Core specification this implementation targets.
const Version = "2020-01-20"

// Format implements the Dublin Core format.
type Format struct{}

// Ensure Format implements the interfaces
var (
	_ format.Format     = (*Format)(nil)
	_ format.Parser     = (*Format)(nil)
	_ format.Serializer = (*Format)(nil)
)

// Name returns the format identifier.
func (f *Format) Name() string {
	return "dublincore"
}

// Description returns a human-readable format description.
func (f *Format) Description() string {
	return "Dublin Core Metadata Element Set (v" + Version + ")"
}

// Extensions returns file extensions associated with this format.
func (f *Format) Extensions() []string {
	return []string{"xml", "dc"}
}

// CanParse returns true if the input looks like Dublin Core XML.
func (f *Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	if len(peek) == 0 {
		return false
	}

	if peek[0] != '<' {
		return false
	}

	dcPatterns := [][]byte{
		[]byte("purl.org/dc/elements"),
		[]byte("purl.org/dc/terms"),
		[]byte("dc:"),
		[]byte("dcterms:"),
		[]byte("<metadata"),
	}

	for _, pattern := range dcPatterns {
		if bytes.Contains(peek, pattern) {
			return true
		}
	}

	return false
}

func init() {
	format.Register(&Format{})
}
