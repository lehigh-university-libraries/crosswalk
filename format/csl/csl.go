// Package csl provides a format plugin for CSL-JSON (Citation Style Language).
package csl

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Version documents the CSL specification this implementation targets.
const Version = "1.0.2"

// Format implements the CSL-JSON format.
type Format struct{}

// Ensure Format implements the interfaces
var (
	_ format.Format     = (*Format)(nil)
	_ format.Serializer = (*Format)(nil)
)

// Name returns the format identifier.
func (f *Format) Name() string {
	return "csl"
}

// Description returns a human-readable format description.
func (f *Format) Description() string {
	return "CSL-JSON (Citation Style Language v" + Version + ")"
}

// Extensions returns file extensions associated with this format.
func (f *Format) Extensions() []string {
	return []string{"json", "csl"}
}

// CanParse returns true if the input looks like CSL-JSON.
func (f *Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	if len(peek) == 0 {
		return false
	}

	// CSL-JSON starts with [ or { and contains type field
	if peek[0] != '[' && peek[0] != '{' {
		return false
	}

	patterns := [][]byte{
		[]byte(`"type"`),
		[]byte(`"id"`),
		[]byte(`"title"`),
		[]byte(`"author"`),
	}

	matchCount := 0
	for _, pattern := range patterns {
		if bytes.Contains(peek, pattern) {
			matchCount++
		}
	}

	return matchCount >= 2
}

func init() {
	format.Register(&Format{})
}
