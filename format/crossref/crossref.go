// Package crossref provides a format plugin for CrossRef deposit XML.
package crossref

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Version documents the CrossRef specification this implementation targets.
const Version = "5.3.1"

// Format implements the CrossRef deposit format.
type Format struct{}

// Ensure Format implements the interfaces
var (
	_ format.Format     = (*Format)(nil)
	_ format.Serializer = (*Format)(nil)
)

// Name returns the format identifier.
func (f *Format) Name() string {
	return "crossref"
}

// Description returns a human-readable format description.
func (f *Format) Description() string {
	return "CrossRef Deposit XML (Schema v" + Version + ")"
}

// Extensions returns file extensions associated with this format.
func (f *Format) Extensions() []string {
	return []string{"xml"}
}

// CanParse returns true if the input looks like CrossRef deposit XML.
func (f *Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	if len(peek) == 0 {
		return false
	}

	if peek[0] != '<' {
		return false
	}

	patterns := [][]byte{
		[]byte("doi_batch"),
		[]byte("crossref.org/schema"),
		[]byte("doi_data"),
		[]byte("journal_article"),
		[]byte("dissertation"),
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
