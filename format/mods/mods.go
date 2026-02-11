// Package mods provides a format plugin for MODS (Metadata Object Description Schema).
package mods

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Version documents the MODS specification this implementation targets.
const Version = "3.8"

// Format implements the MODS format.
type Format struct{}

// Ensure Format implements the interfaces
var (
	_ format.Format     = (*Format)(nil)
	_ format.Serializer = (*Format)(nil)
)

// Name returns the format identifier.
func (f *Format) Name() string {
	return "mods"
}

// Description returns a human-readable format description.
func (f *Format) Description() string {
	return "MODS (Metadata Object Description Schema v" + Version + ")"
}

// Extensions returns file extensions associated with this format.
func (f *Format) Extensions() []string {
	return []string{"xml", "mods"}
}

// CanParse returns true if the input looks like MODS XML.
func (f *Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	if len(peek) == 0 {
		return false
	}

	if peek[0] != '<' {
		return false
	}

	patterns := [][]byte{
		[]byte("loc.gov/mods"),
		[]byte("<mods"),
		[]byte("<modsCollection"),
		[]byte("titleInfo"),
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
