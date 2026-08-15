// Package csl provides a format plugin for CSL-JSON (Citation Style Language).
package csl

import (
	"bytes"
	"encoding/json"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Version documents the CSL specification this implementation targets.
const Version = "1.0.2"

// Format implements the CSL-JSON format.
type Format struct{}

// Ensure Format implements the interfaces
var (
	_ format.Format     = (*Format)(nil)
	_ format.Parser     = (*Format)(nil)
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

	// Detect top-level CSL item keys structurally. Byte searches across nested
	// objects made Zenodo and other API envelopes look like CSL merely because
	// they contained an unrelated id and title below the document root.
	if peek[0] != '[' && peek[0] != '{' {
		return false
	}
	var item map[string]json.RawMessage
	if peek[0] == '{' {
		if err := json.Unmarshal(peek, &item); err != nil {
			return false
		}
	} else {
		var items []json.RawMessage
		if err := json.Unmarshal(peek, &items); err != nil || len(items) == 0 {
			return false
		}
		if err := json.Unmarshal(items[0], &item); err != nil {
			return false
		}
	}
	if _, hasType := item["type"]; !hasType {
		return false
	}
	for _, key := range []string{"id", "title", "author"} {
		if _, exists := item[key]; exists {
			return true
		}
	}
	return false
}

func init() {
	format.MustRegister(&Format{})
}
