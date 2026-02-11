// Package bibtex provides a format plugin for BibTeX bibliography entries.
package bibtex

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Version documents the BibTeX specification this implementation targets.
const Version = "bibtex-1988+biblatex"

// Format implements the BibTeX format.
type Format struct{}

// Ensure Format implements the interfaces
var (
	_ format.Format     = (*Format)(nil)
	_ format.Serializer = (*Format)(nil)
)

// Name returns the format identifier.
func (f *Format) Name() string {
	return "bibtex"
}

// Description returns a human-readable format description.
func (f *Format) Description() string {
	return "BibTeX bibliography format"
}

// Extensions returns file extensions associated with this format.
func (f *Format) Extensions() []string {
	return []string{"bib", "bibtex"}
}

// CanParse returns true if the input looks like BibTeX.
func (f *Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	if len(peek) == 0 {
		return false
	}

	// BibTeX entries start with @
	bibtexPatterns := [][]byte{
		[]byte("@article"),
		[]byte("@book"),
		[]byte("@inproceedings"),
		[]byte("@misc"),
		[]byte("@phdthesis"),
		[]byte("@mastersthesis"),
		[]byte("@techreport"),
		[]byte("@incollection"),
		[]byte("@inbook"),
		[]byte("@proceedings"),
		[]byte("@unpublished"),
		[]byte("@online"),
		[]byte("@string"),
		[]byte("@preamble"),
	}

	lowerPeek := bytes.ToLower(peek)
	for _, pattern := range bibtexPatterns {
		if bytes.Contains(lowerPeek, pattern) {
			return true
		}
	}

	return false
}

func init() {
	format.Register(&Format{})
}
