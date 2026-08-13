// Package wos parses Web of Science Starter API JSON into Crosswalk Hub records.
package wos

import (
	"bytes"

	"github.com/lehigh-university-libraries/crosswalk/format"
)

// Version is the Web of Science Starter representation implemented here.
const Version = "starter-v1"

// Format implements the Web of Science Starter JSON format.
type Format struct{}

var _ format.Parser = (*Format)(nil)

// Name returns the format identifier.
func (*Format) Name() string { return "wos" }

// Description returns a human-readable format description.
func (*Format) Description() string { return "Web of Science Starter API JSON" }

// Extensions returns the associated file extensions.
func (*Format) Extensions() []string { return []string{"json"} }

// CanParse reports whether input resembles a Starter API document or result page.
func (*Format) CanParse(peek []byte) bool {
	peek = bytes.TrimSpace(peek)
	return len(peek) > 0 && peek[0] == '{' && bytes.Contains(peek, []byte(`"uid"`)) &&
		(bytes.Contains(peek, []byte(`"source"`)) || bytes.Contains(peek, []byte(`"hits"`)))
}

func init() { format.MustRegister(&Format{}) }
